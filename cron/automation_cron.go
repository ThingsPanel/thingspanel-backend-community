package cron

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cast"
)

//var C *cron.Cron

func Init() {
	log.Println("定时任务初始化...")
	// 一次性的定时任务，间隔1分钟扫一次
	onceCron()
	// 重复的定时任务
	go TaskCron()
	//
	otaCron()
	// 定时删除resources表中的数据
	deleteResourcesCron()
	// 数据清理
	TpDataCleanup()
	log.Println("定时任务初始化完成")
}

// 每小时执行一次删除resources表中的数据
func deleteResourcesCron() {
	crontab := cron.New()
	spec := "0 0 * * *"
	task := func() {
		logs.Info("删除resources表中的数据开始")
		var resourcesService services.ResourcesService
		isSuccess := resourcesService.Delete()
		if isSuccess {
			logs.Info("删除resources表中的数据成功")
		} else {
			logs.Error("删除resources表中的数据失败")
		}
	}
	crontab.AddFunc(spec, task)
	crontab.Start()
}

func TpDataCleanup() {
	crontab := cron.New()
	spec := "0 2 * * *"
	// spec := "*/1 * * * *"
	task := func() {
		logs.Info("执行数据清理")
		var s services.TpDataCleanupService
		err := s.ExecuteTpDataCleanup()
		if err != nil {
			logs.Error("数据清理失败")
		} else {
			logs.Info("数据清理成功")
		}
	}
	crontab.AddFunc(spec, task)
	crontab.Start()
}

//初始化定时任务，已弃用
// func AutomationCron() {
// 	C := tp_cron.C
// 	//C = cron.New()
// 	var automationConditions []models.TpAutomationCondition
// 	result := psql.Mydb.Table("tp_automation").
// 		Select("tp_automation_condition.*").
// 		Joins("left join tp_automation_condition on tp_automation.id = tp_automation_condition.automation_id").
// 		Where("tp_automation.enabled = '1' and tp_automation_condition.condition_type = '2' and tp_automation_condition.time_condition_type = '2'").
// 		Order("tp_automation.priority asc").
// 		Find(&automationConditions)
// 	if result.Error != nil {
// 		logs.Error(result.Error.Error())
// 		logs.Error("定时任务初始化失败！")
// 	}
// 	for _, automationCondition := range automationConditions {
// 		services.AutomationCron(automationCondition)
// 	}
// 	C.Start()
// }

func onceCron() {
	//c = cron.New(cron.WithSeconds())
	crontab := cron.New()
	spec := "0/1 * * * *" //每分钟一次
	task := func() {
		fmt.Println("检查单次定时任务开始")
		// 获取锁
		lockKey := "onceCronLock"
		lockValue := "1"
		lockDuration := 300 * time.Second
		// 尝试获取锁
		ok, err := redis.SetNX(lockKey, lockValue, lockDuration)
		if err != nil {
			logs.Error(err.Error())
			return
		}
		if ok {
			format := "2006/01/02 15:04:05"
			now, _ := time.Parse(format, time.Now().Format(format))
			var automationConditions []models.TpAutomationCondition
			result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("condition_type = '2' and time_condition_type = '1' and v1 != '' and v1 < ?", now).Find(&automationConditions)
			if result.Error != nil {
				logs.Error(result.Error.Error())
				return
			}
			for _, automationCondition := range automationConditions {
				// 触发，记录日志
				var logMessage string = "触发" + automationCondition.V1 + "的定时任务;"
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
					var automationLogMap = make(map[string]interface{})
					automationLogMap["Id"] = automationLog.Id
					var conditionsService services.ConditionsService
					msg, err := conditionsService.ExecuteAutomationAction(automationCondition.AutomationId, automationLog.Id, "", "")
					if err != nil {
						//执行失败，记录日志
						logs.Error(err.Error())
						automationLogMap["ProcessDescription"] = logMessage + err.Error()
					} else {
						//执行成功，记录日志
						logs.Info(logMessage)
						automationLogMap["ProcessDescription"] = logMessage + msg
						automationLogMap["ProcessResult"] = "1"
					}
					logs.Warn(automationLogMap)
					err = sutomationLogService.UpdateTpAutomationLog(automationLogMap)
					if err != nil {
						logs.Error(err.Error())
					}
				}
				//删除条件
				var automationConditionService services.TpAutomationConditionService
				err = automationConditionService.DeleteById(automationCondition.Id)
				if err != nil {
					logs.Error(err)
				}
			}
			// 释放锁
			redis.DelNX(lockKey)
			fmt.Println("检查单次定时任务结束")
		} else {
			// 未获取到锁，直接返回
			logs.Info("未获取到onceCronLock锁，直接返回！")

		}
	}
	crontab.AddFunc(spec, task)
	crontab.Start()
}

