package extensions

import (
	"ThingsPanel-Go/services"
)

const (
	WIDGET_TYPE_PANEL  = "panel"
	FIELD_TYPE_CHART   = 1 //chart
	FIELD_TYPE_SWITCH  = 2 //switch
	FIELD_TYPE_SCROLL  = 3 //scroll
	FIELD_TYPE_STATUS  = 4 //control status
	FIELD_TYPE_LIQUID  = 5 //Liquid level status
	FIELD_TYPE_ADDRESS = 6 //ADDRESS
	FIELD_TYPE_VISUAL  = 7 //VISUAL
)

type Base struct {
}

func (b *Base) Main(device_ids []string, startTs int64, endTs int64) []interface{} {
	var TSKVService services.TSKVService
	t := TSKVService.GetTelemetry(device_ids, startTs, endTs)
	return t
}
