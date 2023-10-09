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

type DeviceModelController struct {
	beego.Controller
}

// 列表
func (c *DeviceModelController) List() {
	reqData := valid.DeviceModelPaginationValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DeviceModelService services.DeviceModelService
	isSuccess, d, t := DeviceModelService.GetDeviceModelList(reqData, tenantId)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspDeviceModelPaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 编辑
func (c *DeviceModelController) Edit() {
	reqData := valid.DeviceModelValidate{}
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
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DeviceModelService services.DeviceModelService
	isSucess := DeviceModelService.EditDeviceModel(reqData, tenantId)
	if isSucess {
		d := DeviceModelService.GetDeviceModelDetail(reqData.Id)
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}
}

// 新增
func (c *DeviceModelController) Add() {
	reqData := valid.AddDeviceModelValidate{}
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
	var DeviceModelService services.DeviceModelService
	id := uuid.GetUuid()
	DeviceModel := models.DeviceModel{
		ID:        id,
		ModelName: reqData.ModelName,
		Flag:      1,
		ChartData: reqData.ChartData,
		ModelType: reqData.ModelType,
		Describe:  reqData.Describe,
		Version:   reqData.Version,
		Author:    reqData.Author,
		CreatedAt: time.Now().Unix(),
		Sort:      reqData.Sort,
		Issued:    reqData.Issued,
		Remark:    reqData.Remark,
		TenantId:  tenantId,
	}
	if DeviceModel.ChartData == "" {
		DeviceModel.ChartData = "{}"
	}
	isSucess, d := DeviceModelService.AddDeviceModel(DeviceModel)
	if isSucess {
		response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "新增失败", (*context2.Context)(c.Ctx))
	}
}

// 删除
func (c *DeviceModelController) Delete() {
	reqData := valid.DeviceModelValidate{}
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
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DeviceModelService services.DeviceModelService
	DeviceModel := models.DeviceModel{
		ID:       reqData.Id,
		TenantId: tenantId,
	}
	isSucess := DeviceModelService.DeleteDeviceModel(DeviceModel)
	if isSucess {
		response.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}
}

// 获取树
func (c *DeviceModelController) DeviceModelTree() {

	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	var DeviceModelService services.DeviceModelService
	trees := DeviceModelService.DeviceModelTree(tenantId)
	response.SuccessWithDetailed(200, "success", trees, map[string]string{}, (*context2.Context)(c.Ctx))
}
