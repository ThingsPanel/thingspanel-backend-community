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

type DataTranspondController struct {
	beego.Controller
}

// 列表
func (c *DataTranspondController) List() {
	reqData := valid.PaginationValidate{}
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
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DataTranspondService services.DataTranspondService
	isSuccess, d, t := DataTranspondService.GetDataTranspondList(reqData, tenantId)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspPaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 编辑
func (c *DataTranspondController) Edit() {
	reqData := valid.DataTranspondValidate{}
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
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DataTranspondService services.DataTranspondService
	DataTranspond := models.DataTranspond{
		Id:          reqData.Id,
		ProcessId:   reqData.ProcessId,
		ProcessType: reqData.ProcessType,
		Label:       reqData.Label,
		Disabled:    reqData.Disabled,
		Info:        reqData.Info,
		Env:         reqData.Env,
		CustomerId:  reqData.CustomerId,
		RoleType:    reqData.RoleType,
		TenantId:    tenantId,
	}
	isSucess := DataTranspondService.EditDataTranspond(DataTranspond)
	if isSucess {
		response.SuccessWithDetailed(200, "success", DataTranspond, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}
}

// 新增
func (c *DataTranspondController) Add() {
	reqData := valid.AddDataTranspondValidate{}
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
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DataTranspondService services.DataTranspondService
	id := uuid.GetUuid()
	DataTranspond := models.DataTranspond{
		Id:          id,
		ProcessId:   reqData.ProcessId,
		ProcessType: reqData.ProcessType,
		Label:       reqData.Label,
		Disabled:    reqData.Disabled,
		Info:        reqData.Info,
		Env:         reqData.Env,
		CustomerId:  reqData.CustomerId,
		CreatedAt:   time.Now().Unix(),
		RoleType:    reqData.RoleType,
		TenantId:    tenantId,
	}
	isSucess, d := DataTranspondService.AddDataTranspond(DataTranspond)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(c.Ctx))
	}
}

// 删除
func (c *DataTranspondController) Delete() {
	reqData := valid.DataTranspondValidate{}
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
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DataTranspondService services.DataTranspondService
	DataTranspond := models.DataTranspond{
		Id:       reqData.Id,
		TenantId: tenantId,
	}
	isSucess := DataTranspondService.DeleteDataTranspond(DataTranspond)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}
}
