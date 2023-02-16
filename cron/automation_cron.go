package cron

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cast"

	tp_cron "ThingsPanel-Go/initialize/cron"
)

//var C *cron.Cron

func init() {
	onceCron()
	automationCron()

}

func automationCron() {
	C := tp_cron.C
	//C = cron.New()
	var automationConditions []models.TpAutomationCondition
	result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("condition_type = '2' and time_condition_type in ('2','3')").Find(&automationConditions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		logs.Error("定时任务初始化失败！")
	}
	for _, automationCondition := range automationConditions {
		if automationCondition.TimeConditionType == "2" {
			//重复
			var cronString string
			if automationCondition.V1 == "0" {
				//几分钟
				number := cast.ToInt(automationCondition.V3)
				if number > 0 {
					cronString = "0/" + automationCondition.V3 + " * * * *"
				} else {
					logs.Error("cron按分钟不能为空或0")
					continue
				}
			} else if automationCondition.V1 == "1" {
				// 每小时的几分
				number := cast.ToInt(automationCondition.V3)
				cronString = cast.ToString(number) + " 0/1 * * * *"
			} else if automationCondition.V1 == "2" {
				// 每天的几点几分
				timeList := strings.Split(automationCondition.V3, ":")
				cronString = timeList[1] + " " + timeList[0] + " ? * * *"
			} else if automationCondition.V1 == "3" {
				// 星期几的几点几分
				timeList := strings.Split(automationCondition.V3, ":")
				cronString = timeList[2] + " " + timeList[1] + " ? " + timeList[0] + " * *"
			} else if automationCondition.V1 == "4" {
				// 每月的哪一天的几点几分
				timeList := strings.Split(automationCondition.V3, ":")
				cronString = timeList[2] + " " + timeList[1] + " " + timeList[0] + " * ? *"
			} else if automationCondition.V1 == "5" {
				cronString = automationCondition.V1
			}
			cronId, _ := C.AddFunc(cronString, func() { executeAutomationAction(automationCondition.AutomationId) })
			result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("id = ?", automationCondition.AutomationId).Update("V2", cast.ToString(cronId))
			if result.Error != nil {
				C.Remove(cronId)
				logs.Error(result.Error.Error())
			}
		}
	}
	id1, _ := C.AddFunc("0/2 * * * * *", func() { logs.Info("I am 1") })
	C.Start()
	C.AddFunc("0/4 * * * * *", func() { logs.Info("I am 2") })
	C.Remove(id1)
}
func executeAutomationAction(automationId string) {
	var conditionsService services.ConditionsService
	message, err := conditionsService.ExecuteAutomationAction(automationId)
	if err != nil {
		//执行失败，记录日志
		logs.Error(err.Error())
	} else {
		//执行成功，记录日志
		logs.Info(message)
	}
}

func onceCron() {
	//c = cron.New(cron.WithSeconds())
	crontab := cron.New()
	spec := "*/60 * * * * ?"
	task := func() {
		logs.Info("检查单次定时任务开始")
		format := "2006/01/02 15:04:05"
		now, _ := time.Parse(format, time.Now().Format(format))
		var automationConditions []models.TpAutomationCondition
		result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("condition_type = '2' and time_condition_type = '1' and v1 != '' and v1 < ?", now).Find(&automationConditions)
		if result.Error != nil {
			logs.Error(result.Error.Error())
		}
		for _, automationCondition := range automationConditions {
			var conditionsService services.ConditionsService
			message, err := conditionsService.ExecuteAutomationAction(automationCondition.AutomationId)
			if err != nil {
				//执行失败，记录日志
				logs.Error(err.Error())
			} else {
				//执行成功，记录日志
				logs.Info(message)
			}
			//删除条件
			result := psql.Mydb.Delete(&models.TpAutomationCondition{}, automationCondition.Id)
			if result.Error != nil {
				logs.Error(result.Error.Error())
			}
		}
		logs.Info("检查单次定时任务结束")
	}
	crontab.AddFunc(spec, task)
	crontab.Start()
	logs.Info("定时调度启动完成")
}
