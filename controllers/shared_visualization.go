package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type SharedVisualizationController struct {
	beego.Controller
}


// 根据指定的分享id获取分享信息
func (c *SharedVisualizationController) Get() {
	var SharedVisualizationService services.SharedVisualizationService
	GetShareInfoValidate := valid.GetShareInfoValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &GetShareInfoValidate)
	if err != nil {
		logs.Error("参数解析失败", err.Error())
	}
	
	v := validation.Validation{}
	status, _ := v.Valid(GetShareInfoValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(GetShareInfoValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}

	shareInfo, err := SharedVisualizationService.GetShareInfo(GetShareInfoValidate.Id)
	if err != nil {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	response.SuccessWithDetailed(200, "success", shareInfo, map[string]string{}, c.Ctx)
}


// 根据可视化id创建对应可视化的分享
func (c *SharedVisualizationController)GenerateShareId() {
	var TpDashboardService services.TpDashboardService
	var TpConsoleService services.ConsoleService
	var SharedVisualizationService services.SharedVisualizationService
	GenerateShareIdValidate := valid.GenerateShareIdValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &GenerateShareIdValidate)
	if err != nil {
		logs.Error("参数解析失败", err.Error())
	}
	
	v := validation.Validation{}
	status, _ := v.Valid(GenerateShareIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(GenerateShareIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	// 生成分享id
	shareId := utils.GetUuid()
	deviceListJSON := []byte("[]")
	// 根据不同的分享类型生成不同的分享信息
	if GenerateShareIdValidate.ShareType == "console" {
		// 保存分享id到可视化模型中
		flag := TpConsoleService.UpdateShareId(GenerateShareIdValidate.Id, shareId)
		if !flag {
			response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
			return
		}
		// 获取可视化绑定的设备列表
		deviceList, err := TpConsoleService.GetDeviceListByID(GenerateShareIdValidate.Id)
		jsonData, err := json.Marshal(deviceList)
		if err != nil {
			logs.Error("Error marshalling deviceList to JSON:", err)
			return
		}
		// 不存在时deviceListJSON为空数组
		if string(jsonData) != "null" {
			deviceListJSON = jsonData
		}
	} else {
		GenerateShareIdValidate.ShareType = "dashboard"

		// 保存分享id到可视化模型中
		flag := TpDashboardService.UpdateShareId(GenerateShareIdValidate.Id, shareId)
		if !flag {
			response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
			return
		}
		// 获取可视化绑定的设备列表
		deviceList, err := TpDashboardService.GetDeviceListByVisualizationID(GenerateShareIdValidate.Id)
		jsonData, err := json.Marshal(deviceList)
		if err != nil {
			logs.Error("Error marshalling deviceList to JSON:", err)
			return
		}
		// 不存在时deviceListJSON为空数组
		if string(jsonData) != "null" {
			deviceListJSON = jsonData
		}
	}

	// 创建可视化分享模型
	SharedVisualization := models.SharedVisualization{
		ShareID:    shareId,
		DeviceList: string(deviceListJSON),
		DashboardID: GenerateShareIdValidate.Id,
		CreatedAt:      time.Now().Unix(),
		ShareType: GenerateShareIdValidate.ShareType,
	}

	isSucess, d := SharedVisualizationService.CreateSharedVisualization(SharedVisualization)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(c.Ctx))
	}

}