// 一秒扫一次action表，判断是否有需要执行的定时任务
func TaskCron() {
	// 循环
	for {
		// 休眠1秒
		time.Sleep(5 * time.Second)
		// 获取锁
		lockKey := "taskCronLock"
		lockValue := "1"
		lockDuration := 60 * time.Second
		// 尝试获取锁
		ok, err := redis.SetNX(lockKey, lockValue, lockDuration)
		if err != nil {
			logs.Error(err.Error())
			continue
		}
		if ok {
			// 获取到锁，执行任务
			ExecuteTask(lockKey)
		} else {
			// 未获取到锁，直接返回
			logs.Info("未获取到taskCronLock锁")
		}
	}
}

// 执行任务
func ExecuteTask(lockKey string) {
	format := "2006/01/02 15:04:05"
	now, _ := time.Parse(format, time.Now().Format(format))
	var automationConditions []models.TpAutomationCondition
	// 获取condition_type是重复、已启动、v5超过当前时间的定时任务
	result := psql.Mydb.Model(&models.TpAutomationCondition{}).Joins("right join tp_automation on tp_automation.id = tp_automation_condition.automation_id").Where("tp_automation.enabled = '1' and tp_automation_condition.condition_type = '2' and tp_automation_condition.time_condition_type = '2' and tp_automation_condition.v5 < ?", now).Limit(100).Find(&automationConditions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return
	}
	// 更新v5到下次执行时间
	for _, automationCondition := range automationConditions {
		// 计算下次执行时间
		// 计算下次执行时间
		nextTime, err := utils.GetNextTime(automationCondition.V1, automationCondition.V2, automationCondition.V3, automationCondition.V4)
		if err != nil {
			//继续下一个
			logs.Error(err.Error())
			continue
		}
		automationCondition.V5 = nextTime
		// 更新下次执行时间
		result = psql.Mydb.Model(&models.TpAutomationCondition{}).Where("id = ?", automationCondition.Id).Update("v5", nextTime)
		if result.Error != nil {
			logs.Error(result.Error.Error())
			continue
		}
	}
	// 释放锁
	redis.DelNX(lockKey)
	for _, automationCondition := range automationConditions {
		var logMessage string
		switch automationCondition.V1 {
		case "0":
			//几分钟
			number := cast.ToInt(automationCondition.V3)
			if number > 0 {
				logMessage += "触发" + automationCondition.V3 + "分钟执行一次的任务；"
			} else {
				logs.Error("cron按分钟不能为空或0")
				continue
			}
		case "1":
			// 每小时的几分
			logMessage += "触发每小时的" + automationCondition.V3 + "执行一次的任务；"
		case "2":
			// 每天的几点几分
			logMessage += "触发每天的" + automationCondition.V3 + "执行一次的任务；"
		case "3":
			logMessage += "触发每周" + automationCondition.V3 + "的" + automationCondition.V4 + "执行一次的任务；"
		case "4":
			// 每月的哪一天的几点几分
			timeList := strings.Split(automationCondition.V3, ":")
			logMessage += "触发每月" + timeList[0] + "日的" + timeList[1] + ":" + timeList[2] + "执行一次的任务；"
		case "5":
			logMessage += "自定义cron(" + automationCondition.V3 + ")到时；"
		default:
			logs.Error("cron类型错误")
			continue
		}
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
			automationLogMap["Id"] = automationLog.Id
			var conditionsService services.ConditionsService
			msg, err := conditionsService.ExecuteAutomationAction(automationCondition.AutomationId, automationLog.Id, "", "")
			if err != nil {
				//执行失败，记录日志
				logs.Error(err.Error())
				automationLogMap["ProcessDescription"] = logMessage + "|" + err.Error()
			} else {
				//执行成功，记录日志
				logs.Info(logMessage)
				automationLogMap["ProcessDescription"] = logMessage + "|" + msg
				automationLogMap["ProcessResult"] = "1"
			}
			err = sutomationLogService.UpdateTpAutomationLog(automationLogMap)
			if err != nil {
				logs.Error(err.Error())
			}
		}
	}
}
