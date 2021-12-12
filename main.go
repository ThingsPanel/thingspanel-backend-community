package main

import (
	_ "ThingsPanel-Go/initialize/cache"
	_ "ThingsPanel-Go/initialize/psql"
	_ "ThingsPanel-Go/initialize/session"
	_ "ThingsPanel-Go/initialize/validate"

	_ "ThingsPanel-Go/modules/dataService"
	_ "ThingsPanel-Go/routers"

	beego "github.com/beego/beego/v2/server/web"
)

//var ticker *time.Ticker

func main() {
	// var TSKVService services.TSKVService
	// device_ids := []string{"5d9e4336-ef91-756d-c372-5db9e525e4b1"}
	// t := TSKVService.GetTelemetry(device_ids, 0, 2639244657616757)
	// fmt.Println(t)
	// 启动
	// ticker = time.NewTicker(time.Millisecond * 1000)
	// go func() {
	// 	for t := range ticker.C {
	// 		fmt.Println("Tick at", t)
	// 	}
	// }()
	// // 关闭
	// time.Sleep(time.Millisecond * 10000)
	// ticker.Stop()
	beego.Run()
}
