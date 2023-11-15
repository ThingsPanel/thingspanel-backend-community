package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpDashboardController struct {
	beego.Controller
}

// 列表
func (c *TpDashboardController) List() {
	reqData := valid.TpDashboardPaginationValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	// 不存在租户id且无分享id时，返回错误
	if !ok && reqData.ShareId == "" {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpDashboardService services.TpDashboardService
	isSuccess, d, t := TpDashboardService.GetTpDashboardList(reqData, tenantId)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspTpDashboardPaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 编辑
func (c *TpDashboardController) Edit() {
	reqData := valid.TpDashboardValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	if reqData.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(c.Ctx))
	}
	var TpDashboardService services.TpDashboardService
	isSucess := TpDashboardService.EditTpDashboard(reqData, tenantId)
	if isSucess {
		// 存在分享id时，更新对应设备id到分享可视化里
		d := TpDashboardService.GetTpDashboardDetail(reqData.Id)
		if len(d) != 0 {
			shareId := d[0].ShareId
			JsonData := d[0].JsonData
			if shareId != "" {
				var SharedVisualizationService services.SharedVisualizationService
				deviceList, err := utils.GetDeviceListByVisualizationData(JsonData)
				deviceListJSON, err := json.Marshal(deviceList)
				if err == nil {
					isSaved := SharedVisualizationService.UpdateDeviceList(reqData.Id, string(deviceListJSON))
					logs.Info("update shared device id list", isSaved)
				}
			}
		}

		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}
}

// 新增
func (c *TpDashboardController) Add() {
	AddTpDashboardValidate := valid.AddTpDashboardValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &AddTpDashboardValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AddTpDashboardValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AddTpDashboardValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpDashboardService services.TpDashboardService
	id := uuid.GetUuid()
	TpDashboard := models.TpDashboard{
		Id:            id,
		RelationId:    AddTpDashboardValidate.RelationId,
		JsonData:      AddTpDashboardValidate.JsonData,
		DashboardName: AddTpDashboardValidate.DashboardName,
		CreateAt:      time.Now().Unix(),
		Sort:          AddTpDashboardValidate.Sort,
		Remark:        AddTpDashboardValidate.Remark,
		TenantId:      tenantId,
	}
	if TpDashboard.JsonData == "" {
		TpDashboard.JsonData = "{}"
	}
	isSucess, d := TpDashboardService.AddTpDashboard(TpDashboard)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(c.Ctx))
	}
}

// 删除
func (c *TpDashboardController) Delete() {
	reqData := valid.TpDashboardValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	if reqData.Id == "" {
		response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(c.Ctx))
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpDashboardService services.TpDashboardService
	TpDashboard := models.TpDashboard{
		Id:       reqData.Id,
		TenantId: tenantId,
	}
	isSucess := TpDashboardService.DeleteTpDashboard(TpDashboard)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}
}
