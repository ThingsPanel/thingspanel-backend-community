package initialize

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	global "project/global"
	"project/initialize/automatecache"
	"project/model"

	"github.com/sirupsen/logrus"
	"gopkg.in/redis.v5"
)

const (
	AUTOMATE_CACHE_RESULT_NOT_FOUND = iota + 1 //缓存中无数据
	AUTOMATE_CACHE_RESULT_NOT_TASK             //缓存有数据 无任务
	AUTOMATE_CACHE_RESULT_OK                   //缓存有数据 有任务
)

const AUTOMATE_CACHE_CONTENT_NOT_TASK = "NOTASK" //设备无任务 存字符串NOTASK

type AutomateCache struct {
	client    *redis.Client
	expiredIn time.Duration
	device    AutimateCacheKeyDevice
}

var (
	instance *AutomateCache
	mu       sync.Mutex
)

func NewAutomateCache() *AutomateCache {
	mu.Lock()
	defer mu.Unlock()
	if instance == nil {
		instance = &AutomateCache{
			client:    global.REDIS,
			expiredIn: time.Hour * 24, //缓存获取时间暂设置为1天
			device:    automatecache.NewOneDeviceCache(),
		}
	}
	return instance
}

type AutimateCacheKeyDevice interface {
	GetAutomateCacheKeyPrefix() string //设备id或者设备配置
	GetDeviceTriggerConditionType() string
}

// @description getAutomateCacheKey 获取自动化缓存key 方便统一处理key
// @params cType string
// @params id sting
// @return string
func (c *AutomateCache) getAutomateCacheKey(cType string, id string) string {
	return fmt.Sprintf("automate:v1:%s:%s:%s", c.device.GetAutomateCacheKeyPrefix(), cType, id)
}

// 获取单个设置一级缓存key
func (c *AutomateCache) getAutomateCacheKeyBase(deviceId string) string {
	return c.getAutomateCacheKey("_", deviceId)
}

func (c *AutomateCache) getAutomateCacheKeyGroup(groupId string) string {
	return c.getAutomateCacheKey("_group_", groupId)
}

func (c *AutomateCache) getAutomateCacheKeyAction(sceneAutomationId string) string {
	return c.getAutomateCacheKey("_action_", sceneAutomationId)
}

func (c *AutomateCache) GetDeviceType() (AutimateCacheKeyDevice, error) {
	if c.device == nil {
		return nil, errors.New("未设置设备类型 单一设备或单类设备")
	}
	return c.device, nil
}

func (c *AutomateCache) SetDeviceType(deviceType AutimateCacheKeyDevice) {
	c.device = deviceType
}

func (c *AutomateCache) set(key string, value interface{}, expiration time.Duration) error {
	var valueStr string
	if val, ok := value.(string); ok {
		valueStr = val
	} else {
		valBytes, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		valueStr = string(valBytes)
	}
	return c.client.Set(key, valueStr, expiration).Err()
}

func (c *AutomateCache) scan(stringCmd *redis.StringCmd, val interface{}) (int, error) {
	str, err := stringCmd.Result()
	if err == redis.Nil {
		return AUTOMATE_CACHE_RESULT_NOT_FOUND, nil
	} else if err != nil {
		return 0, err
	}
	if str == AUTOMATE_CACHE_CONTENT_NOT_TASK {
		return AUTOMATE_CACHE_RESULT_NOT_TASK, nil
	}
	return AUTOMATE_CACHE_RESULT_OK, stringCmd.Scan(val)
}

// DeleteCacheBySceneAutomationId
// @description  根据联动场景id删除缓存
// @params sceneAutomationId string
// @return error
func (c *AutomateCache) DeleteCacheBySceneAutomationId(sceneAutomationId string) error {
	//删除单类设置缓存
	c.device = automatecache.NewMultipleDeviceCache()
	err := c.deleteCacheBySceneAutomationId(sceneAutomationId)
	if err != nil {
		return err
	}
	//删除单一设备缓存
	c.device = automatecache.NewOneDeviceCache()
	err = c.deleteCacheBySceneAutomationId(sceneAutomationId)
	if err != nil {
		return err
	}
	return nil
}

