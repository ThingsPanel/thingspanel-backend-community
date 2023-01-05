package tphttp

import (
	"net/http"
)

// func getHost() string {
// 	MqttHttpHost := os.Getenv("PLUGIN_HTTP_HOST")
// 	if MqttHttpHost == "" {
// 		MqttHttpHost = viper.GetString("plugin.http_host")
// 	}
// 	return MqttHttpHost
// }

// 获取插件的表单配置
func GetPluginFromConfig(host string, protocol_type string, device_type string) ([]byte, error) {
	if protocol_type[0:4] == "WVP_" {
		return nil, nil
	}
	//host := getHost()
	return Get("http://" + host + "/api/form/config?protocol_type=" + protocol_type + "&device_type=" + device_type)
}

// 删除子设备配置
func DeleteDeviceConfig(reqdata []byte, host string) (*http.Response, error) {
	//host := getHost()
	return PostJson("http://"+host+"/api/device/config/delete", reqdata)
}

// 修改子设备配置
func UpdateDeviceConfig(reqdata []byte, host string) (*http.Response, error) {
	//host := getHost()
	return PostJson("http://"+host+"/api/device/config/update", reqdata)
}

// 新增子设备配置
func AddDeviceConfig(reqdata []byte, host string) (*http.Response, error) {
	//host := getHost()
	return PostJson("http://"+host+"/api/device/config/add", reqdata)
}
