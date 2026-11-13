package test

import (
	"fmt"
	"project/initialize"
	"testing"

	"github.com/sirupsen/logrus"
)

func init() {
	initialize.ViperInit("../../configs/conf-localdev.yml")
	initialize.LogInIt()
	initialize.RedisInit()
	alarmCache = initialize.NewAlarmCache()

}

var (
	alarmCache          *initialize.AlarmCache
	group_id            = "group_id_1234"
	scene_automation_id = "scene_automation_id1234"
	device_ids          = []string{"device_id123", "device_id456"}
	contents            = []string{"温度大于30", "湿度大于27"}
)

func TestSetDevice(t *testing.T) {
	logrus.Debug("单元测试开始执行:")
	err := alarmCache.SetDevice(group_id, scene_automation_id, device_ids, contents)
	if err != nil {
		t.Error("设置告警缓存失败", err)
	}

	res1, err := alarmCache.GetByGroupId(group_id)
	if err != nil {
		t.Error("查询告警缓存失败1", err)
	}
	fmt.Printf("res:%#v", res1)
	res2, err := alarmCache.GetBySceneAutomationId(scene_automation_id)
	if err != nil {
		t.Error("查询告警缓存失败2", err)
	}
	fmt.Printf("res:%#v", res2)

}

func TestDeleteBygroupId(t *testing.T) {
	fmt.Println("测试删除缓存...")

	err := alarmCache.DeleteBygroupId(group_id)
	if err != nil {
		t.Error("设置告警缓存失败", err)
	}
}
