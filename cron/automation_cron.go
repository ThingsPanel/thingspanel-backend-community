package cron

import (
	"github.com/beego/beego/v2/core/logs"
	"github.com/robfig/cron/v3"
)

func init() {
	c := cron.New(cron.WithSeconds())
	id1, _ := c.AddFunc("0/2 * * * * *", func() { logs.Info("I am 1") })
	c.Start()
	c.AddFunc("0/4 * * * * *", func() { logs.Info("I am 2") })
	c.Remove(id1)
}
