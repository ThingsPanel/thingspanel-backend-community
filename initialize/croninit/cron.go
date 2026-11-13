// /initialize/croninit/cron.go
package croninit

import (
	"time"

	"project/internal/service"

	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
)

var c = cron.New()

// 定义任务初始化
func CronInit() {
	// 初始化设备统计定时任务
	InitDeviceStatsCron(c)

	// 单次定义成任务 - 每5秒执行一次
	c.AddFunc("*/5 * * * * *", func() {
		logrus.Debug("【定时任务】自动化单次任务开始：")
		service.GroupApp.OnceTaskExecute()
	})

	// 重复定义成任务 - 每5秒执行一次
	c.AddFunc("*/5 * * * * *", func() {
		logrus.Debug("【定时任务】自动化重复时间任务开始：")
		service.GroupApp.PeriodicTaskExecute()
	})

	// 每天凌晨2点执行数据清理
	c.AddFunc("0 2 * * *", func() {
		logrus.Debug("【定时任务】系统数据清理任务开始：")
		service.GroupApp.CleanSystemDataByCron()
	})

	// 每天凌晨1点执行脚本
	c.AddFunc("0 1 * * *", func() {
		logrus.Debug("【定时任务】每天凌晨1点执行脚本任务开始：")
		service.GroupApp.RunScript()
	})
	// 每天凌晨
	err := c.AddFunc("2 0 * * * *", func() {
		logrus.Debug("【定时任务】消息推送清理任务开始：", time.Now())
		service.GroupApp.MessagePush.MessagePushMangeClear()
	})
	if err != nil {
		logrus.Error("【定时任务】消息推送清理任务启动失败")
	}
	c.Start()
}
