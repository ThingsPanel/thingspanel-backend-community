package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpScriptController struct {
	beego.Controller
}

// 列表
func (c *TpScriptController) List() {
	reqData := valid.TpScriptPaginationValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		utils.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpScriptService services.TpScriptService
	isSuccess, d, t := TpScriptService.GetTpScriptList(reqData, tenantId)

	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspTpScriptPaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 编辑
func (TpScriptController *TpScriptController) Edit() {
	TpScriptValidate := valid.TpScriptValidate{}
	err := json.Unmarshal(TpScriptController.Ctx.Input.RequestBody, &TpScriptValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpScriptValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpScriptValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpScriptController.Ctx))
			break
		}
		return
	}
	if TpScriptValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpScriptController.Ctx))
	}
	var TpScriptService services.TpScriptService
	isSucess := TpScriptService.EditTpScript(TpScriptValidate)
	if isSucess {
		d := TpScriptService.GetTpScriptDetail(TpScriptValidate.Id)
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpScriptController.Ctx))
	} else {
		utils.SuccessWithMessage(400, "编辑失败", (*context2.Context)(TpScriptController.Ctx))
	}
}

// 新增
func (c *TpScriptController) Add() {
	reqData := valid.AddTpScriptValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		utils.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpScriptService services.TpScriptService
	id := utils.GetUuid()
	TpScript := models.TpScript{
		Id:             id,
		ProtocolType:   reqData.ProtocolType,
		ScriptName:     reqData.ScriptName,
		Company:        reqData.Company,
		CreatedAt:      time.Now().Unix(),
		ProductName:    reqData.ProductName,
		ScriptContentA: reqData.ScriptContentA,
		ScriptContentB: reqData.ScriptContentB,
		ScriptType:     reqData.ScriptType,
		Remark:         reqData.Remark,
		DeviceType:     reqData.DeviceType,
		TenantId:       tenantId,
	}
	d, rsp_err := TpScriptService.AddTpScript(TpScript)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
	}
}

// 删除
func (TpScriptController *TpScriptController) Delete() {
	TpScriptIdValidate := valid.TpScriptIdValidate{}
	err := json.Unmarshal(TpScriptController.Ctx.Input.RequestBody, &TpScriptIdValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TpScriptIdValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TpScriptIdValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpScriptController.Ctx))
			break
		}
		return
	}
	if TpScriptIdValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpScriptController.Ctx))
	}
	var TpScriptService services.TpScriptService
	TpScript := models.TpScript{
		Id: TpScriptIdValidate.Id,
	}
	req_err := TpScriptService.DeleteTpScript(TpScript)
	if req_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(TpScriptController.Ctx))
	} else {
		utils.SuccessWithMessage(400, "删除失败", (*context2.Context)(TpScriptController.Ctx))
	}
}
