package gateway

import (
	"ThingsPanel-Go/gateway/bl"
	"ThingsPanel-Go/gateway/tp_mqtt"

	beego "github.com/beego/beego/v2/server/web"
)

func init() {
	isOpen, _ := beego.AppConfig.String("beilai")
	if isOpen == "true" {
		tp_mqtt.InitTpClient()
		bl.InitBl110Client()
	}

}
