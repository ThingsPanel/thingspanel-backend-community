package service

import (
	"context"
	"errors"
	"fmt"
	"project/common"
	"project/dal"
	"project/initialize"
	"project/internal/model"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-basic/uuid"
	pkgerrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Automate struct {
	device *model.Device
	mu     sync.Mutex
}

var conditionAfterDecoration = []ConditionAfterFunc{
	ConditionAfterAlarm,
}

var actionAfterDecoration = []ActionAfterFunc{
	ActionAfterAlarm,
}

type ConditionAfterFunc = func(ok bool, conditions initialize.DTConditions, deviceId string, contents []string) error
type ActionAfterFunc = func(actions []model.ActionInfo, err error) error

type AutomateFromExt struct {
	TriggerParamType string
	TriggerParam     []string
}

func (a *Automate) conditionAfterDecorationRun(ok bool, conditions initialize.DTConditions, deviceId string, contents []string) {
	defer a.ErrorRecover()
	for _, fc := range conditionAfterDecoration {
		err := fc(ok, conditions, deviceId, contents)
		if err != nil {
			logrus.Error(err)
		}
	}
}

func (a *Automate) actionAfterDecorationRun(actions []model.ActionInfo, err error) {
	defer a.ErrorRecover()
	for _, fc := range actionAfterDecoration {
		err := fc(actions, err)
		if err != nil {
			logrus.Error(err)
		}
	}
}

func (a *Automate) ErrorRecover() func() {
	return func() {
		if r := recover(); r != nil {
			// 获取当前的调用堆栈
			stack := string(debug.Stack())
			// 打印堆栈信息
			logrus.Error("自动化 执行异常:\n", r, "\nStack trace:\n", stack)
		}
	}
}

// Execute
// @description 遥测设置上报执行自动化（读取缓存信息 缓存无信息数据库查询保存缓存信息）
// @params deviceInfo *model.Device
// @return error
func (a *Automate) Execute(deviceInfo *model.Device, fromExt AutomateFromExt) error {
	defer a.ErrorRecover()
	a.device = deviceInfo
	//

	//单类设备t
	if deviceInfo.DeviceConfigID != nil {
		var deviceConfigId string
		deviceConfigId = *deviceInfo.DeviceConfigID
		err := a.execute(deviceInfo.ID, deviceConfigId, fromExt)
		if err != nil {
			logrus.Error("自动化执行失败", err)
		}
	}
	return a.execute(deviceInfo.ID, "", fromExt)

}

func (a *Automate) execute(deviceId, deviceConfigId string, fromExt AutomateFromExt) error {
	info, resultInt, err := initialize.NewAutomateCache().GetCacheByDeviceId(deviceId, deviceConfigId)
	logrus.Debugf("info:%#v, resultInt:%d", info, resultInt)
	if err != nil {
		return pkgerrors.Wrap(err, "查询缓存信息失败")
	}
	//当前设备没自动化任务
	if resultInt == initialize.AUTOMATE_CACHE_RESULT_NOT_TASK {
		return nil
	}
	//缓存未查询到数据 数据查询存入缓存
	if resultInt == initialize.AUTOMATE_CACHE_RESULT_NOT_FOUND {
		info, resultInt, err = a.QueryAutomateInfoAndSetCache(deviceId, deviceConfigId)
		if err != nil {
			return pkgerrors.Wrap(err, "查询设置 设置缓存失败")
		}
		//当前设备没自动化任务
		if resultInt == initialize.AUTOMATE_CACHE_RESULT_NOT_TASK {
			return nil
		}
	}
	//过滤自动化触发条件
	info = a.AutomateFilter(info, fromExt)
	//执行自动化
	return a.ExecuteRun(info)
}
func (a *Automate) AutomateFilter(info initialize.AutomateExecteParams, fromExt AutomateFromExt) initialize.AutomateExecteParams {
	var sceneInfo []initialize.AutomateExecteSceneInfo
	for _, scene := range info.AutomateExecteSceeInfos {
		var isExists bool
		for _, cond := range scene.GroupsCondition {
			if cond.TriggerParamType == nil || cond.TriggerParam == nil {
				continue
			}
			condTriggerParamType := strings.ToUpper(*cond.TriggerParamType)
			switch fromExt.TriggerParamType {
			case model.TRIGGER_PARAM_TYPE_TEL:
				if condTriggerParamType == model.TRIGGER_PARAM_TYPE_TEL || condTriggerParamType == model.TRIGGER_PARAM_TYPE_TELEMETRY {
					if a.containString(fromExt.TriggerParam, *cond.TriggerParam) {
						isExists = true
					}
				}
			case model.TRIGGER_PARAM_TYPE_STATUS:
				if condTriggerParamType == model.TRIGGER_PARAM_TYPE_STATUS {
					isExists = true
				}
			case model.TRIGGER_PARAM_TYPE_EVT:
				if condTriggerParamType == model.TRIGGER_PARAM_TYPE_EVT && a.containString(fromExt.TriggerParam, *cond.TriggerParam) {
					isExists = true
				}
			case model.TRIGGER_PARAM_TYPE_ATTR:
				if condTriggerParamType == model.TRIGGER_PARAM_TYPE_ATTR && a.containString(fromExt.TriggerParam, *cond.TriggerParam) {
					isExists = true
				}
			}
		}
		if isExists {
			sceneInfo = append(sceneInfo, scene)
		}
	}
	info.AutomateExecteSceeInfos = sceneInfo
	return info
}

func (a *Automate) containString(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}

// 限流实现 1秒一次 安场景实现
func (a *Automate) LimiterAllow(id string) bool {
	return initialize.NewAutomateLimiter().GetLimiter(fmt.Sprintf("SceneAutomationId:%s", id)).Allow()
}

// ExecuteRun
// @description  自动化场景联动执行
// @params info initialize.AutomateExecteParams
// @return error
func (a *Automate) ExecuteRun(info initialize.AutomateExecteParams) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, v := range info.AutomateExecteSceeInfos {
		//场景频率限制(根据场景id)
		if !a.LimiterAllow(v.SceneAutomationId) {
			continue
		}
		//查询自动化是否关闭
		if a.CheckSceneAutomationHasClose(v.SceneAutomationId) {
			continue
		}
		//条件判断
		if !a.AutomateConditionCheck(v.GroupsCondition, info.DeviceId) {
			continue
		}
		// 场景联动 动作执行
		err := a.SceneAutomateExecute(v.SceneAutomationId, []string{info.DeviceId}, v.Actions)
		// 场景动作之后装饰
		a.actionAfterDecorationRun(v.Actions, err)
	}

	return nil
}

