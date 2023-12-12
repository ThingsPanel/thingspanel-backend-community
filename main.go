package main

import (
	c "ThingsPanel-Go/cron"
	tptodbClient "ThingsPanel-Go/grpc/tptodb_client"
	"ThingsPanel-Go/hook"
	"ThingsPanel-Go/initialize/cache"
	"ThingsPanel-Go/initialize/casbin"
	"ThingsPanel-Go/initialize/conf"
	tp_cron "ThingsPanel-Go/initialize/cron"
	tp_log "ThingsPanel-Go/initialize/log"
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	"ThingsPanel-Go/initialize/session"
	"ThingsPanel-Go/modules/dataService"
	"ThingsPanel-Go/plugin"
	"ThingsPanel-Go/routers"
	services "ThingsPanel-Go/services"
	"fmt"
	"log"
	"time"

	beego "github.com/beego/beego/v2/server/web"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/spf13/viper"
)

var Ticker *time.Ticker

func init() {
	log.Println("系统初始化开始")
	conf.Init()
	redis.Init()
	tp_log.Init()
	psql.Init()
	casbin.Init()
	dataService.Init()
	cache.Init()
	plugin.Init()
	hook.Init()
	tp_cron.Init()
	session.Init()
	routers.Init()
	c.Init()
	log.Println("系统初始化完成")
}
func main() {
	// 初始化grpc
	go tptodbClient.GrpcTptodbInit()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	go func() {
		for t := range ticker.C {
			log.Println(t)
			var ResourcesService services.ResourcesService
			percent, err := cpu.Percent(time.Second, false)
			if err != nil {
				log.Println("Error getting CPU percent:", err)
				continue
			}
			cpu_str := fmt.Sprintf("%.2f", percent[0])
			memInfo, err := mem.VirtualMemory()
			if err != nil {
				log.Println("Error getting virtual memory:", err)
				continue
			}
			mem_str := fmt.Sprintf("%.2f", memInfo.UsedPercent)
			currentTime := fmt.Sprint(time.Now().Format("2006-01-02 15:04:05"))
			ResourcesService.Add(cpu_str, mem_str, currentTime)
		}
	}()

	// 系统初始化的images
	beego.SetStaticPath("/files/init-images", "others/init_images")
	// 静态文件路径
	beego.SetStaticPath("/files", "files")

	beego.BConfig.CopyRequestBody = true
	beego.BConfig.WebConfig.Session.SessionOn = true
	beego.BConfig.WebConfig.AutoRender = false
	beego.BConfig.AppName = "ThingsPanel-Go"

	beego.BConfig.RunMode = viper.GetString("app.runmode")
	httpport := viper.GetString("app.httpport")
	beego.Run(httpport)
}
