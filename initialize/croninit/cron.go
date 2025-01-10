package croninit

import (
	"project/internal/service"

	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
)

var (
	c = cron.New()
)

// 定义任务初始化
func CronInit() {
	//单次定义成任务 - 每5秒执行一次
	c.AddFunc("*/5 * * * * *", func() {
		logrus.Debug("自动化单次任务开始：")
		service.GroupApp.OnceTaskExecute()
	})

	//重复定义成任务 - 每5秒执行一次
	c.AddFunc("*/5 * * * * *", func() {
		logrus.Debug("自动化重复时间任务开始：")
		service.GroupApp.PeriodicTaskExecute()
	})

	// 每天凌晨2点执行数据清理
	c.AddFunc("0 2 * * *", func() {
		logrus.Debug("系统数据清理任务开始：")
		service.GroupApp.CleanSystemDataByCron()
	})

	c.AddFunc("0 1 * * * *", func() {
		service.GroupApp.RunScript()
	})

	c.Start()
}
