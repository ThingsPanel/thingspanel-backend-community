package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"project/initialize"
	"project/internal/dal"
	model "project/internal/model"
	"project/pkg/constant"

	"github.com/sirupsen/logrus"
)

const (
	AUTOMATE_ACTION_PARAM_TYPE_TEL          = "TEL"         // 遥测
	AUTOMATE_ACTION_PARAM_TYPE_TELEMETRY    = "telemetry"   // 遥测
	AUTOMATE_ACTION_PARAM_TYPE_C_TELEMETRY  = "c_telemetry" // 遥测
	AUTOMATE_ACTION_PARAM_TYPE_ATTR         = "ATTR"        // 属性设置
	AUTOMATE_ACTION_PARAM_TYPE_ATTRIBUTES   = "attributes"  // 属性设置
	AUTOMATE_ACTION_PARAM_TYPE_C_ATTRIBUTES = "c_attribute" // 属性设置
	AUTOMATE_ACTION_PARAM_TYPE_CMD          = "CMD"         // 命令下发
	AUTOMATE_ACTION_PARAM_TYPE_COMMAND      = "command"     // 命令下发
	AUTOMATE_ACTION_PARAM_TYPE_C_COMMAND    = "c_command"   // 命令下发
)

// 自动化场景动作执行接口
type AutomateTelemetryAction interface {
	AutomateActionRun(model.ActionInfo) (string, error)
}

func AutomateActionDeviceMqttSend(deviceId string, action model.ActionInfo, tenantID string) (string, error) {
	var executeMsg string
	// 获取设备缓存信息
	deviceInfo, err := initialize.GetDeviceCacheById(deviceId)
	if err != nil {
		executeMsg = fmt.Sprintf("设备id:%s", deviceId)
	} else {
		executeMsg = fmt.Sprintf("设备名称:%s", *deviceInfo.Name)
	}

	if action.ActionParamType == nil {
		return executeMsg + " ActionParamType不存在", errors.New("ActionParamType不存在")
	}
	if action.ActionValue == nil {
		return executeMsg + " 动作目标值不存在", errors.New("动作目标值不存在")
	}
	// 如果是自定义没有标识符
	// if action.ActionParam == nil {
	// 	return executeMsg + " 标识符不存在", errors.New("标识符不存在")
	// }
	ctx := context.Background()

	var userId string
	userId, _ = dal.GetUserIdBYTenantID(tenantID)
	logrus.Debug("AutomateActionDeviceMqttSend:", tenantID, ", userId:", userId)
	operationType := strconv.Itoa(constant.Auto)
	// var valueMap = make(map[string]string)
	switch *action.ActionParamType {
	case AUTOMATE_ACTION_PARAM_TYPE_TEL, AUTOMATE_ACTION_PARAM_TYPE_TELEMETRY, AUTOMATE_ACTION_PARAM_TYPE_C_TELEMETRY:
		msgReq := model.PutMessage{
			DeviceID: deviceId,
		}
		//valueMap = map[string]string{
		//	*action.ActionParam: *action.ActionValue,
		//}
		//valueStr, _ := json.Marshal(valueMap)
		//msgReq.Value = string(valueStr)
		msgReq.Value = *action.ActionValue
		logrus.Warning(msgReq)
		return executeMsg + fmt.Sprintf(" 遥测指令:%s", msgReq.Value), GroupApp.TelemetryData.TelemetryPutMessage(ctx, userId, &msgReq, operationType)

	case AUTOMATE_ACTION_PARAM_TYPE_ATTR, AUTOMATE_ACTION_PARAM_TYPE_ATTRIBUTES, AUTOMATE_ACTION_PARAM_TYPE_C_ATTRIBUTES:
		msgReq := model.AttributePutMessage{
			DeviceID: deviceId,
		}
		//valueMap = map[string]string{
		//	*action.ActionParam: *action.ActionValue,
		//}
		//valueStr, _ := json.Marshal(valueMap)
		//msgReq.Value = string(valueStr)
		msgReq.Value = *action.ActionValue
		return executeMsg + fmt.Sprintf(" 属性设置:%s", msgReq.Value), GroupApp.AttributeData.AttributePutMessage(ctx, userId, &msgReq, operationType)

	case AUTOMATE_ACTION_PARAM_TYPE_CMD, AUTOMATE_ACTION_PARAM_TYPE_COMMAND, AUTOMATE_ACTION_PARAM_TYPE_C_COMMAND:
		type commandInfo struct {
			Method string      `json:"method"`
			Params interface{} `json:"params"`
		}
		var info commandInfo
		err := json.Unmarshal([]byte(*action.ActionValue), &info)
		if err != nil {
			return executeMsg + "命令下发解析数据失败", err
		}
		value, _ := json.Marshal(info.Params)
		valueStr := string(value)
		msgReq := model.PutMessageForCommand{
			DeviceID: deviceId,
			Value:    &valueStr,
			Identify: info.Method,
		}
		//msgReq := model.PutMessageForCommand{
		//	DeviceID: deviceId,
		//	Value:    action.ActionValue,
		//	Identify: *action.ActionParam,
		//}
		return executeMsg + fmt.Sprintf(" 命令下发:%s", *msgReq.Value), GroupApp.CommandData.CommandPutMessage(ctx, userId, &msgReq, operationType)
	default:

		return executeMsg + "不支持的类型", errors.New("不支持的类型")
	}
}

