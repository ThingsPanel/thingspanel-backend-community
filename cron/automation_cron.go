package cron

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"fmt"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cast"

	tp_cron "ThingsPanel-Go/initialize/cron"
)

//var C *cron.Cron

func init() {
	fmt.Println("定时任务初始化开始")
	onceCron()
	automationCron()
	fmt.Println("定时任务初始化完成")
}

func automationCron() {
	C := tp_cron.C
	//C = cron.New()
	var automationConditions []models.TpAutomationCondition
	result := psql.Mydb.Table("tp_automation").
		Select("tp_automation_condition.*").
		Joins("left join tp_automation_condition on tp_automation.id = tp_automation_condition.automation_id").
		Where("tp_automation.enabled = '1' and tp_automation_condition.condition_type = '2' and tp_automation_condition.time_condition_type = '2'").
		Order("tp_automation.priority asc").
		Find(&automationConditions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		logs.Error("定时任务初始化失败！")
	}
	for _, automationCondition := range automationConditions {
		var logMessage string
		var cronString string
		if automationCondition.V1 == "0" {
			//几分钟
			number := cast.ToInt(automationCondition.V3)
			if number > 0 {
				cronString = "0/" + automationCondition.V3 + " * * * *"
				logMessage += "触发" + automationCondition.V3 + "分钟执行一次的任务；"
			} else {
				logs.Error("cron按分钟不能为空或0")
				continue
			}
		} else if automationCondition.V1 == "1" {
			// 每小时的几分
			number := cast.ToInt(automationCondition.V3)
			cronString = cast.ToString(number) + " 0/1 * * * *"
			logMessage += "触发每小时的" + automationCondition.V3 + "执行一次的任务；"
		} else if automationCondition.V1 == "2" {
			// 每天的几点几分
			timeList := strings.Split(automationCondition.V3, ":")
			cronString = timeList[1] + " " + timeList[0] + " ? * * *"
			logMessage += "触发每天的" + automationCondition.V3 + "执行一次的任务；"
		} else if automationCondition.V1 == "3" {
			// 星期几的几点几分
			timeList := strings.Split(automationCondition.V3, ":")
			cronString = timeList[2] + " " + timeList[1] + " ? " + timeList[0] + " * *"
			logMessage += "触发每周的" + automationCondition.V3 + "执行一次的任务；"
		} else if automationCondition.V1 == "4" {
			// 每月的哪一天的几点几分
			timeList := strings.Split(automationCondition.V3, ":")
			cronString = timeList[2] + " " + timeList[1] + " " + timeList[0] + " * ? *"
			logMessage += "触发每月的" + automationCondition.V3 + "执行一次的任务；"
		} else if automationCondition.V1 == "5" {
			cronString = automationCondition.V1
		}
		execute := func() {
			// 触发，记录日志
			var automationLogMap = make(map[string]interface{})
			var sutomationLogService services.TpAutomationLogService
			var automationLog models.TpAutomationLog
			automationLog.AutomationId = automationCondition.AutomationId
			automationLog.ProcessDescription = logMessage
			automationLog.TriggerTime = time.Now().Format("2006/01/02 15:04:05")
			automationLog.ProcessResult = "2"
			automationLog, err := sutomationLogService.AddTpAutomationLog(automationLog)
			if err != nil {
				logs.Error(err.Error())
			} else {
				var conditionsService services.ConditionsService
				msg, err := conditionsService.ExecuteAutomationAction(automationCondition.AutomationId, automationLog.Id)
				if err != nil {
					//执行失败，记录日志
					logs.Error(err.Error())
					automationLogMap["process_description"] = logMessage + err.Error()
				} else {
					//执行成功，记录日志
					logs.Info(logMessage)
					automationLogMap["process_description"] = logMessage + msg
					automationLogMap["process_result"] = '1'
				}
				err = sutomationLogService.UpdateTpAutomationLog(automationLogMap)
				if err != nil {
					logs.Error(err.Error())
				}
			}
		}
		cronId, _ := C.AddFunc(cronString, execute)
		result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("id = ?", automationCondition.AutomationId).Update("V2", cast.ToString(cronId))
		if result.Error != nil {
			C.Remove(cronId)
			logs.Error(result.Error.Error())
		}
	}
	C.Start()
}

func onceCron() {
	//c = cron.New(cron.WithSeconds())
	crontab := cron.New()
	spec := "0/1 * * * *" //每分钟一次
	task := func() {
		fmt.Println("检查单次定时任务开始")
		format := "2006/01/02 15:04:05"
		now, _ := time.Parse(format, time.Now().Format(format))
		var automationConditions []models.TpAutomationCondition
		result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("condition_type = '2' and time_condition_type = '1' and v1 != '' and v1 < ?", now).Find(&automationConditions)
		if result.Error != nil {
			logs.Error(result.Error.Error())
		}
		for _, automationCondition := range automationConditions {
			// 触发，记录日志
			var logMessage string = "触发" + automationCondition.V1 + "的定时任务;"
			var automationLogMap = make(map[string]interface{})
			var sutomationLogService services.TpAutomationLogService
			var automationLog models.TpAutomationLog
			automationLog.AutomationId = automationCondition.AutomationId
			automationLog.ProcessDescription = logMessage
			automationLog.TriggerTime = time.Now().Format("2006/01/02 15:04:05")
			automationLog.ProcessResult = "2"
			automationLog, err := sutomationLogService.AddTpAutomationLog(automationLog)
			if err != nil {
				logs.Error(err.Error())
			} else {
				var conditionsService services.ConditionsService
				msg, err := conditionsService.ExecuteAutomationAction(automationCondition.AutomationId, automationLog.Id)
				if err != nil {
					//执行失败，记录日志
					logs.Error(err.Error())
					automationLogMap["process_description"] = logMessage + err.Error()
				} else {
					//执行成功，记录日志
					logs.Info(logMessage)
					automationLogMap["process_description"] = logMessage + msg
					automationLogMap["process_result"] = '1'
				}
				err = sutomationLogService.UpdateTpAutomationLog(automationLogMap)
				if err != nil {
					logs.Error(err.Error())
				}
			}
			//删除条件
			result := psql.Mydb.Delete(&models.TpAutomationCondition{}, automationCondition.Id)
			if result.Error != nil {
				logs.Error(result.Error.Error())
			}
		}
		fmt.Println("检查单次定时任务结束")
	}
	crontab.AddFunc(spec, task)
	crontab.Start()
}
