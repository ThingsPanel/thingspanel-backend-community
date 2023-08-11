package controller

import (
	"ThingsPanel-Go/controllers"
	gvalid "ThingsPanel-Go/initialize/validate"
	services2 "ThingsPanel-Go/openapi/service"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"strings"
)

type OpenapiDeviceController struct {
	beego.Controller
}

// 设备在线离线状态
func (c *OpenapiDeviceController) DeviceStatus() {
	deviceIdValidate := valid.DeviceIdValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &deviceIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var DeviceService services2.OpenapiDeviceService
	isOk := DeviceService.IsAccessDeviceId(c.Ctx, deviceIdValidate.DeviceId)
	if !isOk {
		response.SuccessWithMessage(401, "无设备访问权限", (*context2.Context)(c.Ctx))
	}
	v := validation.Validation{}
	status, _ := v.Valid(deviceIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(deviceIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}

	d, err := DeviceService.GetDeviceOnlineStatusOne(deviceIdValidate)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 设备事件上报历史纪录查询
func (c *OpenapiDeviceController) DeviceEventHistoryList() {
	inputData := valid.OpenapiDeviceEventCommandHistoryValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &inputData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var s services2.OpenapiDeviceService
	if !s.IsAccessDeviceId(c.Ctx, inputData.DeviceId) {
		response.SuccessWithMessage(401, "无设备访问权限", c.Ctx)
	}

	v := validation.Validation{}
	status, _ := v.Valid(inputData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(inputData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}

	data, count := s.GetDeviceEvnetHistoryList(inputData)
	d := controllers.DataTransponList{
		CurrentPage: inputData.CurrentPage,
		Total:       count,
		PerPage:     inputData.PerPage,
		Data:        data,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))

}

// 设备命令下发历史纪录查询
func (c *OpenapiDeviceController) DeviceCommandHistoryList() {
	inputData := valid.DeviceEventCommandHistoryValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &inputData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	var s services2.OpenapiDeviceService
	if !s.IsAccessDeviceId(c.Ctx, inputData.DeviceId) {
		response.SuccessWithMessage(401, "无设备访问权限", c.Ctx)
	}
	offset := (inputData.CurrentPage - 1) * inputData.PerPage

	data, count := s.GetDeviceCommandHistoryListByDeviceId(offset, inputData.PerPage, inputData.DeviceId)
	d := controllers.DataTransponList{
		CurrentPage: inputData.CurrentPage,
		Total:       count,
		PerPage:     inputData.PerPage,
		Data:        data,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 向设备发送命令
func (c *OpenapiDeviceController) DeviceCommandSend() {
	inputData := valid.DeviceCommandSendValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &inputData)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	var s services2.OpenapiDeviceService
	if !s.IsAccessDeviceId(c.Ctx, inputData.DeviceId) {
		response.SuccessWithMessage(401, "无设备访问权限", c.Ctx)
	}

	device, i := s.Token(inputData.DeviceId)
	if i == 0 {
		response.SuccessWithMessage(400, "no device", (*context2.Context)(c.Ctx))
		return
	}

	// if device.Protocol != "mqtt" && device.Protocol != "MQTT" {
	// 	response.SuccessWithMessage(400, "protocol error", (*context2.Context)(c.Ctx))
	// }

	//

	err = s.SendCommandToDevice(
		device, inputData.CommandIdentifier,
		[]byte(inputData.CommandData),
		inputData.CommandName,
		inputData.Desc)
	if err != nil {
		response.SuccessWithMessage(400, "命令下发失败:"+err.Error(), (*context2.Context)(c.Ctx))
	}

	response.SuccessWithDetailed(200, "success", nil, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 设备总数和设备在线数
func (c *OpenapiDeviceController) DeviceCountOnlineCount() {

	var s services2.OpenapiDeviceService
	data, err := s.GetDeviceCountOnlineCount(c.Ctx)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	response.SuccessWithDetailed(200, "success", data, map[string]string{}, (*context2.Context)(c.Ctx))
}
