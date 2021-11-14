package main

import (
	_ "github.com/ThingsPanel/ThingsPanel-Go/routers"

	beego "github.com/beego/beego/v2/server/web"
)

func main() {
	beego.Run()
}
