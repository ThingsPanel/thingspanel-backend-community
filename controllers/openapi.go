package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"strings"
	"time"
)

type OpenApiController struct {
	beego.Controller
	services.OpenApiService
}

// 列表
func (OpenApiController *OpenApiController) List() {
	paginationValidate := valid.OpenApiPaginationValidate{}
	err := json.Unmarshal(OpenApiController.Ctx.Input.RequestBody, &paginationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(paginationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(paginationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(OpenApiController.Ctx))
			break
		}
		return
	}

	service := services.OpenApiService{}
	isSuccess, d, t := service.GetOpenApiAuthList(paginationValidate)
	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", OpenApiController.Ctx)
	}

	dd := valid.RspOpenapiAuthPaginationValidate{
		CurrentPage: paginationValidate.CurrentPage,
		PerPage:     paginationValidate.PerPage,
		Data:        d,
		Total:       t,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, OpenApiController.Ctx)
}

// 新增
func (c *OpenApiController) Add() {
	validate := valid.AddOpenapiAuthValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &validate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(validate)

	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(validate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, c.Ctx)
			break

		}
	}

	service := services.OpenApiService{}

	appKey := service.GenerateKey()
	secretKey := service.GenerateKey()
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		utils.SuccessWithMessage(400, "代码逻辑错误", c.Ctx)
		return
	}
	openapiAuth := models.TpOpenapiAuth{
		ID:                utils.GetUuid(),
		TenantId:          tenantId,
		Name:              validate.Name,
		AppKey:            appKey,
		SecretKey:         secretKey,
		SignatureMode:     validate.SignatureMode,
		IpWhitelist:       validate.IpWhitelist,
		DeviceAccessScope: validate.DeviceAccessScope,
		ApiAccessScope:    validate.ApiAccessScope,
		Description:       validate.Description,
		CreatedAt:         time.Now().Unix(),
	}

	d, err := service.AddOpenapiAuth(openapiAuth)
	if err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, c.Ctx)
	} else {
		utils.SuccessWithMessage(400, err.Error(), c.Ctx)
	}
}

// 修改
func (c *OpenApiController) Edit() {
	validate := valid.OpenapiAuthValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &validate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(validate)

	if !status {
		for _, err := range v.Errors {
			//	获取字段名称
			// 获取字段别称
			alias := gvalid.GetAlias(validate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, c.Ctx)
			break

		}
	}
	if validate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", c.Ctx)
	}
	service := services.OpenApiService{}

	rsp_err := service.EditOpenApiAuth(validate)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", c.Ctx)
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), c.Ctx)
	}
}

// 删除
func (c *OpenApiController) Delete() {
	validate := valid.OpenapiAuthValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &validate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}

	if validate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", c.Ctx)
	}
	service := services.OpenApiService{}

	rsp_err := service.DelOpenApiAuthById(validate.Id)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", c.Ctx)
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), c.Ctx)
	}
}

// openapi 接口列表
func (c *OpenApiController) ApiList() {
	paginationValidate := valid.ApiPaginationValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &paginationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(paginationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(paginationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, c.Ctx)
			break
		}
		return
	}

	service := services.OpenApiService{}
	isSuccess, d, t := service.GetApiList(paginationValidate)
	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", c.Ctx)
	}

	dd := valid.RspApiPaginationValidate{
		CurrentPage: paginationValidate.CurrentPage,
		PerPage:     paginationValidate.PerPage,
		Data:        d,
		Total:       t,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, c.Ctx)
}

// api新增
func (c *OpenApiController) ApiAdd() {
	validate := valid.AddApiValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &validate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(validate)

	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(validate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, c.Ctx)
			break

		}
	}

	service := services.OpenApiService{}
	api := models.TpApi{
		ID:          utils.GetUuid(),
		Name:        validate.Name,
		Url:         validate.Url,
		ApiType:     validate.ApiType,
		ServiceType: validate.ServiceType,
		Remark:      validate.Remark,
	}

	d, err := service.AddApi(api)
	if err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, c.Ctx)
	} else {
		utils.SuccessWithMessage(400, err.Error(), c.Ctx)
	}
}

// api修改
func (c *OpenApiController) ApiEdit() {
	validate := valid.ApiValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &validate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(validate)

	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(validate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, c.Ctx)
			break

		}
	}
	if validate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", c.Ctx)
	}
	service := services.OpenApiService{}

	rsp_err := service.EditApi(validate)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", c.Ctx)
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), c.Ctx)
	}
}

// api 删除
func (c *OpenApiController) ApiDelete() {
	validate := valid.ApiValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &validate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}

	if validate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", c.Ctx)
	}
	service := services.OpenApiService{}

	rsp_err := service.DelApiById(validate.Id)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", c.Ctx)
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), c.Ctx)
	}
}

// openapi 接口授权添加
func (c *OpenApiController) ROpenApiAdd() {
	validate := valid.AddROpenApiValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &validate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(validate)

	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(validate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, c.Ctx)
			break

		}
	}

	service := services.OpenApiService{}

	err = service.AddROpenApi(validate)
	if err == nil {
		utils.SuccessWithMessage(200, "success", c.Ctx)
	} else {
		utils.SuccessWithMessage(400, err.Error(), c.Ctx)
	}
}

// openapi 接口授权删除
func (c *OpenApiController) ROpenApiDelete() {
	validate := valid.ROpenApiValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &validate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(validate)

	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(validate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, c.Ctx)
			break

		}
	}
	if validate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", c.Ctx)
	}
	service := services.OpenApiService{}

	err = service.DelROpenApi(validate)
	if err == nil {
		utils.SuccessWithMessage(200, "success", c.Ctx)
	} else {
		utils.SuccessWithMessage(400, err.Error(), c.Ctx)
	}
}

// openapi 设备授权
func (c *OpenApiController) RDeviceEdit() {
	validate := valid.RDeviceValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &validate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(validate)

	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(validate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, c.Ctx)
			break

		}
	}
	if validate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", c.Ctx)
	}

	rsp_err := c.EditRDevice(validate)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", c.Ctx)
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), c.Ctx)
	}
}
