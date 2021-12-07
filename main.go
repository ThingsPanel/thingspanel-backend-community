package main

import (
	_ "ThingsPanel-Go/initialize/cache"
	_ "ThingsPanel-Go/initialize/psql"
	_ "ThingsPanel-Go/initialize/session"
	_ "ThingsPanel-Go/initialize/validate"

	//_ "ThingsPanel-Go/modules/dataService"
	_ "ThingsPanel-Go/routers"

	beego "github.com/beego/beego/v2/server/web"
)

func main() {
	beego.Run()
}
