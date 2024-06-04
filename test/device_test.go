package test

import (
	"project/dal"
	"testing"

	"github.com/sirupsen/logrus"
)

func init() {
	// initialize.ViperInit("../configs/conf.yml")
	// initialize.LogInIt()
	// db := initialize.PgInit()
	// //initialize.RedisInit()
	// query.SetDefault(db)
}
func TestGetDeviceTemplateIdByDeviceId(t *testing.T) {

	tempId, _ := dal.GetDeviceTemplateIdByDeviceId("6c21e49c-d0dc-7315-bd5c-5702ad789936")

	logrus.Debug("结果：", tempId)

	fc := dal.GetIdentifierNameAttribute()
	name := fc(tempId, "wendu")

	logrus.Debug("name:", name)

}
