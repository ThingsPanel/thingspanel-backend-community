package service

import (
	"project/initialize"
	model "project/internal/model"

	pkgerrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// ActionAfterAlarm
// @description 联动场景执行完成 告警缓存处理
// param actions []model.ActionInfo
// @return error
func ActionAfterAlarm(actions []model.ActionInfo, actionResultErr error) error {
	//查询该场景是否有缓存 无缓存直接跳过
	var scene_automation_id = actions[0].SceneAutomationID
	alarmCache := initialize.NewAlarmCache()
	groupIds, err := alarmCache.GetBySceneAutomationId(scene_automation_id)
	if err != nil {
		return pkgerrors.Wrap(err, "获取缓存失败1")
	}
	if len(groupIds) == 0 {
		return nil
	}
	logrus.Debug("ActionAfterAlarm:", groupIds)
	var alarm_config_ids []string
	//查看动作用是否有警告
	for _, act := range actions {
		if act.ActionType == model.AUTOMATE_ACTION_TYPE_ALARM && act.ActionTarget != nil && *act.ActionTarget != "" {
			alarm_config_ids = append(alarm_config_ids, *act.ActionTarget)
		}
	}
	for _, group_id := range groupIds {
		//没有发现告警服务 删除缓存
		if len(alarm_config_ids) == 0 {
			err = alarmCache.DeleteBygroupId(group_id)
		} else if actionResultErr == nil { //保存缓存 且执行成功 添加执行成功标识
			err = alarmCache.SetAlarm(group_id, alarm_config_ids)
		}
		if err != nil {
			return pkgerrors.Wrap(err, "缓存删除或设置失败")
		}
	}

	return nil
}

// ConditionAfterAlarm
// @description 条件判断后告警业务处理
// param ok bool
// param conditions initialize.DTConditions
// param deviceId string
// @return error
func ConditionAfterAlarm(ok bool, conditions initialize.DTConditions, deviceId string, contents []string) error {
	var (
		device_ids          []string
		group_id            string
		scene_automation_id string
		alarmCache          = initialize.NewAlarmCache()
	)
	for _, cond := range conditions {
		group_id = cond.GroupID
		scene_automation_id = cond.SceneAutomationID
		if cond.TriggerConditionType == model.DEVICE_TRIGGER_CONDITION_TYPE_ONE {
			device_ids = append(device_ids, *cond.TriggerSource)
		}
		if cond.TriggerConditionType == model.DEVICE_TRIGGER_CONDITION_TYPE_MULTIPLE {
			device_ids = append(device_ids, deviceId)
		}
	}
	logrus.Debug("ConditionAfterAlarm:", group_id, device_ids, ok, contents)
	if len(device_ids) == 0 {
		return nil
	}
	//删除缓存 测试删除缓存
	//alarmCache.DeleteBygroupId(group_id)

	//条件通过 添加告警缓存
	if ok {
		err := alarmCache.SetDevice(group_id, scene_automation_id, device_ids, contents)
		if err != nil {
			return pkgerrors.Wrap(err, "缓存设置失败")
		}
		groupIds, _ := alarmCache.GetBySceneAutomationId(scene_automation_id)
		logrus.Debug("getGroupId:", groupIds)
	} else {
		//恢复告警
		err := AlarmRecovery(group_id, contents)
		if err != nil {
			return pkgerrors.WithMessage(err, "恢复告警失败")
		}
		//删除缓存
		err = alarmCache.DeleteBygroupId(group_id)
		if err != nil {
			return pkgerrors.Wrap(err, "缓存设置失败")
		}
		c, _ := alarmCache.GetByGroupId(group_id)
		logrus.Debug("删除后查询: ", c)
	}
	return nil
}

// AlarmExecute
// @description 告警执行执行
// param alarm_config_id string
// param scene_automation_id
// @return bool
func AlarmExecute(alarm_config_id, scene_automation_id string) (bool, string) {
	var (
		alarmName string
		resultOk  bool
	)
	//查询缓存判断 判断告警是否触发
	alarmCache := initialize.NewAlarmCache()
	groupIds, err := alarmCache.GetBySceneAutomationId(scene_automation_id)
	logrus.Debugf("缓存11:%#v,场景id:%#v", groupIds, scene_automation_id)
	if err != nil || len(groupIds) == 0 {
		return resultOk, alarmName
	}
	for _, group_id := range groupIds {
		cache, err := alarmCache.GetByGroupId(group_id)
		if err != nil {
			return resultOk, alarmName
		}
		logrus.Debugf("告警执行前查询: %#v", cache)
		var isOk bool
		for _, acid := range cache.AlarmConfigIdList {
			if acid == alarm_config_id {
				isOk = true
				break
			}
		}
		if isOk {
			continue
		}
		var content string
		content = "场景自动化触发告警"
		for _, strval := range cache.Contents {
			content += ";" + strval
		}
		resultOk, alarmName = GroupApp.AlarmExecute(alarm_config_id, content, scene_automation_id, group_id, cache.AlaramDeviceIdList)
	}
	return resultOk, alarmName
}

// AlarmRecovery
// @description
// param group_id
func AlarmRecovery(group_id string, contents []string) error {
	alarmCache := initialize.NewAlarmCache()
	cache, err := alarmCache.GetByGroupId(group_id)
	if err != nil {
		return err
	}
	logrus.Debug("AlarmRecovery:cache:", cache)
	for _, acid := range cache.AlarmConfigIdList {
		var content string
		content = "场景自动化恢复告警"
		for _, strval := range contents {
			content += ";" + strval
		}
		GroupApp.AlarmRecovery(acid, content, cache.SceneAutomationId, group_id, cache.AlaramDeviceIdList)

	}

	return nil
}