// CheckSceneAutomationHasClose
// @description 查询是否关闭了自动化
func (a *Automate) CheckSceneAutomationHasClose(sceneAutomationId string) bool {
	ok := dal.CheckSceneAutomationHasClose(sceneAutomationId)
	//删除缓存
	if ok {
		_ = initialize.NewAutomateCache().DeleteCacheBySceneAutomationId(sceneAutomationId)
	}
	return ok
}

// SceneAutomateExecute
// @description 场景联动 动作执行
// @params info initialize.AutomateExecteParams
// @return error
func (a *Automate) SceneAutomateExecute(sceneAutomationId string, deviceIds []string, actions []model.ActionInfo) error {
	tenantID := dal.GetSceneAutomationTenantID(context.Background(), sceneAutomationId)

	//执行动作
	details, err := a.AutomateActionExecute(sceneAutomationId, deviceIds, actions, tenantID)

	_ = a.sceneExecuteLogSave(sceneAutomationId, details, err)

	return err
}

// ActiveSceneExecute
// @description 场景激活
// @params info initialize.AutomateExecteParams
// @return error
func (a *Automate) ActiveSceneExecute(scene_id, tenantID string) error {

	actions, err := dal.GetActionInfoListBySceneId([]string{scene_id})
	if err != nil {
		return nil
	}
	var (
		deviceIds      []string
		deviceConfigId []string
	)
	for _, v := range actions {
		if v.ActionType == model.AUTOMATE_ACTION_TYPE_MULTIPLE && v.ActionTarget != nil {
			deviceConfigId = append(deviceConfigId, *v.ActionTarget)
		}
	}
	if len(deviceConfigId) > 0 {
		deviceIds, err = dal.GetDeviceIdsByDeviceConfigId(deviceConfigId)
		if err != nil {
			return err
		}
	}
	details, err := a.AutomateActionExecute(scene_id, deviceIds, actions, tenantID)
	var exeResult string
	if err == nil {
		exeResult = "S"
	} else {
		exeResult = "F"
	}
	logrus.Debug(details)
	return dal.SceneLogInsert(&model.SceneLog{
		ID:              uuid.New(),
		SceneID:         scene_id,
		ExecutedAt:      time.Now().UTC(),
		Detail:          details,
		ExecutionResult: exeResult,
		TenantID:        tenantID,
	})
}

