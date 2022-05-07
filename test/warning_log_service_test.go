package services

import (
	"ThingsPanel-Go/services"
	"reflect"
	"testing"
)

var WarningLogService services.WarningLogService

func TestGetWarningLogByPaging1(t *testing.T) {
	warningLogs, count := WarningLogService.GetWarningLogByPaging("", "", "", 1,
		10, "2022/04/06 14:48:58", "2022/05/06 14:48:58")
	value := reflect.ValueOf(count)
	if value.Int() <= 0 {
		t.Error("fail")
	} else {
		t.Log(count)
		t.Log(warningLogs)
	}

}
