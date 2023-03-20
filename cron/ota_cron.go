package cron

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
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
		//查询待升级的任务
		result := psql.Mydb.Model(&models.TpOtaTask{}).Where("task_status = '0' and start_time <= ?", now).Find(&tpOtaTasks)
		if result.Error != nil {
			logs.Error(result.Error.Error())
			return
		}
		//修改定时任务状态为升级中
		psql.Mydb.Model(&models.TpOtaTask{}).Where("task_status = '0' and start_time <= ?", now).Update("task_status", "1")
		//推送固件版本至设备
		sql := `select d.id,d.token from tp_ota_device od left join tp_ota_task ot on od.ota_task_id =ot.id
		left join device d on od.device_id=d.id  where od.upgrade_status='0' and od.ota_task_id=?`
		for _, otatask := range tpOtaTasks {
			var ota models.TpOta
			result := psql.Mydb.Model(&models.TpOta{}).Where("id=?", otatask.OtaId).Find(&ota)
			if result.Error != nil {
				logs.Error(result.Error.Error())
				continue
			}
			var devices []models.Device
			if err := psql.Mydb.Raw(sql, otatask.Id).Scan(&devices); err != nil {
				logs.Error(err.Error.Error())
				continue
			}
			var tpOtaDeviceService services.TpOtaDeviceService
			if err := tpOtaDeviceService.OtaToUpgradeMsg(devices, ota.Id); err != nil {
				logs.Error(err)
				continue
			}
			//修改ota设备升级状态为1 已推送
			psql.Mydb.Model(&models.TpOtaDevice{}).Where("ota_task_id=? and upgrade_status=?", otatask.Id, "0").Updates(&models.TpOtaDevice{UpgradeStatus: "1", StatusUpdateTime: time.Now().Format("2006-01-02 15:04:05")})

		}

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