// @description sceneExecuteLogSave 自动化场景联动执行
// @params info initialize.AutomateExecteParams
// @return error
func (a *Automate) sceneExecuteLogSave(scene_id, details string, err error) error {
	var exeResult string
	if err == nil {
		exeResult = "S"
	} else {
		exeResult = "F"
	}
	logrus.Debug(details)
	return dal.SceneAutomationLogInsert(&model.SceneAutomationLog{
		SceneAutomationID: scene_id,
		ExecutedAt:        time.Now().UTC(),
		Detail:            details,
		ExecutionResult:   exeResult,
		TenantID:          dal.GetSceneAutomationTenantID(context.Background(), scene_id),
	})
}

// AutomateConditionCheck
// @description  自动化条件判断 复合其中一组条件就返回true
// @params conditions []initialize.DTConditions
// @return bool true 表示可以执行动作
func (a *Automate) AutomateConditionCheck(conditions initialize.DTConditions, deviceId string) bool {
	logrus.Debug("条件判断开始...")
	//key是groupId val是条件列表
	conditionsByGroupId := make(map[string]initialize.DTConditions)
	for _, v := range conditions {
		conditionsByGroupId[v.GroupID] = append(conditionsByGroupId[v.GroupID], v)
	}
	var result bool
	for _, val := range conditionsByGroupId {
		ok, contents := a.AutomateConditionCheckWithGroup(val, deviceId)
		if ok {
			result = true
		}
		//组条件执行完成装饰
		a.conditionAfterDecorationRun(ok, val, deviceId, contents)
	}
	return result
}

// AutomateConditionCheckWithGroup
// @description  一组条件比较 一个为假结果就为假
// @params conditions initialize.DTConditions
// @return bool
func (a *Automate) AutomateConditionCheckWithGroup(conditions initialize.DTConditions, deviceId string) (bool, []string) {
	var (
		result   []string
		resultOk bool = true
	)
	for _, val := range conditions {
		ok, content := a.AutomateConditionCheckWithGroupOne(val, deviceId)
		result = append(result, content)
		if !ok {
			resultOk = false
			break
		}
	}

	return resultOk, result
}

// @description AutomateConditionCheckWithGroupOne 单个条件验证
// @params cond model.DeviceTriggerCondition
// @return bool
func (a *Automate) AutomateConditionCheckWithGroupOne(cond model.DeviceTriggerCondition, deviceId string) (bool, string) {
	logrus.Debug("条件type:", cond.TriggerConditionType)
	switch cond.TriggerConditionType {
	case model.DEVICE_TRIGGER_CONDITION_TYPE_TIME:
		return a.automateConditionCheckWithTime(cond), ""
	case model.DEVICE_TRIGGER_CONDITION_TYPE_ONE, model.DEVICE_TRIGGER_CONDITION_TYPE_MULTIPLE:
		return a.automateConditionCheckWithDevice(cond, deviceId)
	default:
		return true, ""
	}
}

// @description automateConditionCheckWithTime 单个条件时间范围验证
// @params cond model.DeviceTriggerCondition
// @return bool
func (a *Automate) automateConditionCheckWithTime(cond model.DeviceTriggerCondition) bool {
	logrus.Debug("时间范围对比开始... 条件:", cond.TriggerValue)
	nowTime := time.Now().UTC()
	if cond.TriggerValue == "" {
		return false
	}
	valParts := strings.Split(cond.TriggerValue, "|")
	if len(valParts) < 3 {
		return false
	}
	var ok bool
	//获取当前星期
	weekDay := common.GetWeekDay(nowTime)
	//判断当前时间和条件星期
	for _, char := range valParts[0] {
		num, _ := strconv.Atoi(string(char))
		if weekDay == num {
			ok = true
			continue
		}
	}
	//没有在当前指定的星期中
	if !ok {
		return false
	}
	nowTimeNotDay, _ := time.Parse("15:04:05-07:00", nowTime.Format("15:04:05-07:00"))
	startTime, err := time.Parse("15:04:05-07:00", valParts[1])
	if err != nil {
		logrus.Error("时间格式不正确, 字符串", cond.TriggerValue)
		return false
	}
	if startTime.After(nowTimeNotDay) {
		return false
	}

	endTime, err := time.Parse("15:04:05-07:00", valParts[2])
	if err != nil {
		logrus.Error("时间格式不正确, 字符串", cond.TriggerValue)
		return false
	}
	if endTime.Before(nowTimeNotDay) {
		return false
	}
	logrus.Debug("时间范围对比结束。OK")
	return true
}

