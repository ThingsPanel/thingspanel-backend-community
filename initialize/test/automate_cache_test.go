package test

import (
	"fmt"
	"project/initialize"
	"project/internal/model"
	"testing"
)

func init() {
	initialize.ViperInit("../../configs/conf-localdev.yml")
	initialize.RedisInit()
	cache = initialize.NewAutomateCache()
}

var (
	sceneAutomateId = "sceneAutomateId_test"
	cache           *initialize.AutomateCache
)

func StringPoints(s string) *string {
	return &s
}

func getConditions(sceneAutomateId string) []model.DeviceTriggerCondition {
	var conditions []model.DeviceTriggerCondition

	condition := model.DeviceTriggerCondition{
		SceneAutomationID:    sceneAutomateId,
		GroupID:              "groupId",
		TriggerConditionType: "10",
		TriggerValue:         "30",
		TriggerSource:        StringPoints("condition_deviceIds01"),
		TriggerParamType:     StringPoints("TEL"),
		TriggerParam:         StringPoints("temperature"),
		TriggerOperator:      StringPoints(">"),
	}

	conditions = append(conditions, condition)

	condition1 := model.DeviceTriggerCondition{
		SceneAutomationID:    sceneAutomateId,
		GroupID:              "groupId",
		TriggerConditionType: "10",
		TriggerValue:         "30",
		TriggerSource:        StringPoints("condition_deviceIds02"),
		TriggerParamType:     StringPoints("TEL"),
		TriggerParam:         StringPoints("temperature"),
		TriggerOperator:      StringPoints(">"),
	}
	conditions = append(conditions, condition1)
	return conditions
}

func getActions(sceneAutomateId string) []model.ActionInfo {
	var actions []model.ActionInfo
	action1 := model.ActionInfo{
		SceneAutomationID: sceneAutomateId,
		ActionType:        "10",
		ActionTarget:      StringPoints("action_deviceIds01"),
		ActionParamType:   StringPoints("CMD"),
		ActionParam:       StringPoints("test_cmd"),
		ActionValue:       StringPoints("test_val"),
	}
	actions = append(actions, action1)
	action2 := model.ActionInfo{
		SceneAutomationID: sceneAutomateId,
		ActionType:        "11",
		ActionTarget:      StringPoints("action_deviceIds02"),
		ActionParamType:   StringPoints("CMD"),
		ActionParam:       StringPoints("test_cmd"),
		ActionValue:       StringPoints("test_val"),
	}
	actions = append(actions, action2)
	return actions
}

// conditions []model.DeviceTriggerCondition, actions []model.ActionInfo
func TestSetCacheBySceneAutomationId(t *testing.T) {
	fmt.Println("测试创建缓存...")
	//cache := initialize.NewAutomateCache()
	conditions := getConditions(sceneAutomateId)
	err := cache.SetCacheBySceneAutomationId(sceneAutomateId, conditions, getActions(sceneAutomateId))
	if err != nil {
		t.Error("自动化缓存保存失败", err)
	}
}

func TestGetCacheByDeviceId(t *testing.T) {
	fmt.Println("测试查询缓存存在的情况...")
	//cache := initialize.NewAutomateCache()
	res, resultInt, err := cache.GetCacheByDeviceId("condition_deviceIds01", "")
	if err != nil {
		t.Error("根据设备获取自动化缓存失败", err)
	}
	if resultInt != initialize.AUTOMATE_CACHE_RESULT_OK {
		t.Errorf("查询异常, 查询状态:%d, 结果: %#v", resultInt, res)
	}
	fmt.Printf("结果:%#v", res)
}

func TestDeleteCacheBySceneAutomationId(t *testing.T) {
	fmt.Println("测试删除场景缓存...")
	//cache := initialize.NewAutomateCache()
	err := cache.DeleteCacheBySceneAutomationId(sceneAutomateId)
	if err != nil {
		t.Error("删除缓存失败", err)
	}
}

func TestGetCacheByDeviceIdNotExists(t *testing.T) {
	fmt.Println("测试设备缓存中无数据...")
	//cache := initialize.NewAutomateCache()
	res, resultInt, err := cache.GetCacheByDeviceId("condition_deviceIds0004", "")
	fmt.Printf("查询状态:%d", resultInt)
	if err != nil {
		t.Error("根据设备获取自动化缓存失败", err)
	}
	if resultInt != initialize.AUTOMATE_CACHE_RESULT_NOT_FOUND {
		t.Errorf("查询异常, 查询状态:%d, 结果: %#v", resultInt, res)
	}
	fmt.Printf("结果:%#v", res)
}

func TestSetCacheByDeviceIdWithNoTask(t *testing.T) {
	fmt.Println("测试缓存中保存无任务设备...")
	err := cache.SetCacheByDeviceIdWithNoTask("condition_deviceIds00005", "")
	if err != nil {
		t.Error("测试缓存中保存无任务设备失败", err)
	}
	_, resultInt, err := cache.GetCacheByDeviceId("condition_deviceIds00005", "")
	fmt.Printf("查询结果: resultInt:%d", resultInt)
	if err != nil {
		t.Error("根据设备获取自动化缓存失败", err)
	}
	if resultInt != initialize.AUTOMATE_CACHE_RESULT_NOT_TASK {
		t.Errorf("查询异常, 查询状态:%d", resultInt)
	}
}

