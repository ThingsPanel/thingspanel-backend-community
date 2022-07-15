package gateway

import (
	"ThingsPanel-Go/gateway/bl"
	"ThingsPanel-Go/gateway/tp_mqtt"
)

func init() {
	tp_mqtt.InitTpClient()
	bl.InitBl110Client()
}
