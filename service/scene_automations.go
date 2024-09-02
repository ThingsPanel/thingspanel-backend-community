package service

import (
	"fmt"
	"project/dal"
	"project/initialize"
	model "project/internal/model"
	utils "project/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type SceneAutomation struct{}

func (s *SceneAutomation) CreateSceneAutomation(req *model.CreateSceneAutomationReq, u *utils.UserClaims) (string, error) {

	var scene_automation_id string

	// 开启事物
	logrus.Info("开启事物")
	tx, err := dal.StartTransaction()
	if err != nil {
		return scene_automation_id, err
	}

	// 写入 scene_automations
	var sceneAutomation = model.SceneAutomation{}
	sceneAutomation.ID = uuid.New()

	scene_automation_id = sceneAutomation.ID

	sceneAutomation.Name = req.Name
	sceneAutomation.Description = &req.Description
	//sceneAutomation.Enabled = req.Enabled
	sceneAutomation.Enabled = "N"
	sceneAutomation.TenantID = u.TenantID
	sceneAutomation.Creator = u.ID
	sceneAutomation.Updator = u.ID
	sceneAutomation.CreatedAt = utils.GetUTCTime()
	sceneAutomation.UpdatedAt = &sceneAutomation.CreatedAt
	sceneAutomation.Remark = &req.Remark
	// 创建场景联动
	logrus.Info("创建场景联动信息")
	err = dal.CreateSceneAutomation(&sceneAutomation, tx)
	if err != nil {
		dal.Rollback(tx)
		return "", err
	}

	for _, v := range req.TriggerConditionGroups {
		groupId := uuid.New()
		for _, v2 := range v {

			switch v2.TriggerConditionsType {
			case "10", "11", "22":
				// 写入 device_trigger_condition
				var dtc = model.DeviceTriggerCondition{}
				dtc.ID = uuid.New()
				dtc.SceneAutomationID = scene_automation_id
				dtc.Enabled = req.Enabled
				dtc.GroupID = groupId
				dtc.TriggerConditionType = v2.TriggerConditionsType
				dtc.TriggerSource = v2.TriggerSource
				dtc.TriggerParamType = v2.TriggerParamType
				dtc.TriggerParam = v2.TriggerParam
				dtc.TriggerOperator = v2.TriggerOperator
				if v2.TriggerValue != nil {
					dtc.TriggerValue = *v2.TriggerValue
				}
				dtc.Enabled = req.Enabled
				dtc.TenantID = u.TenantID
				// 创建设备触发条件
				logrus.Info("创建设备触发条件信息")
				err = dal.CreateDeviceTriggerCondition(dtc, tx)
				if err != nil {
					dal.Rollback(tx)
					return "", err
				}

			case "20":
				// 写入 one_time_tasks
				var ott = model.OneTimeTask{}
				ott.ID = uuid.New()
				ott.SceneAutomationID = scene_automation_id
				if v2.ExecutionTime != nil {
					ott.ExecutionTime = *v2.ExecutionTime
				}

				// if v2.ExecutionTime != nil {
				// 	orgTime := *v2.ExecutionTime
				// 	intOrgTime := orgTime.Unix()
				// 	ott.ExpirationTime = intOrgTime
				// }
				if v2.ExpirationTime != nil {
					ott.ExpirationTime = int64(*v2.ExpirationTime)
				}
				ott.ExecutingState = "NEX"
				ott.Enabled = req.Enabled
				// 创建一次性任务
				logrus.Info("创建一次性任务信息")
				err = dal.CreateOneTimeTask(ott, tx)
				if err != nil {
					dal.Rollback(tx)
					return "", err
				}
			case "21":
				// 写入periodic_tasks
				var pt = model.PeriodicTask{}
				pt.ID = uuid.New()
				pt.SceneAutomationID = scene_automation_id
				if v2.TaskType != nil {
					pt.TaskType = *v2.TaskType
				}
				if v2.Params != nil {
					pt.Param = *v2.Params
				}
				if v2.ExecutionTime != nil {
					pt.ExecutionTime = *v2.ExecutionTime
				}

				if v2.ExpirationTime != nil {
					pt.ExpirationTime = int64(*v2.ExpirationTime)
				}
				//pt.Enabled = req.Enabled
				pt.Enabled = "Y"
				// 创建周期性任务
				logrus.Info("创建周期性任务信息")
				err = dal.CreatePeriodicTask(pt, tx)
				if err != nil {
					dal.Rollback(tx)
					return "", err
				}
			default:
				dal.Rollback(tx)
				return "", fmt.Errorf("not support")
			}

		}
	}

	for _, v := range req.Actions {
		// 写入 action_info
		var actionInfo = model.ActionInfo{}
		actionInfo.ID = uuid.New()
		actionInfo.SceneAutomationID = scene_automation_id
		actionInfo.ActionTarget = &v.ActionTarget
		actionInfo.ActionType = v.ActionType
		actionInfo.ActionParamType = &v.ActionParamType
		actionInfo.ActionParam = &v.ActionParam
		actionInfo.ActionValue = &v.ActionValue
		// 创建动作信息
		logrus.Info("创建动作信息")
		err = dal.CreateActionInfo(actionInfo, tx)
		if err != nil {
			dal.Rollback(tx)
			return "", err
		}

	}

	dal.Commit(tx)
	//保存自动化缓存信息
	go func() {
		if req.Enabled == "Y" {
			err := s.AutomateCacheSet(scene_automation_id)
			if err != nil {
				logrus.Error("新建场景联动保存自动换缓存信息失败，err:", err)
			}
		}
	}()
	return scene_automation_id, nil
}

// AutomateCacheSet 保存自动化缓存信息
func (s *SceneAutomation) AutomateCacheSet(scene_automation_id string) error {
	logrus.Info("开始保存自动化缓存信息")
	groupInfoPtrs, err := dal.GetDeviceTriggerCondition(scene_automation_id)
	if err != nil {
		return err
	}
	actionInfoPtrs, err := dal.GetActionInfo(scene_automation_id)
	if err != nil {
		return err
	}
	var groupInfos []model.DeviceTriggerCondition
	for _, groupInfo := range groupInfoPtrs {
		if groupInfo != nil && groupInfo.Enabled == "Y" {
			groupInfos = append(groupInfos, *groupInfo)
		}
	}
	var actionInfos []model.ActionInfo
	for _, actionInfo := range actionInfoPtrs {
		if actionInfo != nil {
			actionInfos = append(actionInfos, *actionInfo)
		}
	}
	return initialize.NewAutomateCache().SetCacheBySceneAutomationId(scene_automation_id, groupInfos, actionInfos)
}

func (s *SceneAutomation) DeleteSceneAutomation(scene_automation_id string) error {
	return dal.DeleteSceneAutomation(scene_automation_id, nil)
}

func (s *SceneAutomation) GetSceneAutomation(scene_automation_id string) (interface{}, error) {
	sceneAutomation, err := dal.GetSceneAutomation(scene_automation_id, nil)
	if err != nil {
		return nil, err
	}

	deviceTriggerCondition, err := dal.GetDeviceTriggerCondition(scene_automation_id)
	if err != nil {
		return nil, err
	}

	oneTimeTask, err := dal.GetOneTimeTask(scene_automation_id)
	if err != nil {
		return nil, err
	}

	periodicTask, err := dal.GetPeriodicTask(scene_automation_id)
	if err != nil {
		return nil, err
	}

	actionInfo, err := dal.GetActionInfo(scene_automation_id)
	if err != nil {
		return nil, err
	}

	res := make(map[string]interface{})
	res["id"] = sceneAutomation.ID
	res["name"] = sceneAutomation.Name
	res["description"] = sceneAutomation.Description
	res["enabled"] = sceneAutomation.Enabled
	res["tenant_id"] = sceneAutomation.TenantID
	res["creator"] = sceneAutomation.Creator
	res["updator"] = sceneAutomation.Updator

	triggerConditionGroups := make([][]map[string]interface{}, 0)

	if len(periodicTask) > 0 {
		tmp := make([][]map[string]interface{}, 0)
		for _, v := range periodicTask {
			mapList := make([]map[string]interface{}, 0)
			periodicTaskMap := make(map[string]interface{})
			periodicTaskMap["task_type"] = v.TaskType
			periodicTaskMap["expiration_time"] = v.ExpirationTime
			periodicTaskMap["params"] = v.Param
			periodicTaskMap["trigger_conditions_type"] = "21"
			mapList = append(mapList, periodicTaskMap)
			tmp = append(tmp, mapList)
		}
		triggerConditionGroups = append(triggerConditionGroups, tmp...)
	}

	if len(oneTimeTask) > 0 {
		tmp := make([][]map[string]interface{}, 0)
		for _, v := range oneTimeTask {
			mapList := make([]map[string]interface{}, 0)
			oneTimeTaskMap := make(map[string]interface{})
			oneTimeTaskMap["execution_time"] = v.ExecutionTime
			oneTimeTaskMap["expiration_time"] = v.ExpirationTime
			oneTimeTaskMap["trigger_conditions_type"] = "20"
			mapList = append(mapList, oneTimeTaskMap)
			tmp = append(tmp, mapList)
		}
		triggerConditionGroups = append(triggerConditionGroups, tmp...)
	}

	if len(deviceTriggerCondition) > 0 {
		tmp := make([][]map[string]interface{}, 0)

		// 以 group_id 分组
		rebuild := make(map[string][]*model.DeviceTriggerCondition)
		for _, v := range deviceTriggerCondition {
			rebuild[v.GroupID] = append(rebuild[v.GroupID], v)
		}

		for _, v := range rebuild {
			mapList := make([]map[string]interface{}, 0)
			for _, v2 := range v {
				deviceTriggerConditionMap := make(map[string]interface{})
				deviceTriggerConditionMap["id"] = v2.ID
				deviceTriggerConditionMap["group_id"] = v2.GroupID
				deviceTriggerConditionMap["trigger_conditions_type"] = v2.TriggerConditionType
				deviceTriggerConditionMap["trigger_source"] = v2.TriggerSource
				deviceTriggerConditionMap["trigger_param_type"] = v2.TriggerParamType
				deviceTriggerConditionMap["trigger_param"] = v2.TriggerParam
				deviceTriggerConditionMap["trigger_operator"] = v2.TriggerOperator
				deviceTriggerConditionMap["trigger_value"] = v2.TriggerValue
				mapList = append(mapList, deviceTriggerConditionMap)
			}
			tmp = append(tmp, mapList)
		}
		triggerConditionGroups = append(triggerConditionGroups, tmp...)

	}

	res["trigger_condition_groups"] = triggerConditionGroups

	if len(actionInfo) > 0 {
		actionInfoMap := make([]map[string]interface{}, 0)
		for _, v := range actionInfo {
			tmp := make(map[string]interface{})
			tmp["action_type"] = v.ActionType
			tmp["action_target"] = v.ActionTarget
			tmp["action_param_type"] = v.ActionParamType
			tmp["action_param"] = v.ActionParam
			tmp["action_value"] = v.ActionValue
			actionInfoMap = append(actionInfoMap, tmp)
		}
		res["actions"] = actionInfoMap
	}

	return res, err
}

func (s *SceneAutomation) SwitchSceneAutomation(scene_automation_id, target string) error {

	// 开启事物
	tx, err := dal.StartTransaction()
	if err != nil {
		return err
	}

	if target == "" {
		data, err := dal.GetSceneAutomation(scene_automation_id, tx)
		if err != nil {
			dal.Rollback(tx)
			return err
		}
		if data.Enabled == "Y" {
			target = "N"
		} else {
			target = "Y"
		}
	}

	err = dal.SwitchSceneAutomation(scene_automation_id, target, tx)
	if err != nil {
		dal.Rollback(tx)
		return err
	}

	err = dal.SwitchDeviceTriggerCondition(scene_automation_id, target, tx)
	if err != nil {
		dal.Rollback(tx)
		return err
	}

	err = dal.SwitchOneTimeTask(scene_automation_id, target, tx)
	if err != nil {
		dal.Rollback(tx)
		return err
	}

	err = dal.SwitchPeriodicTask(scene_automation_id, target, tx)
	if err != nil {
		dal.Rollback(tx)
		return err
	}

	dal.Commit(tx)
	//场景联动关闭 启动自动化缓存设置
	go func() {
		//场景联动开启
		if target == "Y" {
			err = s.AutomateCacheSet(scene_automation_id)
			if err != nil {
				logrus.Error("编辑场景联动保存自动换缓存信息失败，err: ", err)
			}
		}
		//场景联动关闭
		if target == "N" {
			err := initialize.NewAutomateCache().DeleteCacheBySceneAutomationId(scene_automation_id)
			if err != nil {
				logrus.Error("编辑删除自动化缓存失败: ", err)
			}
		}
	}()
	return nil
}

func (s *SceneAutomation) GetSceneAutomationByPageReq(req *model.GetSceneAutomationByPageReq, u *utils.UserClaims) (interface{}, error) {
	total, sceneInfo, err := dal.GetSceneAutomationByPage(req, u.TenantID)
	if err != nil {
		return nil, err
	}
	sceneListMap := make(map[string]interface{})
	sceneListMap["total"] = total
	sceneListMap["list"] = sceneInfo
	return sceneListMap, nil
}

func (s *SceneAutomation) GetSceneAutomationWithAlarmByPageReq(req *model.GetSceneAutomationsWithAlarmByPageReq, u *utils.UserClaims) (interface{}, error) {
	total, sceneInfo, err := dal.GetSceneAutomationWithAlarmByPageReq(req, u.TenantID)
	if err != nil {
		return nil, err
	}
	sceneListMap := make(map[string]interface{})
	sceneListMap["total"] = total
	sceneListMap["list"] = sceneInfo
	return sceneListMap, nil
}

func (s *SceneAutomation) UpdateSceneAutomation(req *model.UpdateSceneAutomationReq, u *utils.UserClaims) (string, error) {

	var scene_automation_id string

	// 开启事物
	tx, err := dal.StartTransaction()
	if err != nil {
		return scene_automation_id, err
	}

	scene_automation_id = req.ID
	t := utils.GetUTCTime()
	// 更新 scene_automations
	var sceneAutomation = model.SceneAutomation{}
	sceneAutomation.ID = scene_automation_id
	sceneAutomation.Name = req.Name
	sceneAutomation.Description = &req.Description
	sceneAutomation.Enabled = req.Enabled
	sceneAutomation.TenantID = u.TenantID
	sceneAutomation.Updator = u.ID
	sceneAutomation.UpdatedAt = &t
	sceneAutomation.Remark = &req.Remark

	err = dal.SaveSceneAutomation(&sceneAutomation, tx)
	if err != nil {
		return "", err
	}

	// 删除
	err = dal.DeleteDeviceTriggerCondition(scene_automation_id, tx)
	if err != nil {
		dal.Rollback(tx)
		return "", err
	}

	// 删除
	err = dal.DeleteOneTimeTask(scene_automation_id, tx)
	if err != nil {
		dal.Rollback(tx)
		return "", err
	}

	// 删除
	err = dal.DeletePeriodicTask(scene_automation_id, tx)
	if err != nil {
		dal.Rollback(tx)
		return "", err
	}

	// 删除
	err = dal.DeleteActionInfo(scene_automation_id, tx)
	if err != nil {
		dal.Rollback(tx)
		return "", err
	}

	for _, v := range req.TriggerConditionGroups {
		groupId := uuid.New()
		for _, v2 := range v {

			switch v2.TriggerConditionsType {
			case "10", "11", "22":
				// 写入 device_trigger_condition
				var dtc = model.DeviceTriggerCondition{}
				dtc.ID = uuid.New()
				dtc.SceneAutomationID = scene_automation_id
				dtc.Enabled = req.Enabled
				dtc.GroupID = groupId
				dtc.TriggerConditionType = v2.TriggerConditionsType
				dtc.TriggerSource = v2.TriggerSource
				dtc.TriggerParamType = v2.TriggerParamType
				dtc.TriggerParam = v2.TriggerParam
				dtc.TriggerOperator = v2.TriggerOperator
				if v2.TriggerValue != nil {
					dtc.TriggerValue = *v2.TriggerValue
				}
				dtc.Enabled = req.Enabled
				dtc.TenantID = u.TenantID
				// 创建设备触发条件
				logrus.Info("创建设备触发条件信息")
				err = dal.CreateDeviceTriggerCondition(dtc, tx)
				if err != nil {
					dal.Rollback(tx)
					return "", err
				}

			case "20":
				// 写入 one_time_tasks
				var ott = model.OneTimeTask{}
				ott.ID = uuid.New()
				ott.SceneAutomationID = scene_automation_id
				if v2.ExecutionTime != nil {
					ott.ExecutionTime = *v2.ExecutionTime
				}
				// 将字符串时间转换为时间戳
				// if v2.ExecutionTime != nil {
				// 	orgTime := *v2.ExecutionTime
				// 	intOrgTime := orgTime.Unix()
				// 	ott.ExpirationTime = intOrgTime
				// }

				if v2.ExpirationTime != nil {
					ott.ExpirationTime = int64(*v2.ExpirationTime)
				}
				ott.ExecutingState = "NEX"
				ott.Enabled = req.Enabled
				// 创建一次性任务
				logrus.Info("创建一次性任务信息")
				err = dal.CreateOneTimeTask(ott, tx)
				if err != nil {
					dal.Rollback(tx)
					return "", err
				}
			case "21":
				// 写入periodic_tasks
				var pt = model.PeriodicTask{}
				pt.ID = uuid.New()
				pt.SceneAutomationID = scene_automation_id
				if v2.TaskType != nil {
					pt.TaskType = *v2.TaskType
				}
				if v2.Params != nil {
					pt.Param = *v2.Params
				}
				if v2.ExpirationTime != nil {
					pt.ExpirationTime = int64(*v2.ExpirationTime)
				}
				// TODO 计算执行时间和过期时间
				// pt.ExecutionTime = *v2.ExecutionTime
				// pt.ExpirationTime = *v2.ExpirationTime
				pt.Enabled = "Y"
				// 创建周期性任务
				logrus.Info("创建周期性任务信息")
				err = dal.CreatePeriodicTask(pt, tx)
				if err != nil {
					dal.Rollback(tx)
					return "", err
				}
			default:
				dal.Rollback(tx)
				return "", fmt.Errorf("not support")
			}

		}
	}

	for _, v := range req.Actions {
		// 写入 action_info
		var actionInfo = model.ActionInfo{}
		actionInfo.ID = uuid.New()
		actionInfo.SceneAutomationID = scene_automation_id
		actionInfo.ActionTarget = &v.ActionTarget
		actionInfo.ActionType = v.ActionType
		actionInfo.ActionParamType = &v.ActionParamType
		actionInfo.ActionParam = &v.ActionParam
		actionInfo.ActionValue = &v.ActionValue
		err = dal.CreateActionInfo(actionInfo, tx)
		if err != nil {
			dal.Rollback(tx)
			return "", err
		}

	}

	dal.Commit(tx)

	//保存自动化缓存信息(先删除缓存)
	go func() {
		err := initialize.NewAutomateCache().DeleteCacheBySceneAutomationId(scene_automation_id)
		if err != nil {
			logrus.Error("编辑删除自动化缓存失败: ", err)
		}
		if req.Enabled == "Y" {
			err = s.AutomateCacheSet(scene_automation_id)
			if err != nil {
				logrus.Error("编辑场景联动保存自动换缓存信息失败，err: ", err)
			}
		}
	}()
	return scene_automation_id, nil
}
