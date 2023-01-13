package main

import (
	_ "ThingsPanel-Go/initialize/log"

	_ "ThingsPanel-Go/modules/dataService"

	_ "ThingsPanel-Go/initialize/cache"
	_ "ThingsPanel-Go/initialize/psql"

	_ "ThingsPanel-Go/initialize/send_message"
	_ "ThingsPanel-Go/initialize/session"
	_ "ThingsPanel-Go/initialize/validate"
	_ "ThingsPanel-Go/routers"

	_ "ThingsPanel-Go/cron"
	services "ThingsPanel-Go/services"
	"fmt"
	"time"

	beego "github.com/beego/beego/v2/server/web"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

var Ticker *time.Ticker

func main() {
	// //beego日志模块配置
	// logs.Debug("系统日志初始化开始...")
	// dateStr := time.Now().Format("2006-01-02")
	// maxdays, _ := beego.AppConfig.String("maxdays")
	// level, _ := beego.AppConfig.String("level")
	// maxlines, _ := beego.AppConfig.String("maxlines")
	// dataSource := fmt.Sprintf(`{"filename":"files/logs/%s/log.log","level":%s,"maxlines":%s,"maxsize":0,"daily":true,"maxdays":%s,"color":true}`,
	// 	dateStr,
	// 	level,
	// 	maxlines,
	// 	maxdays,
	// )
	// //maxdays 文件最多保存多少天，默认保存 7 天
	// logs.SetLogger(logs.AdapterFile, dataSource)
	// // 输出log时能显示输出文件名和行号（非必须）
	// logs.EnableFuncCallDepth(true)
	// //异步输出
	// logs.Async()
	// logs.Debug("系统日志完成初始化")
	// // go基本log设置
	// log.SetFlags(log.Lshortfile | log.Ltime | log.Ldate)
	// 读取服务器信息
	Ticker = time.NewTicker(time.Millisecond * 5000)
	go func() {
		for t := range Ticker.C {
			fmt.Println(t)
			var ResourcesService services.ResourcesService
			percent, _ := cpu.Percent(time.Second, false)
			cpu_str := fmt.Sprintf("%.2f", percent[0])
			memInfo, _ := mem.VirtualMemory()
			mem_str := fmt.Sprintf("%.2f", memInfo.UsedPercent)
			currentTime := fmt.Sprint(time.Now().Format("2006-01-02 15:04:05"))
			ResourcesService.Add(cpu_str, mem_str, currentTime)
		}
	}()
	beego.SetStaticPath("/extensions", "extensions")
	beego.SetStaticPath("/files", "files")
	beego.Run()
}