func TestSetCacheByDeviceId(t *testing.T) {
	fmt.Println("测试根据设备id自动化缓存信息...")
	deviceId := "condition_deviceIds_with_device"
	conditions := []model.DeviceTriggerCondition{
		{
			SceneAutomationID:    "sceneAutomateId_with_device_01",
			GroupID:              "groupId",
			TriggerConditionType: "10",
			TriggerValue:         "20-20",
			TriggerSource:        StringPoints(deviceId),
			TriggerParamType:     StringPoints("TEL"),
			TriggerParam:         StringPoints("temperature"),
			TriggerOperator:      StringPoints(">"),
		},
		{
			SceneAutomationID:    "sceneAutomateId_with_device_02",
			GroupID:              "groupId",
			TriggerConditionType: "10",
			TriggerValue:         "20-20",
			TriggerSource:        StringPoints(deviceId),
			TriggerParamType:     StringPoints("TEL"),
			TriggerParam:         StringPoints("temperature"),
			TriggerOperator:      StringPoints(">"),
		},
	}

	actions := []model.ActionInfo{
		{
			SceneAutomationID: "sceneAutomateId_with_device_01",
			ActionType:        "10",
			ActionTarget:      StringPoints("action_deviceIds01"),
			ActionParamType:   StringPoints("CMD"),
			ActionParam:       StringPoints("test_cmd"),
			ActionValue:       StringPoints("test_val"),
		},
		{
			SceneAutomationID: "sceneAutomateId_with_device_02",
			ActionType:        "10",
			ActionTarget:      StringPoints("action_deviceIds01"),
			ActionParamType:   StringPoints("CMD"),
			ActionParam:       StringPoints("test_cmd"),
			ActionValue:       StringPoints("test_val"),
		},
	}

	err := cache.SetCacheByDeviceId(deviceId, "", conditions, actions)
	if err != nil {
		t.Error("测试缓存中保存无任务设备失败", err)
	}
	res, resultInt, err := cache.GetCacheByDeviceId(deviceId, "")
	fmt.Printf("查询结果: resultInt:%d; 缓存信息: %#v", resultInt, res)
	if err != nil {
		t.Error("根据设备获取自动化缓存失败", err)
	}
	if resultInt != initialize.AUTOMATE_CACHE_RESULT_OK {
		t.Errorf("查询异常, 查询状态:%d", resultInt)
	}
}

func TestSetCacheByDeviceConfidId(t *testing.T) {
	fmt.Println("测试根据设备id自动化缓存信息...")
	deviceId := "condition_deviceIds_with_device"
	deviceConfigId := "condition_deviceIds_with_device_config_id"
	conditions := []model.DeviceTriggerCondition{
		{
			SceneAutomationID:    "sceneAutomateId_with_device_01",
			GroupID:              "groupId",
			TriggerConditionType: "11",
			TriggerValue:         "21",
			TriggerSource:        StringPoints(deviceConfigId),
			TriggerParamType:     StringPoints("TEL"),
			TriggerParam:         StringPoints("temperature"),
			TriggerOperator:      StringPoints(">"),
		},
		{
			SceneAutomationID:    "sceneAutomateId_with_device_01",
			GroupID:              "groupId",
			TriggerConditionType: "22",
			TriggerValue:         "137|06:30:00+00:00|16:30:00+00:00",
		},
		{
			SceneAutomationID:    "sceneAutomateId_with_device_02",
			GroupID:              "groupId02",
			TriggerConditionType: "11",
			TriggerValue:         "21",
			TriggerSource:        StringPoints(deviceConfigId),
			TriggerParamType:     StringPoints("TEL"),
			TriggerParam:         StringPoints("temperature"),
			TriggerOperator:      StringPoints(">"),
		},
	}

	actions := []model.ActionInfo{
		{
			SceneAutomationID: "sceneAutomateId_with_device_01",
			ActionType:        "10",
			ActionTarget:      StringPoints("action_deviceIds01"),
			ActionParamType:   StringPoints("CMD"),
			ActionParam:       StringPoints("test_cmd"),
			ActionValue:       StringPoints("test_val"),
		},
		{
			SceneAutomationID: "sceneAutomateId_with_device_02",
			ActionType:        "10",
			ActionTarget:      StringPoints("action_deviceIds01"),
			ActionParamType:   StringPoints("CMD"),
			ActionParam:       StringPoints("test_cmd"),
			ActionValue:       StringPoints("test_val"),
		},
	}

	err := cache.SetCacheByDeviceId(deviceId, deviceConfigId, conditions, actions)
	if err != nil {
		t.Error("测试缓存中保存无任务设备失败", err)
	}
	res, resultInt, err := cache.GetCacheByDeviceId(deviceId, deviceConfigId)
	fmt.Printf("查询结果: resultInt:%d; 缓存信息: %#v", resultInt, res)
	if err != nil {
		t.Error("根据设备获取自动化缓存失败", err)
	}
	if resultInt != initialize.AUTOMATE_CACHE_RESULT_OK {
		t.Errorf("查询异常, 查询状态:%d", resultInt)
	}
}
