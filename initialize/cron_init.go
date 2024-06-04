package initialize

import "github.com/robfig/cron"

//定义任务初始化
func CronInit() {
	c := cron.New()

	//单次定义成任务
	c.AddFunc("*/5 * * * * *", func() {

	})

	//重复定义成任务
	c.AddFunc("*/5 * * * * *", func() {

	})
}
