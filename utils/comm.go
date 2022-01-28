package utils

import (
	"fmt"
)

func TsKvFilterToSql(filters map[string]interface{}) (string, []interface{}) {
	SQL := " WHERE 1=1 "
	params := []interface{}{}
	for key, value := range filters {
		switch key {
		case "start_date":
			SQL = fmt.Sprintf("%s and ts_kv.ts >= ?", SQL)
			params = append(params, value)
		case "end_date":
			SQL = fmt.Sprintf("%s and ts_kv.ts < ?", SQL)
			params = append(params, value)
		case "business_id":
			SQL = fmt.Sprintf("%s and business.id = ?", SQL)
			params = append(params, value)
		case "asset_id":
			SQL = fmt.Sprintf("%s and asset.id = ?", SQL)
			params = append(params, value)
		case "token":
			SQL = fmt.Sprintf("%s and device.token = ?", SQL)
			params = append(params, value)
		}
	}
	return SQL, params
}

func WidgetsToSql(filters map[string]interface{}) (string, []interface{}) {
	SQL := "1=1"
	params := []interface{}{}
	for key, value := range filters {
		switch key {
		case "dashboard_id":
			SQL = fmt.Sprintf("%s and dashboard_id = ?", SQL)
			params = append(params, value)
		case "asset_id":
			SQL = fmt.Sprintf("%s and asset_id = ?", SQL)
			params = append(params, value)
		case "device_id":
			SQL = fmt.Sprintf("%s and device_id = ?", SQL)
			params = append(params, value)
		}
	}
	return SQL, params
}