// DeleteCacheBySceneAutomationId
// @description  根据联动场景id删除缓存
// @params sceneAutomationId string
// @return error
func (c *AutomateCache) deleteCacheBySceneAutomationId(sceneAutomationId string) error {
	//1 先查询出动作缓存
	var (
		action         AutomateActionInfo
		deleteCacheKes []string
		deviceIds      []string
	)
	actionCacheKey := c.getAutomateCacheKeyAction(sceneAutomationId)
	resultInt, err := c.scan(c.client.Get(actionCacheKey), &action)
	if err != nil {
		return err
	}
	//为找到任务或者 缓存中无数据 无需删除
	//if resultInt == AUTOMATE_CACHE_RESULT_NOT_TASK || resultInt == AUTOMATE_CACHE_RESULT_NOT_FOUND {
	//	return nil
	//}
	if resultInt == AUTOMATE_CACHE_RESULT_NOT_FOUND {
		return nil
	}
	deleteCacheKes = append(deleteCacheKes, actionCacheKey)
	//2 group缓存key 并查询此场景关联的所有设备
	for _, groupId := range action.GroupIds {
		var (
			groupCacheKey = c.getAutomateCacheKeyGroup(groupId)
			groupInfos    DTConditions
		)
		deleteCacheKes = append(deleteCacheKes, groupCacheKey)
		err := c.client.Get(groupCacheKey).Scan(&groupInfos)
		if err != nil {
			continue
		}
		for _, v := range groupInfos {
			if v.TriggerConditionType == c.device.GetDeviceTriggerConditionType() {
				deviceIds = append(deviceIds, *v.TriggerSource)
			}
		}
	}
	//3 查询设备缓存 知道当前场景并删除（删除单个设备1级缓存）
	err = c.automateDeviceCacheDeleteHandel(sceneAutomationId, deviceIds, &deleteCacheKes)
	if err != nil {
		return err
	}
	//4 删除缓存
	return c.client.Del(deleteCacheKes...).Err()
}

