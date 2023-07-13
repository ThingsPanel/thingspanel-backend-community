package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpLocalVisPluginController struct {
	beego.Controller
}

// 列表
func (c *TpLocalVisPluginController) List() {
	reqData := valid.TpLocalVisPluginPaginationValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	var tplocalvisplugin services.TpLocalVis
	isSuccess, d, t := tplocalvisplugin.GetTpLocalVisPluginList(reqData)
	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspTpLocalVisPluginPaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 新增
func (c *TpLocalVisPluginController) Add() {
	reqData := valid.AddTpLocalVisPluginValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var tplocalvisplugin services.TpLocalVis

	if sd := tplocalvisplugin.GetTpLocalVisPluginDetail(reqData.Id, tenantId); len(sd) > 0 {
		utils.SuccessWithMessage(400, "id已存在", (*context2.Context)(c.Ctx))
		return
	}
	d, rsp_err := tplocalvisplugin.AddTpLocalVisPlugin(reqData, tenantId)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
	}
}

// 编辑
func (c *TpLocalVisPluginController) Edit() {
	reqData := valid.EditTpLocalVisPluginValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var tplocalvisplugin services.TpLocalVis
	err := tplocalvisplugin.EditTpLocalVisPlugin(reqData, tenantId)
	if err == nil {
		d := tplocalvisplugin.GetTpLocalVisPluginDetail(reqData.Id, tenantId)
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
	}
}

// 删除
func (c *TpLocalVisPluginController) Del() {
	reqData := valid.TpLocalVisPluginIdValidate{}
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
			utils.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	if reqData.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(c.Ctx))
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var tplocalvisplugin services.TpLocalVis
	TpLocalVisPlugin := models.TpLocalVisPlugin{
		Id:       reqData.Id,
		TenantId: tenantId,
	}
	req_err := tplocalvisplugin.DeleteTpLocalVisPlugin(TpLocalVisPlugin)
	if req_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, "删除失败", (*context2.Context)(c.Ctx))
	}

}
