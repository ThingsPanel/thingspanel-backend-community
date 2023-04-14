package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpProductController struct {
	beego.Controller
}

// 列表
func (c *TpProductController) List() {
	reqData := valid.TpProductPaginationValidate{}
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
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var TpProductService services.TpProductService
	isSuccess, d, t := TpProductService.GetTpProductList(reqData, tenantId)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspTpProductPaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 编辑
func (c *TpProductController) Edit() {
	reqData := valid.TpProductValidate{}
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
	var TpProductService services.TpProductService
	isSucess := TpProductService.EditTpProduct(reqData, tenantId)
	if isSucess {
		d := TpProductService.GetTpProductDetail(reqData.Id)
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}
}

// 新增
func (c *TpProductController) Add() {
	reqData := valid.AddTpProductValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(v, err.Field)
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
	var TpProductService services.TpProductService
	if reqData.Plugin == "" {
		reqData.Plugin = "{}"
	}
	id := uuid.GetUuid()
	TpProduct := models.TpProduct{
		Id:            id,
		ProtocolType:  reqData.ProtocolType,
		AuthType:      reqData.AuthType,
		Describe:      reqData.Describe,
		CreatedTime:   time.Now().Unix(),
		Name:          reqData.Name,
		Plugin:        reqData.Plugin,
		Remark:        reqData.Remark,
		SerialNumber:  reqData.SerialNumber,
		DeviceModelId: reqData.DeviceModelId,
		TenantId:      tenantId,
	}
	rsp_err, d := TpProductService.AddTpProduct(TpProduct)
	if rsp_err == nil {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		var err string
		isTrue := strings.Contains(rsp_err.Error(), "23505")
		if isTrue {
			err = "产品编号不能重复！"
		} else {
			err = rsp_err.Error()
		}
		response.SuccessWithMessage(400, err, (*context2.Context)(c.Ctx))
	}
}

// 删除
func (c *TpProductController) Delete() {
	reqData := valid.TpProductIdValidate{}
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
	var TpProductService services.TpProductService
	TpProduct := models.TpProduct{
		Id:       reqData.Id,
		TenantId: tenantId,
	}
	rsp_err := TpProductService.DeleteTpProduct(TpProduct)
	if rsp_err == nil {
		response.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		var err string
		isTrue := strings.Contains(rsp_err.Error(), "23503")
		if isTrue {
			err = "该产品下存在批次，请先删除批次！"
		} else {
			err = rsp_err.Error()
		}
		response.SuccessWithMessage(400, err, (*context2.Context)(c.Ctx))
	}
}
