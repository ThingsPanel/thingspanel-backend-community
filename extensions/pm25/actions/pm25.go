package actions

import (
	"ThingsPanel-Go/extensions"
	"fmt"
)

type PM25 struct{}

func init() {
	fmt.Println("pm25")
}

func (p *PM25) Main(device_ids []string, startTs int64, endTs int64) []interface{} {
	var Base extensions.Base
	t := Base.Main(device_ids, startTs, endTs)
	return t
}
