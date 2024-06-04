package test

import (
	"fmt"
	"project/dal"
	"project/initialize"
	"project/query"
	"project/service"
	"testing"

	"github.com/sirupsen/logrus"
)

func init() {
	initialize.ViperInit("../configs/conf.yml")
	initialize.LogInIt()
	db := initialize.PgInit()
	initialize.RedisInit()
	query.SetDefault(db)
}
func TestExecute(T *testing.T) {

	// list, _ := query.DeviceTriggerCondition.Where(query.DeviceTriggerCondition.Enabled.Eq("Y"), query.DeviceTriggerCondition.TriggerConditionType.Eq("10")).Find()
	// for _, val := range list {
	// 	fmt.Println("设备id:", *val.TriggerSource)
	// }
	// fmt.Print(list)
	logrus.Debug("单元测试开始执行:")
	// deviceId := "condition_deviceIds_with_device"
	// deviceConfigId := "condition_deviceIds_with_device_config_id"
	// device := &model.Device{
	// 	ID:             deviceId,
	// 	DeviceConfigID: &deviceConfigId,
	// }
	//d81b4f86-b6dc-1493-c72e-38e3c4b932ff
	device, _ := dal.GetDeviceByID("41b44d60-305f-f559-1d8d-61c040b63b1e")
	//logrus.Debug(device)
	err := service.GroupApp.Execute(device)
	if err != nil {
		//logrus.Errorf("自动化执行失败, err: %w", err)
		fmt.Println("自动化执行失败，err:", err)
	}
}
