package initialize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	global "project/pkg/global"
	"sync"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

var (
	alarmCache *AlarmCache
	alarmMu    sync.Mutex
)

type AlarmCache struct {
	client   *redis.Client
	expireIn time.Duration
}

// 缓存1 aralm_groupid 以场景组id 保存出发告警config_id, 设备id
// 缓存2 aralm_device_id 以设备id  组id'
// 缓存3 alarm_config_id  以告警id 保存组id
// 缓存4 scene_automation_id 以场景id 保存组id

func NewAlarmCache() *AlarmCache {
	alarmMu.Lock()
	defer alarmMu.Unlock()
	if alarmCache == nil {
		alarmCache = &AlarmCache{
			client:   global.REDIS,
			expireIn: time.Hour * 24 * 6,
		}
	}
	return alarmCache
}

//	{
//	    "scene_automation_id":"xxx",
//	    "alarm_config_id_list": ["xxx","xxx"],
//	    "alarm_device_id_list":["xxx"]//通过设备配置触发时才保存
//	}
type AlarmCacheGroup struct {
	SceneAutomationId  string   `json:"scene_automation_id"`
	AlarmConfigIdList  []string `json:"alarm_config_id_list"`
	AlaramDeviceIdList []string `json:"alaram_device_id_list"`
	Contents           []string `json:"contents"`
}

func (a *AlarmCacheGroup) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, a)
}

type SliceString []string

func (a *SliceString) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, a)
}

func (*AlarmCache) getCacheKeyByGroupId(group_id string) string {
	return fmt.Sprintf("alarm_cache_group_v6_%s", group_id)
}

func (*AlarmCache) getCacheKeyByDevice(device_id string) string {
	return fmt.Sprintf("alarm_cach_device_v6_%s", device_id)
}

func (*AlarmCache) getCacheKeyByAlarm(alarm_config_id string, device_id string) string {
	return fmt.Sprintf("alarm_cach_alarm_v6_%s_%s", alarm_config_id, device_id)
}

func (*AlarmCache) getCacheKeyByScene(scene_automation_id string) string {
	return fmt.Sprintf("alarm_cach_scene_v6_%s", scene_automation_id)
}

func (a *AlarmCache) set(key string, value interface{}) error {
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
	logrus.Debug(valueStr)
	return a.client.Set(context.Background(), key, valueStr, a.expireIn).Err()
}

// SetDevice
// description 缓存条件中设备信息

func (a *AlarmCache) SetDevice(group_id, scene_automation_id string, device_ids, contents []string) error {
	alarmMu.Lock()
	defer alarmMu.Unlock()
	var info AlarmCacheGroup
	cacheKey := a.getCacheKeyByGroupId(group_id)
	if count, err := a.client.Exists(context.Background(), cacheKey).Result(); err != nil {
		return pkgerrors.Wrap(err, "检查缓存是否存在失败")
	} else if count > 0 {
		err = a.client.Get(context.Background(), cacheKey).Scan(&info)
		if err != nil {
			return pkgerrors.Wrap(err, "获取缓存失败")
		}
		info.Contents = contents
	} else {
		info = AlarmCacheGroup{
			SceneAutomationId:  scene_automation_id,
			AlaramDeviceIdList: device_ids,
			Contents:           contents,
		}
	}
	logrus.Debugf("AlarmCacheGroupSet:%#v", info)
	err := a.set(cacheKey, info)
	if err != nil {
		return err
	}
	for _, device_id := range device_ids {
		cacheKey = a.getCacheKeyByDevice(device_id)
		err = a.groupCacheAdd(cacheKey, group_id)
		if err != nil {
			return err
		}
	}
	cacheKey = a.getCacheKeyByScene(scene_automation_id)
	logrus.Debug("SetDevice:", cacheKey, "==>", group_id)
	return a.groupCacheAdd(cacheKey, group_id)
}

func (a *AlarmCache) GetAlarmDeviceExists(deviceIds []string, groupId string) (bool, error) {
	for _, deviceId := range deviceIds {
		cacheKey := a.getCacheKeyByAlarm(deviceId, groupId)
		var groupIds SliceString
		err := a.client.Get(context.Background(), cacheKey).Scan(&groupIds)
		logrus.Debug("GetAlarmDeviceExists:", cacheKey, "==>", groupIds)
		if err != nil && !errors.Is(err, redis.Nil) {
			return false, err
		}
		for _, g := range groupIds {
			if g == groupId {
				return true, nil
			}
		}
	}
	return false, nil
}

