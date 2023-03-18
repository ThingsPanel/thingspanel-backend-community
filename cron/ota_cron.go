package cron

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"fmt"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/robfig/cron/v3"
)

func otaCron() {
	crontab := cron.New()
	spec := "0/1 * * * *" //每分钟一次
	task := func() {
		fmt.Println("检查待升级ota定时任务开始")
		format := "2006-01-02 15:04:05"
		now, _ := time.Parse(format, time.Now().Format(format))

		var tpOtaTasks []models.TpOtaTask
		result := psql.Mydb.Model(&models.TpOtaTask{}).Where("task_status = '0' and start_time <= ?", now).Find(&tpOtaTasks) //修改定时任务状态为升级中
		psql.Mydb.Model(&models.TpOtaTask{}).Where("task_status = '0' and start_time <= ?", now).Update("task_status", 1)
		if result.Error != nil {
			logs.Error(result.Error.Error())
			return
		}
		//修改定时任务状态为升级中
		psql.Mydb.Model(&models.TpOtaTask{}).Where("task_status = '0' and start_time <= ?", now).Update("task_status", 1)

		for _, tpOtaTask := range tpOtaTasks {
			//没有推送和升级中的设备，修改任务状态为2-已完成
			var count int64
			psql.Mydb.Model(&models.TpOtaDevice{}).Where("ota_task_id=? and upgrade_status not in ('1','2')", tpOtaTask.Id).Count(&count)
			if count == 0 {
				psql.Mydb.Model(&models.TpOtaTask{}).Where("id = ?", tpOtaTask.Id).Update("task_status", 2)
			}
			//过了升级时间还未推送的设备标记为升级失败4
			psql.Mydb.Model(&models.TpOtaDevice{}).Where("ota_task_id=? and upgrade_status=?", tpOtaTask.Id, "0").Updates(&models.TpOtaDevice{UpgradeStatus: "4", StatusUpdateTime: time.Now().Format("2006-01-02 15:04:05")})
		}
		fmt.Println("检查待升级ota定时任务结束")
	}
	crontab.AddFunc(spec, task)
	crontab.Start()
}
