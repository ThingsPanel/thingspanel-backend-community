package main

import (
	"log"

	_ "ThingsPanel-Go/initialize/log"
	_ "ThingsPanel-Go/initialize/psql"

	beego "github.com/beego/beego/v2/server/web"

	_ "ThingsPanel-Go/modules/dataService"

	_ "ThingsPanel-Go/initialize/cache"

	_ "ThingsPanel-Go/initialize/cron"
	_ "ThingsPanel-Go/initialize/send_message"
	_ "ThingsPanel-Go/initialize/session"
	_ "ThingsPanel-Go/initialize/validate"
	_ "ThingsPanel-Go/routers"

	services "ThingsPanel-Go/services"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"

	_ "ThingsPanel-Go/grpc/protocol_plugin/server"

	_ "ThingsPanel-Go/cron"

	tptodbClient "ThingsPanel-Go/grpc/tptodb_client"
)

var Ticker *time.Ticker

func init() {
	log.Println("系统初始化")
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
	beego.Run()
}