// groupCacheAdd
// description 组缓存添加
func (a *AlarmCache) groupCacheAdd(cacheKey, groupId string) error {
	var groupIds SliceString
	err := a.client.Get(context.Background(), cacheKey).Scan(&groupIds)
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	var isOk bool
	for _, g := range groupIds {
		if g == groupId {
			isOk = true
			break
		}
	}
	//已经存在 就不加入
	if isOk {
		return nil
	}
	groupIds = append(groupIds, groupId)
	err = a.set(cacheKey, groupIds)
	if err != nil {
		return err
	}
	return nil
}
func (a *AlarmCache) name() {

}

// groupCacheDel
// description 组缓存深处

func (a *AlarmCache) groupCacheDel(cachekey, group_id string) error {
	var groupIds SliceString
	err := a.client.Get(context.Background(), cachekey).Scan(&groupIds)
	if err != nil && err != redis.Nil {
		return err
	}
	for i, g := range groupIds {
		if g == group_id {
			groupIds = append(groupIds[:i], groupIds[i+1:]...)
		}
	}
	if len(groupIds) > 0 {
		err = a.set(cachekey, groupIds)
	} else {
		err = a.client.Del(context.Background(), cachekey).Err()
	}

	if err != nil {
		return err
	}
	return nil
}

// SetAlarm
// description 缓存设备告警
func (a *AlarmCache) SetAlarm(group_id string, alarm_config_ids []string, deviceId string) error {
	alarmMu.Lock()
	defer alarmMu.Unlock()
	var info AlarmCacheGroup
	cachekey := a.getCacheKeyByGroupId(group_id)
	err := a.client.Get(context.Background(), cachekey).Scan(&info)
	if err != nil && err != redis.Nil {
		return err
	}
	info.AlarmConfigIdList = alarm_config_ids
	err = a.set(cachekey, info)
	if err != nil {
		return err
	}
	for _, alarm_id := range alarm_config_ids {
		cachekey = a.getCacheKeyByAlarm(alarm_id, deviceId)
		err = a.groupCacheAdd(cachekey, group_id)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetByGroupId
// description 根据groupId获取缓存
func (a *AlarmCache) GetByGroupId(group_id string) (AlarmCacheGroup, error) {
	var info AlarmCacheGroup
	cachekey := a.getCacheKeyByGroupId(group_id)
	err := a.client.Get(context.Background(), cachekey).Scan(&info)
	if err != nil && err != redis.Nil {
		return info, err
	}
	return info, nil
}

// GetByGroupId
// description 根据场景id获取groupId
func (a *AlarmCache) GetBySceneAutomationId(scene_automation_id string) ([]string, error) {
	var groupIds SliceString
	cachekey := a.getCacheKeyByScene(scene_automation_id)
	err := a.client.Get(context.Background(), cachekey).Scan(&groupIds)
	if err != nil && err != redis.Nil {
		return groupIds, err
	}
	return groupIds, nil
}

// DeleteBygroupId
// description 根据groupid删除缓存
func (a *AlarmCache) DeleteBygroupId(group_Id string) error {
	alarmMu.Lock()
	defer alarmMu.Unlock()
	info, err := a.GetByGroupId(group_Id)
	if err != nil {
		return err
	}
	for _, alarmId := range info.AlarmConfigIdList {
		for _, deviceId := range info.AlaramDeviceIdList {
			cachekey := a.getCacheKeyByAlarm(alarmId, deviceId)
			err = a.groupCacheDel(cachekey, group_Id)
			if err != nil {
				return err
			}
		}
	}
	for _, deviceId := range info.AlaramDeviceIdList {
		cachekey := a.getCacheKeyByDevice(deviceId)
		err = a.groupCacheDel(cachekey, group_Id)
		if err != nil {
			return err
		}
		//cachekey = a.getCacheKeyByExecuted(deviceId)
		//err = a.groupCacheDel(cachekey, group_Id)
		//if err != nil {
		//	return err
		//}
	}
	cachekey := a.getCacheKeyByScene(info.SceneAutomationId)
	err = a.groupCacheDel(cachekey, group_Id)
	if err != nil {
		return err
	}

	cacheKey := a.getCacheKeyByGroupId(group_Id)

	return a.client.Del(context.Background(), cacheKey).Err()
}
