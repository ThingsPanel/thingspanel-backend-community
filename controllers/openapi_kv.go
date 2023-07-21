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
	"log"
	"strings"
)

type OpenapiKvController struct {
	beego.Controller
	services.OpenApiCommonService
}

// 根据业务获取所有设备和设备当前KV
func (this *OpenapiKvController) CurrentDataByBusiness() {
	CurrentKVByBusiness := valid.CurrentKVByBusiness{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &CurrentKVByBusiness)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(CurrentKVByBusiness)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(CurrentKVByBusiness, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t := TSKVService.GetCurrentDataByBusiness(CurrentKVByBusiness.BusinessiD)
	log.Println(t)
	response.SuccessWithDetailed(200, "获取成功", t, map[string]string{}, (*context2.Context)(this.Ctx))
}

// 根据设备分组获取所有设备和设备当前KV
func (this *OpenapiKvController) CurrentDataByAsset() {
	CurrentKVByAsset := valid.CurrentKVByAsset{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &CurrentKVByAsset)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(CurrentKVByAsset)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(CurrentKVByAsset, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t := TSKVService.GetCurrentDataByAsset(CurrentKVByAsset.AssetId)
	log.Println(t)
	response.SuccessWithDetailed(200, "获取成功", t, map[string]string{}, (*context2.Context)(this.Ctx))
}

// 根据设备分组获取所有设备和设备当前KV app
func (this *OpenapiKvController) CurrentDataByAssetA() {
	CurrentKVByAsset := valid.CurrentKVByAsset{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &CurrentKVByAsset)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(CurrentKVByAsset)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(CurrentKVByAsset, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t := TSKVService.GetCurrentDataByAssetA(CurrentKVByAsset.AssetId)
	log.Println(t)
	response.SuccessWithDetailed(200, "获取成功", t, map[string]string{}, (*context2.Context)(this.Ctx))
}

// 根据设备id分页查询当前kv
func (KvController *OpenapiKvController) DeviceHistoryData() {
	DeviceHistoryDataValidate := valid.DeviceHistoryDataValidate{}
	err := json.Unmarshal(KvController.Ctx.Input.RequestBody, &DeviceHistoryDataValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(DeviceHistoryDataValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DeviceHistoryDataValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(KvController.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t, count := TSKVService.DeviceHistoryData(DeviceHistoryDataValidate.DeviceId, DeviceHistoryDataValidate.Current, DeviceHistoryDataValidate.Size)
	var data = make(map[string]interface{})
	data["data"] = t
	data["count"] = count
	response.SuccessWithDetailed(200, "获取成功", data, map[string]string{}, (*context2.Context)(KvController.Ctx))
}

// 查询历史数据
func (KvController *OpenapiKvController) HistoryData() {
	HistoryDataValidate := valid.HistoryDataValidate{}
	err := json.Unmarshal(KvController.Ctx.Input.RequestBody, &HistoryDataValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	if !KvController.IsAccessDeviceId(KvController.Ctx, HistoryDataValidate.DeviceId) {
		response.SuccessWithMessage(401, "无设备访问权限", KvController.Ctx)
	}
	v := validation.Validation{}
	status, _ := v.Valid(HistoryDataValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(HistoryDataValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(KvController.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	trees := TSKVService.GetHistoryData(HistoryDataValidate.DeviceId, HistoryDataValidate.Attribute, HistoryDataValidate.StartTs, HistoryDataValidate.EndTs, HistoryDataValidate.Rate)
	response.SuccessWithDetailed(200, "success", trees, map[string]string{}, (*context2.Context)(KvController.Ctx))
}

// 获取设备当前值
func (KvController *OpenapiKvController) GetCurrentDataAndMap() {
	CurrentKV := valid.CurrentKV{}
	err := json.Unmarshal(KvController.Ctx.Input.RequestBody, &CurrentKV)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	if !KvController.IsAccessDeviceId(KvController.Ctx, CurrentKV.EntityID) {
		response.SuccessWithMessage(401, "无设备访问权限", KvController.Ctx)
	}
	v := validation.Validation{}
	status, _ := v.Valid(CurrentKV)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(CurrentKV, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(KvController.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	m, err := TSKVService.GetCurrentDataAndMap(CurrentKV.EntityID, CurrentKV.Attribute)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(KvController.Ctx))
	}
	response.SuccessWithDetailed(200, "success", m, map[string]string{}, (*context2.Context)(KvController.Ctx))
}
