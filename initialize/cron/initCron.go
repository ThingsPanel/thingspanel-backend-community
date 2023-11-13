package tp_cron

import "github.com/robfig/cron/v3"

var C *cron.Cron

func Init() {
	C = cron.New()
}
