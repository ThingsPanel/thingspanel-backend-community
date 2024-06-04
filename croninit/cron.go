package croninit

import (
	"project/service"

	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
)

var (
	c = cron.New()
)

// 定义任务初始化
func CronInit() {

	//单次定义成任务
	c.AddFunc("* * * * * *", func() {
		logrus.Debug("单次任务开始：")
		service.GroupApp.OnceTaskExecute()
	})

	//重复定义成任务
	c.AddFunc("* * * * * *", func() {
		logrus.Debug("重复定义成任务开始：")
		service.GroupApp.PeriodicTaskExecute()
	})

	// 每天执行数据清理
	c.AddFunc("0 2 * * *", func() {
		logrus.Debug("系统数据清理任务开始：")
		service.GroupApp.CleanSystemDataByCron()
	})

	c.Start()
}