func (a *Automate) automateConditionCheckWithDevice(cond model.DeviceTriggerCondition, deviceId string) (bool, string) {
	logrus.Debug("设备条件验证开始...")
	//设备id不存在 返回假
	if cond.TriggerSource == nil {
		return false, ""
	}
	//单类设置 获取上报的设置 单个设置 使用设置的设备id
	if cond.TriggerConditionType == model.DEVICE_TRIGGER_CONDITION_TYPE_ONE {
		deviceId = *cond.TriggerSource
	}

	//条件查询
	var (
		actualValue     string
		trigger         string
		triggerValue    string
		triggerOperator string
		triggerKey      string
		result          string
		deviceName      string
	)
	if a.device.Name != nil {
		deviceName = *a.device.Name
	}
	if cond.TriggerOperator == nil {
		triggerOperator = "="
	} else {
		triggerOperator = *cond.TriggerOperator
	}

	logrus.Debug("设备条件验证开始...", strings.ToUpper(*cond.TriggerParamType))
	switch strings.ToUpper(*cond.TriggerParamType) {
	case model.TRIGGER_PARAM_TYPE_TEL, model.TRIGGER_PARAM_TYPE_TELEMETRY: //遥测
		trigger = "遥测"
		actualValue, _ = dal.GetCurrentTelemetryDataOneKeys(deviceId, *cond.TriggerParam)
		triggerValue = cond.TriggerValue
		triggerKey = *cond.TriggerParam
		logrus.Debug("GetCurrentTelemetryDataOneKeys:.", triggerOperator, *cond.TriggerParam, triggerValue, actualValue)
		dataValue := a.getTriggerParamsValue(triggerKey, dal.GetIdentifierNameTelemetry())
		result = fmt.Sprintf("设备(%s)%s [%s]: %s %s %s", deviceName, trigger, dataValue, actualValue, triggerOperator, triggerValue)
	case model.TRIGGER_PARAM_TYPE_ATTR: //属性
		trigger = "属性"
		actualValue, _ = dal.GetAttributeOneKeys(deviceId, *cond.TriggerParam)
		triggerValue = cond.TriggerValue
		triggerKey = *cond.TriggerParam
		dataValue := a.getTriggerParamsValue(triggerKey, dal.GetIdentifierNameAttribute())
		result = fmt.Sprintf("设备(%s)%s [%s]: %s %s %s", deviceName, trigger, dataValue, actualValue, triggerOperator, triggerValue)
	case model.TRIGGER_PARAM_TYPE_EVT: //事件
		trigger = "事件"
		actualValue, _ = dal.GetDeviceEventOneKeys(deviceId, *cond.TriggerParam)
		triggerValue = cond.TriggerValue
		triggerKey = *cond.TriggerParam
		dataValue := a.getTriggerParamsValue(triggerKey, dal.GetIdentifierNameEvent())
		result = fmt.Sprintf("设备(%s)%s [%s]: %s %s %s", deviceName, trigger, dataValue, actualValue, triggerOperator, triggerValue)
	case model.TRIGGER_PARAM_TYPE_STATUS: //状态
		trigger = "下线"
		actualValue, _ = dal.GetDeviceCurrentStatus(deviceId)
		triggerValue = *cond.TriggerParam
		if strings.ToUpper(actualValue) == "ON-LINE" {
			trigger = "上线"
		}
		result = fmt.Sprintf("设备(%s)已%s", deviceName, trigger)
		triggerOperator = "="
	}
	logrus.Debug("automateConditionCheckByOperator:设备条件验证参数...", triggerOperator, triggerValue, actualValue)
	ok := a.automateConditionCheckByOperator(triggerOperator, triggerValue, actualValue)

	return ok, result
}

type DataIdentifierName func(device_template_id, identifier string) string

func (a *Automate) getTriggerParamsValue(triggerKey string, fc DataIdentifierName) string {
	tempId, _ := dal.GetDeviceTemplateIdByDeviceId(triggerKey)
	if tempId == "" {
		return triggerKey
	}

	return fc(tempId, triggerKey)
}

