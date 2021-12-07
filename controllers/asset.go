package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type AssetController struct {
	beego.Controller
}

type AssetData struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	CustomerID string       `json:"customer_id"`
	BusinessID string       `json:"business_id"`
	WidgetID   string       `json:"widget_id"`
	WidgetName string       `json:"widget_name"`
	Device     []DeviceData `json:"device"`
	Two        []AssetData2 `json:"two"`
}

type DeviceData struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Type     string                `json:"type"`
	Disabled bool                  `json:"disabled"`
	Dm       string                `json:"dm"`
	State    string                `json:"state"`
	Dash     []services.Widget     `json:"dash"`
	Mapping  []models.FieldMapping `json:"mapping"`
}

type AssetData2 struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	CustomerID string       `json:"customer_id"`
	BusinessID string       `json:"business_id"`
	WidgetID   string       `json:"widget_id"`
	WidgetName string       `json:"widget_name"`
	Device     []DeviceData `json:"device"`
	There      []AssetData3 `json:"there"`
}

type AssetData3 struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	CustomerID string       `json:"customer_id"`
	BusinessID string       `json:"business_id"`
	WidgetID   string       `json:"widget_id"`
	WidgetName string       `json:"widget_name"`
	Device     []DeviceData `json:"device"`
}