// automateDeviceCacheDeleteHandel
// @description  自动化一级缓存 删除处理
// @params sceneAutomationId string
// @params ids []string
// @params deleteCacheKes *[]string
// @return error
func (c *AutomateCache) automateDeviceCacheDeleteHandel(sceneAutomationId string, ids []string, deleteCacheKes *[]string) error {
	for _, deviceId := range ids {
		var (
			baseCacheKey = c.getAutomateCacheKeyBase(deviceId)
			//baseCacheKey        = getCachekey(deviceId)
			automateDeviceInfos AutomateDeviceInfos
		)
		err := c.client.Get(baseCacheKey).Scan(&automateDeviceInfos)
		if err != nil {
			continue
		}
		for index, val := range automateDeviceInfos {
			if val.SceneAutomationId == sceneAutomationId {
				copy(automateDeviceInfos[index:], automateDeviceInfos[index+1:])
				automateDeviceInfos = automateDeviceInfos[:len(automateDeviceInfos)-1]
				break
			}
		}
		if len(automateDeviceInfos) > 0 {
			*deleteCacheKes = append(*deleteCacheKes, baseCacheKey)
		} else {
			err := c.set(baseCacheKey, automateDeviceInfos, c.expiredIn)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// SetCacheBySceneAutomationId
// @description  保存或编辑设置缓存
// @params sceneAutomationId string
// @params contions []model.DeviceTriggerCondition
// @params actions []model.ActionInfo
// @return error
func (c *AutomateCache) SetCacheBySceneAutomationId(sceneAutomationId string, conditions []model.DeviceTriggerCondition, actions []model.ActionInfo) error {

	//删除单类设置缓存
	c.device = automatecache.NewMultipleDeviceCache()
	err := c.setCacheBySceneAutomationId(sceneAutomationId, conditions, actions)
	if err != nil {
		return err
	}
	//删除单一设备缓存
	c.device = automatecache.NewOneDeviceCache()
	err = c.setCacheBySceneAutomationId(sceneAutomationId, conditions, actions)
	if err != nil {
		return err
	}

	return nil
}

// @description SetCacheBySceneAutomationId 保存或编辑设置缓存
// @params sceneAutomationId string
// @params contions []model.DeviceTriggerCondition
// @params actions []model.ActionInfo
// @return error
func (c *AutomateCache) setCacheBySceneAutomationId(sceneAutomationId string, conditions []model.DeviceTriggerCondition, actions []model.ActionInfo) error {
	automateDeviceInfo := AutomateDeviceInfo{
		SceneAutomationId: sceneAutomationId,
	}
	actionInfos := AutomateActionInfo{
		Actions: actions,
	}
	var (
		groupInfosMap = make(map[string][]model.DeviceTriggerCondition)
		deviceIdsMap  = make(map[string]bool)
	)

	for _, v := range conditions {
		groupInfosMap[v.GroupID] = append(groupInfosMap[v.GroupID], v)
		if v.TriggerConditionType == c.device.GetDeviceTriggerConditionType() {
			if _, ok := deviceIdsMap[*v.TriggerSource]; !ok {
				deviceIdsMap[*v.TriggerSource] = true
			}
		}
	}

	//group条件保存
	for groupId, val := range groupInfosMap {
		//去掉条件中 不存在此类型的条件组 防止重复到定时任务条件组
		var ok bool
		for _, v := range val {
			if v.TriggerConditionType == c.device.GetDeviceTriggerConditionType() {
				ok = true
				break
			}
		}
		if !ok {
			continue
		}
		automateDeviceInfo.GroupIds = append(automateDeviceInfo.GroupIds, groupId)
		actionInfos.GroupIds = append(actionInfos.GroupIds, groupId)
		var groupCacheKey = c.getAutomateCacheKeyGroup(groupId)
		err := c.set(groupCacheKey, val, c.expiredIn)
		if err != nil {
			return err
		}
	}
	//动作保存
	c.set(c.getAutomateCacheKeyAction(sceneAutomationId), actionInfos, c.expiredIn)

	//单个设备缓存保存
	for deviceId := range deviceIdsMap {
		var automateDeviceInfos AutomateDeviceInfos
		var deviceCacheKey = c.getAutomateCacheKeyBase(deviceId)
		_, err := c.scan(c.client.Get(deviceCacheKey), &automateDeviceInfos)
		if err != nil {
			continue
		}
		automateDeviceInfos = append(automateDeviceInfos, automateDeviceInfo)
		err = c.set(deviceCacheKey, automateDeviceInfos, c.expiredIn)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetCacheByDeviceId
// @description  设备遥测获取缓存联动信息
// @params deviceId string 1无数据 2无自动化任务 3有任务
// @return AutomateExecteParams error
func (c *AutomateCache) GetCacheByDeviceId(deviceId, deviceConfigId string) (AutomateExecteParams, int, error) {
	var deviceCacheKey string
	if deviceConfigId == "" {
		c.SetDeviceType(automatecache.NewOneDeviceCache())
		deviceCacheKey = c.getAutomateCacheKeyBase(deviceId)
	} else {
		c.SetDeviceType(automatecache.NewMultipleDeviceCache())
		deviceCacheKey = c.getAutomateCacheKeyBase(deviceConfigId)
	}
	return c.getCacheByDeviceId(deviceId, deviceConfigId, deviceCacheKey)
}

// getCacheByDeviceId
// @description  设备遥测获取缓存联动信息
// @params deviceId string 1无数据 2无自动化任务 3有任务
// @return AutomateExecteParams error
func (c *AutomateCache) getCacheByDeviceId(deviceId, deviceConfigId, deviceCacheKey string) (AutomateExecteParams, int, error) {
	var (
		automateDeviceInfos   = make(AutomateDeviceInfos, 0)
		automateExecuteParams = AutomateExecteParams{
			DeviceId:       deviceId,
			DeviceConfigId: deviceConfigId,
		}
		resultInt int
	)
	stringCmd := c.client.Get(deviceCacheKey)
	resultInt, err := c.scan(stringCmd, &automateDeviceInfos)
	if err != nil || resultInt != AUTOMATE_CACHE_RESULT_OK {
		return automateExecuteParams, resultInt, err
	}

	for _, info := range automateDeviceInfos {
		automateExecuteSceneInfo := AutomateExecteSceneInfo{
			SceneAutomationId: info.SceneAutomationId,
		}
		for _, groupId := range info.GroupIds {
			var (
				groupCacheKey = c.getAutomateCacheKeyGroup(groupId)
				condition     DTConditions
			)
			err := c.client.Get(groupCacheKey).Scan(&condition)
			if err != nil {
				logrus.Warning("redis未查询到数据1", err)
				continue
			}
			automateExecuteSceneInfo.GroupsCondition = append(automateExecuteSceneInfo.GroupsCondition, condition...)
		}
		var (
			actionInfo     AutomateActionInfo
			actionCacheKey = c.getAutomateCacheKeyAction(info.SceneAutomationId)
		)
		err := c.client.Get(actionCacheKey).Scan(&actionInfo)
		if err != nil {
			logrus.Warning("redis未查询到数据", err)
			continue
		}
		automateExecuteSceneInfo.Actions = actionInfo.Actions
		automateExecuteParams.AutomateExecteSceeInfos = append(automateExecuteParams.AutomateExecteSceeInfos, automateExecuteSceneInfo)
	}
	return automateExecuteParams, resultInt, nil
}

// SetCacheByDeviceId
// @description  保存设备务缓存
// @params deviceId string
// @params deviceConfigId string 设备配置id 存在就走 单类设备
// @params conditions []model.DeviceTriggerCondition
// @params actions []model.ActionInfo
// @return AutomateExecteParams error
func (c *AutomateCache) SetCacheByDeviceId(deviceId, deviceConfigId string, conditions []model.DeviceTriggerCondition, actions []model.ActionInfo) error {
	if deviceConfigId == "" {
		c.device = automatecache.NewOneDeviceCache()
	} else {
		c.device = automatecache.NewMultipleDeviceCache()
	}

	return c.setCacheByDeviceId(deviceId, deviceConfigId, conditions, actions)
}

// @description setCacheByDeviceId 保存设备务缓存
// @params deviceId string
// @params deviceConfigId string 设备配置id 存在就走 单类设备
// @params conditions []model.DeviceTriggerCondition
// @params actions []model.ActionInfo
// @return AutomateExecteParams error
func (c *AutomateCache) setCacheByDeviceId(deviceId, deviceConfigId string, conditions []model.DeviceTriggerCondition, actions []model.ActionInfo) error {
	var (
		groupInfosMap  = make(map[string][]model.DeviceTriggerCondition)
		deviceInfosMap = make(map[string]map[string]bool)
	)
	logrus.Debug("deviceConfigID", deviceConfigId)
	for _, v := range conditions {
		groupInfosMap[v.GroupID] = append(groupInfosMap[v.GroupID], v)
		if deviceInfosMap[v.SceneAutomationID] == nil {
			deviceInfosMap[v.SceneAutomationID] = make(map[string]bool)
		}
		deviceInfosMap[v.SceneAutomationID][v.GroupID] = true
	}

	//group条件保存
	for groupId, val := range groupInfosMap {
		var groupCacheKey = c.getAutomateCacheKeyGroup(groupId)
		err := c.set(groupCacheKey, val, c.expiredIn)
		if err != nil {
			return err
		}
	}
	//动作保存
	var (
		actionsMap = make(map[string][]model.ActionInfo)
	)
	for _, val := range actions {
		actionsMap[val.SceneAutomationID] = append(actionsMap[val.SceneAutomationID], val)
	}
	//设备缓存
	var automateDeviceInfos []AutomateDeviceInfo
	for sceneAutomationID, actions := range actionsMap {
		var groupIds []string
		if groupsMap, ok := deviceInfosMap[sceneAutomationID]; ok {
			for groupId := range groupsMap {
				groupIds = append(groupIds, groupId)
			}
			actionInfos := AutomateActionInfo{
				Actions:  actions,
				GroupIds: groupIds,
			}
			c.set(c.getAutomateCacheKeyAction(sceneAutomationID), actionInfos, c.expiredIn)
			automateDeviceInfos = append(automateDeviceInfos, AutomateDeviceInfo{
				SceneAutomationId: sceneAutomationID,
				GroupIds:          groupIds,
			})
		}
	}
	cacheKey := c.getAutomateCacheKeyBase(deviceId)
	//保存设备任务缓存
	return c.set(cacheKey, automateDeviceInfos, c.expiredIn)
}

// @description SetCacheByDeviceIdWithNoTask 设置无任务的设备缓存
// @params deviceId string
// @return error
func (c *AutomateCache) SetCacheByDeviceIdWithNoTask(deviceId, deviceConfigId string) error {
	if deviceConfigId == "" {
		c.device = automatecache.NewOneDeviceCache()
	} else {
		c.device = automatecache.NewMultipleDeviceCache()
	}
	cacheKey := c.getAutomateCacheKeyBase(deviceId)
	return c.set(cacheKey, AUTOMATE_CACHE_CONTENT_NOT_TASK, c.expiredIn)
}

type AutomateDeviceInfo struct {
	SceneAutomationId string   `json:"scene_automation_id"`
	GroupIds          []string `json:"group_id"`
}

type AutomateActionInfo struct {
	GroupIds []string           `json:"group_id"`
	Actions  []model.ActionInfo `json:"actions"`
}

type AutomateExecteSceneInfo struct {
	SceneAutomationId string             `json:"scene_automation_id"`
	GroupsCondition   DTConditions       `json:"groups_condition"`
	Actions           []model.ActionInfo `json:"actions"`
}

type AutomateExecteParams struct {
	DeviceId                string                    `json:"device_id"`
	DeviceConfigId          string                    `json:"device_config_id"`
	AutomateExecteSceeInfos []AutomateExecteSceneInfo `json:"automate_execte_scene_infos"`
}

type AutomateDeviceInfos []AutomateDeviceInfo

// UnmarshalBinary(data []byte) error
func (a *AutomateDeviceInfos) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, a)
}

type DTConditions []model.DeviceTriggerCondition

func (a *DTConditions) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, a)
}

//AutomateActionInfo

func (a *AutomateActionInfo) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, a)
}