// automateConditionCheckByOperator
// @description  运算符处理
// @params cond model.DeviceTriggerCondition
// @return bool
func (a *Automate) automateConditionCheckByOperator(operator string, condValue string, actualValue string) bool {
	logrus.Warningf("比较:operator:%s, condValue:%s, actualValue: %s, result:%d", operator, condValue, actualValue, strings.Compare(actualValue, condValue))
	switch operator {
	case model.CONDITION_TRIGGER_OPERATOR_EQ:
		return strings.EqualFold(strings.ToUpper(actualValue), strings.ToUpper(condValue))
	case model.CONDITION_TRIGGER_OPERATOR_NEQ:
		return strings.Compare(actualValue, condValue) != 0
	case model.CONDITION_TRIGGER_OPERATOR_GT:
		actualValueFloat64, err := strconv.ParseFloat(actualValue, 64)
		if err != nil {
			return strings.Compare(actualValue, condValue) > 0
		}
		condValueFloat64, err := strconv.ParseFloat(condValue, 64)
		if err != nil {
			return strings.Compare(actualValue, condValue) > 0
		}
		return actualValueFloat64 > condValueFloat64
	case model.CONDITION_TRIGGER_OPERATOR_LT:
		actualValueFloat64, err := strconv.ParseFloat(actualValue, 64)
		if err != nil {
			return strings.Compare(actualValue, condValue) < 0
		}
		condValueFloat64, err := strconv.ParseFloat(condValue, 64)
		if err != nil {
			return strings.Compare(actualValue, condValue) < 0
		}
		return actualValueFloat64 < condValueFloat64
	case model.CONDITION_TRIGGER_OPERATOR_GTE:
		actualValueFloat64, err := strconv.ParseFloat(actualValue, 64)
		if err != nil {
			return strings.Compare(actualValue, condValue) >= 0
		}
		condValueFloat64, err := strconv.ParseFloat(condValue, 64)
		if err != nil {
			return strings.Compare(actualValue, condValue) >= 0
		}
		return actualValueFloat64 >= condValueFloat64
	case model.CONDITION_TRIGGER_OPERATOR_LTE:
		actualValueFloat64, err := strconv.ParseFloat(actualValue, 64)
		if err != nil {
			return strings.Compare(actualValue, condValue) <= 0
		}
		condValueFloat64, err := strconv.ParseFloat(condValue, 64)
		if err != nil {
			return strings.Compare(actualValue, condValue) <= 0
		}
		return actualValueFloat64 <= condValueFloat64
	case model.CONDITION_TRIGGER_OPERATOR_BETWEEN:
		valParts := strings.Split(condValue, "-")
		if len(valParts) != 2 {
			return false
		}
		actualValueFloat64, err := strconv.ParseFloat(actualValue, 64)
		if err != nil {
			return actualValue >= valParts[0] && actualValue <= valParts[1]
		}
		valParts0Float64, err := strconv.ParseFloat(valParts[0], 64)
		if err != nil {
			return actualValue >= valParts[0] && actualValue <= valParts[1]
		}
		valParts1Float64, err := strconv.ParseFloat(valParts[1], 64)
		if err != nil {
			return actualValue >= valParts[0] && actualValue <= valParts[1]
		}
		return actualValueFloat64 >= valParts0Float64 && actualValueFloat64 <= valParts1Float64
	case model.CONDITION_TRIGGER_OPERATOR_IN:
		valParts := strings.Split(condValue, ",")
		for _, v := range valParts {
			if v == actualValue {
				return true
			}
		}
	}
	return false
}