// 单个设备 10
type AutomateTelemetryActionOne struct {
	TenantID string
}

func (a *AutomateTelemetryActionOne) AutomateActionRun(action model.ActionInfo) (string, error) {
	if action.ActionTarget == nil {
		return "单设备执行，设备id不存在", errors.New("设备id不存在")
	}
	return AutomateActionDeviceMqttSend(*action.ActionTarget, action, a.TenantID)
}

// 单类设备 11
type AutomateTelemetryActionMultiple struct {
	DeviceIds []string
	TenantID  string
}

func (a *AutomateTelemetryActionMultiple) AutomateActionRun(action model.ActionInfo) (string, error) {
	var (
		messages []string
		errs     error
	)
	for _, deviceId := range a.DeviceIds {
		msg, err := AutomateActionDeviceMqttSend(deviceId, action, a.TenantID)
		if err != nil && errs == nil {
			errs = err
		}
		messages = append(messages, msg)
	}

	return "单类设置:" + fmt.Sprintf("%s", messages), errs
}

// 激活场景 20
type AutomateTelemetryActionScene struct {
	TenantID string
}

func (a *AutomateTelemetryActionScene) AutomateActionRun(action model.ActionInfo) (string, error) {
	if action.ActionTarget == nil {
		return "场景激活", errors.New("场景id不存在")
	}
	// 获取场景信息
	sceneInfo, err := dal.GetSceneInfo(*action.ActionTarget)
	if err != nil {
		return "场景激活", err
	}
	return fmt.Sprintf("场景激活:%s", sceneInfo.Name), GroupApp.ActiveSceneExecute(*action.ActionTarget, a.TenantID)
}

// 警告 30
type AutomateTelemetryActionAlarm struct {
	DeviceIds []string
}

func (*AutomateTelemetryActionAlarm) AutomateActionRun(action model.ActionInfo) (string, error) {
	logrus.Debugf("告警服务: %#v", *action.ActionTarget)
	// 告警服务 有装饰器实现 这里不做处理
	if action.ActionTarget == nil || *action.ActionTarget == "" {
		return "告警服务", errors.New("告警id不存在")
	}
	ok, alarmName, reason := AlarmExecute(*action.ActionTarget, action.SceneAutomationID)
	if ok {
		return fmt.Sprintf("告警服务(%s)", alarmName), nil
	}

	// 获取告警名称，无论是否执行成功都可以显示
	alarmName = dal.GetAlarmNameWithCache(*action.ActionTarget)

	// 处理告警已存在的情况 - 不将其标记为错误
	if reason == "告警已存在" {
		logrus.Debugf("告警(%s)已存在，不再重复触发", alarmName)
		return fmt.Sprintf("告警服务(%s) - 告警已存在，不再重复触发", alarmName), nil
	}

	// 其他失败情况
	errRsp := errors.New("执行失败," + reason)
	return fmt.Sprintf("告警服务(%s)", alarmName), errRsp
}

// 服务 40
type AutomateTelemetryActionService struct{}

func (*AutomateTelemetryActionService) AutomateActionRun(_ model.ActionInfo) (string, error) {
	// todo 待实现
	fmt.Println("自动化服务动作实现")
	return "服务", nil
}
