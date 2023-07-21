package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
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
	services.OpenApiCommonService
}

// 设备在线离线状态
func (c *OpenapiDeviceController) DeviceStatus() {
	DeviceMapValidate := valid.DeviceIdListValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &DeviceMapValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	isAll, ids := c.GetAccessDeviceIds(c.Ctx, DeviceMapValidate.DeviceIdList)
	if !isAll {
		if len(ids) == 0 {
			response.SuccessWithMessage(401, "无设备访问权限", (*context2.Context)(c.Ctx))
		} else {
			DeviceMapValidate.DeviceIdList = ids
		}
	}
	v := validation.Validation{}
	status, _ := v.Valid(DeviceMapValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DeviceMapValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	d, err := DeviceService.GetDeviceOnlineStatus(DeviceMapValidate)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 设备事件上报历史纪录查询
func (c *OpenapiDeviceController) DeviceEventHistoryList() {
	inputData := valid.DeviceEventCommandHistoryValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &inputData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	if !c.IsAccessDeviceId(c.Ctx, inputData.DeviceId) {
		response.SuccessWithMessage(401, "无设备访问权限", c.Ctx)
	}
	offset := (inputData.CurrentPage - 1) * inputData.PerPage
	var s services.DeviceEvnetHistory
	data, count := s.GetDeviceEvnetHistoryListByDeviceId(offset, inputData.PerPage, inputData.DeviceId)
	d := DataTransponList{
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

	if !c.IsAccessDeviceId(c.Ctx, inputData.DeviceId) {
		response.SuccessWithMessage(401, "无设备访问权限", c.Ctx)
	}
	offset := (inputData.CurrentPage - 1) * inputData.PerPage
	var s services.DeviceCommandHistory

	data, count := s.GetDeviceCommandHistoryListByDeviceId(offset, inputData.PerPage, inputData.DeviceId)
	d := DataTransponList{
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

	if !c.IsAccessDeviceId(c.Ctx, inputData.DeviceId) {
		response.SuccessWithMessage(401, "无设备访问权限", c.Ctx)
	}
	var d services.DeviceService
	device, i := d.Token(inputData.DeviceId)
	if i == 0 {
		response.SuccessWithMessage(400, "no device", (*context2.Context)(c.Ctx))
		return
	}

	// if device.Protocol != "mqtt" && device.Protocol != "MQTT" {
	// 	response.SuccessWithMessage(400, "protocol error", (*context2.Context)(c.Ctx))
	// }

	//

	d.SendCommandToDevice(
		device, inputData.CommandIdentifier,
		[]byte(inputData.CommandData),
		inputData.CommandName,
		inputData.Desc)

	response.SuccessWithDetailed(200, "success", nil, map[string]string{}, (*context2.Context)(c.Ctx))
}
