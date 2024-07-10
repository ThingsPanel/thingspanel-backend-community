package http_client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sirupsen/logrus"
)

/*
- 有子设备关联的设备配置不能更换协议类型
*/

type RspData struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type RspDeviceListData struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Data    ListData `json:"data"`
}
type ListData struct {
	Total int          `json:"total"`
	List  []DeviceData `json:"list"`
}
type DeviceData struct {
	DeviceName   string `json:"device_name"`
	DeviceNumber string `json:"device_number"`
	Description  string `json:"description"`
	IsBind       bool   `json:"is_bind"`
}

// 获取插件的表单配置
// CONFIG-配置表单 VOUCHER-凭证表单 VOUCHER-TYPE-凭证类型表单
// func GetPluginFromConfig(host string, protocol_type string, device_type string, form_type string, voucher_type string) ([]byte, error) {
// 	return Get("http://" + host + "/api/v1/form/config?protocol_type=" + protocol_type + "&device_type=" + device_type + "&form_type=" + form_type + "&voucher_type=" + voucher_type)
// }

// /api/v2/form/config
// CFG-配置表单 VCR-凭证表单 VCRT-凭证类型表单 SVCRT-服务凭证表单
func GetPluginFromConfigV2(host string, service_identifier string, device_type string, form_type string) (interface{}, error) {
	b, err := Get("http://" + host + "/api/v1/form/config?protocol_type=" + service_identifier + "&device_type=" + device_type + "&form_type=" + form_type)
	if err != nil {
		logrus.Error(err)
		return nil, fmt.Errorf("get plugin form failed: %s", err)
	}
	// 解析表单
	var rspdata RspData
	err = json.Unmarshal(b, &rspdata)
	if err != nil {
		logrus.Error(err)
		return nil, fmt.Errorf("unmarshal response data failed: %s", err)
	}
	if rspdata.Code != 200 {
		err = fmt.Errorf("protocol plugin response message: %s", rspdata.Message)
		logrus.Error(err)
	}
	return rspdata.Data, nil
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

// messageType 1-服务配置修改
func Notification(messageType string, message string, host string) ([]byte, error) {
	type ReqData struct {
		MessageType string `json:"message_type"`
		Message     string `json:"message"`
	}
	reqData := ReqData{MessageType: messageType, Message: message}
	reqDataBytes, err := json.Marshal(reqData)
	if err != nil {
		return nil, err
	}
	response, err := PostJson("http://"+host+"/api/v1/notify/event", reqDataBytes)
	if err != nil {
		logrus.Error(err)
		return nil, fmt.Errorf("post plugin notification failed: %s", err)
	}
	if response.StatusCode != 200 {
		err = fmt.Errorf("protocol plugin response message: %s", response.Status)
		logrus.Error(err)
		return nil, err

	}
	// 读取body
	body, err := io.ReadAll(response.Body)
	if err != nil {
		logrus.Error(err)
		return nil, fmt.Errorf("read plugin response body failed: %s", err)
	}
	logrus.Info(string(body))

	return body, nil
}

// /api/v1/service/access/device/list
// 三方服务列表查询
func GetServiceAccessDeviceList(host string, voucher string, page_size string, page string) (*ListData, error) {
	b, err := Get("http://" + host + "/api/v1/plugin/device/list?voucher=" + voucher + "&page_size=" + page_size + "&page=" + page)
	if err != nil {
		logrus.Error(err)
		logrus.Error("http://" + host + "/api/v1/plugin/device/list?voucher=" + voucher + "&page_size=" + page_size + "&page=" + page)
		return nil, fmt.Errorf("get plugin form failed: %s", err)
	}
	// 解析表单
	var rspdata RspDeviceListData
	err = json.Unmarshal(b, &rspdata)
	if err != nil {
		logrus.Error(err)
		return nil, fmt.Errorf("unmarshal response data failed: %s", err)
	}
	if rspdata.Code != 200 {
		err = fmt.Errorf("protocol plugin response message: %s", rspdata.Message)
		logrus.Error(err)
	}
	return &rspdata.Data, nil
}
