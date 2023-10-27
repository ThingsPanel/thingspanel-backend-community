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

type ShareVisualizationController struct {
	beego.Controller
}


// 根据指定的分享id获取分享信息
func (c *ShareVisualizationController) Get() {
	var SharedVisualizationService services.ShareVisualizationService
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
func (c *ShareVisualizationController)GenerateShareId() {
	var TpDashboardService services.TpDashboardService
	var SharedVisualizationService services.ShareVisualizationService
	GetShareLinkValidate := valid.GetShareLinkValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &GetShareLinkValidate)
	if err != nil {
		logs.Error("参数解析失败", err.Error())
	}
	
	v := validation.Validation{}
	status, _ := v.Valid(GetShareLinkValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(GetShareLinkValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	// 生成分享id
	shareId := utils.GetUuid()

	// 保存分享id到可视化模型中
	flag := TpDashboardService.UpdateShareId(GetShareLinkValidate.Id, shareId)
	if !flag {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	// 获取可视化绑定的设备列表
	deviceList, err := TpDashboardService.GetDeviceListByVisualizationID(GetShareLinkValidate.Id)
	deviceListJSON, err := json.Marshal(deviceList)
	if err != nil {
		logs.Error("Error marshalling deviceList to JSON:", err)
		return
	}
	// 不存在时deviceListJSON为空数组
	if string(deviceListJSON) == "null" {
		deviceListJSON = []byte("[]")
	}

	// 创建可视化分享模型
	ShareVisualization := models.SharedVisualization{
		ShareID:    shareId,
		DeviceList: string(deviceListJSON),
		DashboardID: GetShareLinkValidate.Id,
		CreatedAt:      time.Now().Unix(),
	}

	isSucess, d := SharedVisualizationService.CreateSharedVisualization(ShareVisualization)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(c.Ctx))
	}

}


