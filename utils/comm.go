package utils

import (
	"errors"
	"fmt"
	"strings"
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

//用户输入组合路径安全校验
func CheckPath(param string) error {
	if count := strings.Count(param, "."); count > 0 {
		return errors.New("路径中不能包含非法字符“.”")
	}
	if count := strings.Count(param, "/"); count > 0 {
		return errors.New("路径中不能包含非法字符“/”")
	}
	if count := strings.Count(param, "\\"); count > 0 {
		return errors.New("路径中不能包含非法字符“\\”")
	}
	return nil
}

//用户输入文件名安全校验
func CheckFilename(param string) error {
	if count := strings.Count(param, "."); count > 1 {
		return errors.New("文件名中不能超过一个“.”")
	}
	if count := strings.Count(param, "/"); count > 0 {
		return errors.New("文件名中不能包含非法字符“/”")
	}
	if count := strings.Count(param, "\\"); count > 0 {
		return errors.New("文件名中不能包含非法字符“\\”")
	}
	return nil
}

//字符串替换非法字符
func ReplaceUserInput(s string) string {
	newStringInput := strings.NewReplacer("\n", " ", "\r", " ")
	return newStringInput.Replace(s)
}

//字符包含非法字符
func ContainsIllegal(target string) bool {
	var str_array [3]string = [3]string{"/", "./", "\\"}
	for _, element := range str_array {
		if strings.Contains(target, element) {
			return true
		}
	}
	return false
}
