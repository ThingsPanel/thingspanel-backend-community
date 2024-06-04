package http_client

import (
	"net/http"
)

/*
- 有子设备关联的设备配置不能更换协议类型
*/

type RspData struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// 获取插件的表单配置
// CONFIG-配置表单 VOUCHER-凭证表单 VOUCHER-TYPE-凭证类型表单
func GetPluginFromConfig(host string, protocol_type string, device_type string, form_type string, voucher_type string) ([]byte, error) {
	return Get("http://" + host + "/api/v1/form/config?protocol_type=" + protocol_type + "&device_type=" + device_type + "&form_type=" + form_type + "&voucher_type=" + voucher_type)
}

// 断开设备连接让设备重新连接
func DisconnectDevice(reqdata []byte, host string) (*http.Response, error) {
	return PostJson("http://"+host+"/api/v1/device/disconnect", reqdata)
}

// 删除设备或子设备通知（设备协议变更也被认为是删除）
func DeleteDevice(reqdata []byte, host string) (*http.Response, error) {
	return PostJson("http://"+host+"/api/v1/device/delete", reqdata)
}

// 设备或子设备配置变更通知
func UpdateDeviceConfig(reqdata []byte, host string) (*http.Response, error) {
	return PostJson("http://"+host+"/api/v1/device/config/update", reqdata)
}

// 新增设备或子设备通知（设备协议变更也被认为是新增）
func AddDevice(reqdata []byte, host string) (*http.Response, error) {
	return PostJson("http://"+host+"/api/v1/device/add", reqdata)
}
