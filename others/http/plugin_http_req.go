package tphttp

import "net/http"

// 获取插件的表单配置
func GetPluginFromConfig() ([]byte, error) {
	return Get("http://127.0.0.1:503/api/form/config")
}

// 删除子设备配置
func DeleteDeviceConfig(reqdata []byte) (*http.Response, error) {
	return PostJson("http://127.0.0.1:503/api/device/config/delete", reqdata)
}

// 修改子设备配置
func UpdateDeviceConfig(reqdata []byte) (*http.Response, error) {
	return PostJson("http://127.0.0.1:503/api/device/config/update", reqdata)
}

// 新增子设备配置
func AddDeviceConfig(reqdata []byte) (*http.Response, error) {
	return PostJson("http://127.0.0.1:503/api/device/config/add", reqdata)
}