// AutomateActionExecute
// @description  自动化动作执行
// @params deviceId string
// @params actions []model.ActionInf
// @return void
func (a *Automate) AutomateActionExecute(sceneId string, deviceIds []string, actions []model.ActionInfo, tenantID string) (string, error) {
	logrus.Debug("动作开始执行:")
	var (
		result    string
		resultErr error
	)
	if len(actions) == 0 {
		return "未找到执行动作", errors.New("未找到执行动作")
	}
	for _, action := range actions {
		var actionService AutomateTelemetryAction
		logrus.Debug("actionType:", action.ActionType)
		switch action.ActionType {
		case model.AUTOMATE_ACTION_TYPE_ONE: //单个设置
			actionService = &AutomateTelemetryActionOne{TenantID: tenantID}
		case model.AUTOMATE_ACTION_TYPE_ALARM: //告警触发
			actionService = &AutomateTelemetryActionAlarm{}
		case model.AUTOMATE_ACTION_TYPE_MULTIPLE: //单类设置
			actionService = &AutomateTelemetryActionMultiple{DeviceIds: deviceIds, TenantID: tenantID}
		case model.AUTOMATE_ACTION_TYPE_SCENE: //激活场景
			actionService = &AutomateTelemetryActionScene{TenantID: tenantID}
		case model.AUTOMATE_ACTION_TYPE_SERVICE: //服务
			actionService = &AutomateTelemetryActionService{}
		}
		if actionService == nil {
			logrus.Error("暂不支持的动作类型")
			return "暂不支持的动作类型", errors.New("暂不支持的动作类型")
		}
		// go func(actionService AutomateTelemetryAction, action model.ActionInfo) {
		// 	actionService.AutomateActionRun(action)
		// }(actionService, action)
		actionMessage, err := actionService.AutomateActionRun(action)
		if err != nil && resultErr == nil {
			resultErr = err
		}
		if err != nil {
			result += fmt.Sprintf("%s 执行失败;", actionMessage)
		} else {
			result += fmt.Sprintf("%s 执行成功;", actionMessage)
		}
	}
	logrus.Debug("result:", result)
	return result, resultErr
}

// QueryAutomateInfoAndSetCache
// @description  查询设备自动化信息并缓存
// @params deviceId string
// @return initialize.AutomateExecteParams, int, error
func (a *Automate) QueryAutomateInfoAndSetCache(deviceId, deviceConfigId string) (initialize.AutomateExecteParams, int, error) {
	automateExecuteParams := initialize.AutomateExecteParams{
		DeviceId:       deviceId,
		DeviceConfigId: deviceConfigId,
	}
	var (
		groups []model.DeviceTriggerCondition
		err    error
	)
	//deviceConfigId 存在 表示单类设置
	if deviceConfigId != "" {
		groups, err = dal.GetDeviceTriggerConditionByDeviceId(deviceConfigId, model.DEVICE_TRIGGER_CONDITION_TYPE_MULTIPLE)
	} else {
		groups, err = dal.GetDeviceTriggerConditionByDeviceId(deviceId, model.DEVICE_TRIGGER_CONDITION_TYPE_ONE)
	}
	if err != nil {
		return automateExecuteParams, 0, pkgerrors.Wrap(err, "根据设备id查询自动化条件失败")
	}
	//没有查询到该设备自动化信息
	if len(groups) == 0 {
		err := initialize.NewAutomateCache().SetCacheByDeviceIdWithNoTask(deviceId, deviceConfigId)
		if err != nil {
			return automateExecuteParams, 0, pkgerrors.Wrap(err, "设置设备无自动化信息缓存失败")
		}
		return automateExecuteParams, initialize.AUTOMATE_CACHE_RESULT_NOT_TASK, nil
	}
	sceneAutomateGroups := make(map[string]bool)
	var (
		sceneAutomateIds []string
		groupIds         []string
	)

	for _, groupInfo := range groups {
		if _, ok := sceneAutomateGroups[groupInfo.SceneAutomationID]; !ok {
			sceneAutomateIds = append(sceneAutomateIds, groupInfo.SceneAutomationID)
			sceneAutomateGroups[groupInfo.SceneAutomationID] = true
		}
		groupIds = append(groupIds, groupInfo.GroupID)
	}
	//查询场景所有的group条件
	groups, err = dal.GetDeviceTriggerConditionByGroupIds(groupIds)
	if err != nil {
		return automateExecuteParams, 0, pkgerrors.Wrap(err, "查询自动化条件失败")
	}
	//查询场景执行动作
	actionInfos, err := dal.GetActionInfoListBySceneAutomationId(sceneAutomateIds)
	if err != nil {
		return automateExecuteParams, 0, pkgerrors.Wrap(err, "查询自动化执行失败")
	}
	//设置自动化缓存
	err = initialize.NewAutomateCache().SetCacheByDeviceId(deviceId, deviceConfigId, groups, actionInfos)
	if err != nil {
		return automateExecuteParams, 0, pkgerrors.Wrap(err, "设置自动化缓存失败")
	}

	return initialize.NewAutomateCache().GetCacheByDeviceId(deviceId, deviceConfigId)
}