// 设备列表
func (this *AssetController) Index() {
	var AssetService services.AssetService
	d := AssetService.List()
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 添加资产
func (this *AssetController) Add() {
	addAssetValidate := valid.AddAsset{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &addAssetValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(addAssetValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(addAssetValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var AssetService services.AssetService
	f := AssetService.Add(addAssetValidate.Data)
	if f {
		response.SuccessWithMessage(200, "插入成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "插入失败", (*context2.Context)(this.Ctx))
	return
}

// 编辑资产
func (this *AssetController) Edit() {
	editAssetValidate := valid.EditAsset{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &editAssetValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(editAssetValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(editAssetValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var AssetService services.AssetService
	f := AssetService.Edit(editAssetValidate.Data)
	if f {
		response.SuccessWithMessage(200, "编辑成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(this.Ctx))
	return
}

// 删除资产
func (this *AssetController) Delete() {
	deleteAssetValidate := valid.DeleteAsset{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &deleteAssetValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(deleteAssetValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(deleteAssetValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var AssetService services.AssetService
	if deleteAssetValidate.TYPE == 1 {
		_, c := AssetService.GetAssetsByParentID(deleteAssetValidate.ID)
		if c != 0 {
			response.SuccessWithMessage(400, "请先删除下一级", (*context2.Context)(this.Ctx))
			return
		}
		f := AssetService.Delete(deleteAssetValidate.ID)
		if f {
			var DeviceService services.DeviceService
			var FieldMappingService services.FieldMappingService
			d, s := DeviceService.GetDevicesByAssetID(deleteAssetValidate.ID)
			if s != 0 {
				for _, ds := range d {
					DeviceService.Delete(ds.ID)
					FieldMappingService.DeleteByDeviceId(ds.ID)
				}
			}
			response.SuccessWithMessage(200, "删除成功", (*context2.Context)(this.Ctx))
			return
		}
	} else {
		var DeviceService services.DeviceService
		var FieldMappingService services.FieldMappingService
		f1 := DeviceService.Delete(deleteAssetValidate.ID)
		f2 := FieldMappingService.DeleteByDeviceId(deleteAssetValidate.ID)
		if f1 && f2 {
			response.SuccessWithMessage(200, "删除成功", (*context2.Context)(this.Ctx))
			return
		}
	}
	response.SuccessWithMessage(400, "删除失败", (*context2.Context)(this.Ctx))
	return
}

// 获取组件
func (this *AssetController) Widget() {
	widgetAssetValidate := valid.WidgetAsset{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &widgetAssetValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(widgetAssetValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(widgetAssetValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var AssetService services.AssetService
	a := AssetService.Widget(widgetAssetValidate.ID)
	response.SuccessWithDetailed(200, "success", a, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 资产列表
func (this *AssetController) List() {
	listAssetValidate := valid.ListAsset{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &listAssetValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(listAssetValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(listAssetValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var disabled bool
	var dm string
	var state string
	var ResAssetData []AssetData
	var AssetService services.AssetService
	var DeviceService services.DeviceService
	var FieldMappingService services.FieldMappingService
	l, c := AssetService.GetAssetByBusinessId(listAssetValidate.BusinessID)
	if c != 0 {
		// 第一层
		for _, s := range l {
			d, cd := DeviceService.GetDevicesByAssetID(s.ID)
			var ResDeviceData []DeviceData
			if cd != 0 {
				for _, di := range d {
					if di.Type == "-1" {
						disabled = false
						dm = ""
						state = ""
					} else {
						disabled = true
						dm = "代码"
						state = "正常"
					}
					dd := AssetService.Widget(di.Type)
					var ResWidgetData []services.Widget
					if len(dd) > 0 {
						for _, wv := range dd {
							ResWidgetData = append(ResWidgetData, wv)
						}
					}
					fml, _ := FieldMappingService.GetByDeviceid(di.ID)
					rdi := DeviceData{
						ID:       di.ID,
						Name:     di.Name,
						Type:     di.Type,
						Disabled: disabled,
						Dm:       dm,
						State:    state,
						Dash:     ResWidgetData,
						Mapping:  fml,
					}
					ResDeviceData = append(ResDeviceData, rdi)
				}
			}
			//第二层
			l2, c2 := AssetService.GetAssetsByParentID(s.ID)
			var ResAssetData2 []AssetData2
			if c2 != 0 {
				for _, s := range l2 {
					d, cd := DeviceService.GetDevicesByAssetID(s.ID)
					var ResDeviceData2 []DeviceData
					if cd != 0 {
						for _, di := range d {
							if di.Type == "-1" {
								disabled = false
								dm = ""
								state = ""
							} else {
								disabled = true
								dm = "代码"
								state = "正常"
							}
							dd := AssetService.Widget(di.Type)
							var ResWidgetData2 []services.Widget
							if len(dd) > 0 {
								for _, wv := range dd {
									ResWidgetData2 = append(ResWidgetData2, wv)
								}
							}
							fml, _ := FieldMappingService.GetByDeviceid(di.ID)
							rdi := DeviceData{
								ID:       di.ID,
								Name:     di.Name,
								Type:     di.Type,
								Disabled: disabled,
								Dm:       dm,
								State:    state,
								Dash:     ResWidgetData2,
								Mapping:  fml,
							}
							ResDeviceData2 = append(ResDeviceData2, rdi)
						}
					}
					// 第三层
					l3, c3 := AssetService.GetAssetsByParentID(s.ID)
					var ResAssetData3 []AssetData3
					if c3 != 0 {
						for _, s := range l3 {
							d, cd := DeviceService.GetDevicesByAssetID(s.ID)
							var ResDeviceData3 []DeviceData
							if cd != 0 {
								for _, di := range d {
									if di.Type == "-1" {
										disabled = false
										dm = ""
										state = ""
									} else {
										disabled = true
										dm = "代码"
										state = "正常"
									}
									dd := AssetService.Widget(di.Type)
									var ResWidgetData3 []services.Widget
									if len(dd) > 0 {
										for _, wv := range dd {
											ResWidgetData3 = append(ResWidgetData3, wv)
										}
									}
									fml, _ := FieldMappingService.GetByDeviceid(di.ID)
									rdi := DeviceData{
										ID:       di.ID,
										Name:     di.Name,
										Type:     di.Type,
										Disabled: disabled,
										Dm:       dm,
										State:    state,
										Dash:     ResWidgetData3,
										Mapping:  fml,
									}
									ResDeviceData3 = append(ResDeviceData3, rdi)
								}
							}
							rd := AssetData3{
								ID:         s.ID,
								Name:       s.Name,
								CustomerID: s.CustomerID,
								BusinessID: s.BusinessID,
								Device:     ResDeviceData3,
							}
							ResAssetData3 = append(ResAssetData3, rd)
						}
					}
					rd := AssetData2{
						ID:         s.ID,
						Name:       s.Name,
						CustomerID: s.CustomerID,
						BusinessID: s.BusinessID,
						Device:     ResDeviceData2,
						There:      ResAssetData3,
					}
					ResAssetData2 = append(ResAssetData2, rd)
				}
			}
			rd := AssetData{
				ID:         s.ID,
				Name:       s.Name,
				CustomerID: s.CustomerID,
				BusinessID: s.BusinessID,
				Device:     ResDeviceData,
				Two:        ResAssetData2,
			}
			ResAssetData = append(ResAssetData, rd)
		}
		response.SuccessWithDetailed(200, "success", ResAssetData, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithDetailed(200, "success", "", map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
