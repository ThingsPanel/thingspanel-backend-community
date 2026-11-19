package iotdbhistory

import (
	"encoding/json"
	"reflect"
	"testing"

	"project/internal/model"
)

func TestDecodeRowsMapsIoTDBTableResults(t *testing.T) {
	b := &Backend{}
	rows, err := b.decodeRows([][]interface{}{
		{json.Number("1720000000123"), json.Number("21.5"), nil, nil, "tenant-a", "device-a", "temperature"},
		{"2024-07-03T09:46:40.456Z", nil, true, nil, "tenant-a", "device-a", "online"},
		{json.Number("1720000000789"), nil, nil, "normal", "tenant-a", "device-a", "state"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []*model.TelemetryData{
		{T: 1720000000123, NumberV: floatPointer(21.5), TenantID: stringPointer("tenant-a"), DeviceID: "device-a", Key: "temperature"},
		{T: 1720000000456, BoolV: boolPointer(true), TenantID: stringPointer("tenant-a"), DeviceID: "device-a", Key: "online"},
		{T: 1720000000789, StringV: stringPointer("normal"), TenantID: stringPointer("tenant-a"), DeviceID: "device-a", Key: "state"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("decoded rows = %#v, want %#v", rows, want)
	}
}

func TestDecodeRowsRejectsMalformedResults(t *testing.T) {
	b := &Backend{}
	for name, input := range map[string][][]interface{}{
		"wrong width":   {{int64(1)}},
		"bad timestamp": {{"not-a-time", nil, nil, nil, nil, "d", "k"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := b.decodeRows(input); err == nil {
				t.Fatal("malformed IoTDB result must return an error")
			}
		})
	}
}

func TestHistoryPageValidationHappensBeforeNetwork(t *testing.T) {
	b := &Backend{}
	zero, one := 0, 1
	for name, request := range map[string]*model.GetTelemetryHistoryDataByPageReq{
		"nil request":       nil,
		"page without size": {Page: &one},
		"zero page":         {Page: &zero, PageSize: &one},
		"zero size":         {Page: &one, PageSize: &zero},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := b.GetHistoryTelemetrDataByPage(request); err == nil {
				t.Fatal("invalid history page must fail before using the nil sink")
			}
		})
	}
	if _, err := b.GetHistoryTelemetrDataByExport(nil, 0, 1); err == nil {
		t.Fatal("nil export request must fail")
	}
}

func floatPointer(v float64) *float64 { return &v }
func boolPointer(v bool) *bool        { return &v }
func stringPointer(v string) *string  { return &v }
