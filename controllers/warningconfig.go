package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"github.com/bitly/go-simplejson"
)

type WarningconfigController struct {
	beego.Controller
}

type Warningconfigfield struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type Warningconfigindex struct {
	ID           string                   `json:"id"`
	Wid          string                   `json:"wid"`
	Name         string                   `json:"name"`
	Describe     string                   `json:"describe"`
	Config       []map[string]interface{} `json:"config"`
	Message      string                   `json:"message"`
	Bid          string                   `json:"bid"`
	Sensor       string                   `json:"sensor"`
	CustomerID   string                   `json:"customer_id"`
	OtherMessage string                   `json:"other_message"`
}

// 警告列表
func (this *WarningconfigController) Index() {
	warningConfigIndexValidate := valid.WarningConfigIndex{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &warningConfigIndexValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(warningConfigIndexValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(warningConfigIndexValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var WarningConfigService services.WarningConfigService
	u, c := WarningConfigService.Paginate(warningConfigIndexValidate.Wid, warningConfigIndexValidate.Page-1, warningConfigIndexValidate.Limit)
	var rd []Warningconfigindex
	if c > 0 {
		for _, wv := range u {
			var d []map[string]interface{}
			r, e := simplejson.NewJson([]byte(wv.Config))
			if e != nil {
				fmt.Println("解析出错", e)
			} else {
				rows, _ := r.Array()
				for _, row := range rows {
					d = append(d, row.(map[string]interface{}))
				}
				i := Warningconfigindex{
					ID:           wv.ID,
					Wid:          wv.Wid,
					Name:         wv.Name,
					Describe:     wv.Describe,
					Config:       d,
					Message:      wv.Message,
					Bid:          wv.Bid,
					Sensor:       wv.Sensor,
					CustomerID:   wv.CustomerID,
					OtherMessage: wv.OtherMessage,
				}
				rd = append(rd, i)
			}
		}
	}
	if len(rd) == 0 {
		rd = []Warningconfigindex{}
	}
	response.SuccessWithDetailed(200, "success", rd, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 添加警告
func (this *WarningconfigController) Add() {
	warningConfigAddValidate := valid.WarningConfigAdd{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &warningConfigAddValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(warningConfigAddValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(warningConfigAddValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var WarningConfigService services.WarningConfigService
	_, c := WarningConfigService.GetWarningConfigByWidAndBid(warningConfigAddValidate.Wid, warningConfigAddValidate.Bid)
	if c == 0 {
		f, _ := WarningConfigService.Add(
			warningConfigAddValidate.Wid,
			warningConfigAddValidate.Name,
			warningConfigAddValidate.Describe,
			warningConfigAddValidate.Config,
			warningConfigAddValidate.Message,
			warningConfigAddValidate.Bid,
			warningConfigAddValidate.Sensor,
			warningConfigAddValidate.CustomerID,
			warningConfigAddValidate.OtherMessage,
		)
		if f {
			response.SuccessWithMessage(200, "success", (*context2.Context)(this.Ctx))
			return
		} else {
			response.SuccessWithMessage(400, "插入失败", (*context2.Context)(this.Ctx))
			return
		}
	} else {
		response.SuccessWithMessage(400, "此设备已有预警策略", (*context2.Context)(this.Ctx))
		return
	}
}

// 编辑警告
func (this *WarningconfigController) Edit() {
	warningConfigEditValidate := valid.WarningConfigEdit{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &warningConfigEditValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(warningConfigEditValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(warningConfigEditValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var WarningConfigService services.WarningConfigService
	f := WarningConfigService.Edit(
		warningConfigEditValidate.ID,
		warningConfigEditValidate.Wid,
		warningConfigEditValidate.Name,
		warningConfigEditValidate.Describe,
		warningConfigEditValidate.Config,
		warningConfigEditValidate.Message,
		warningConfigEditValidate.Bid,
		warningConfigEditValidate.Sensor,
		warningConfigEditValidate.CustomerID,
		warningConfigEditValidate.OtherMessage,
	)
	if f {
		response.SuccessWithMessage(200, "编辑成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(this.Ctx))
	return
}

// 删除警告
func (this *WarningconfigController) Delete() {
	warningConfigDeleteValidate := valid.WarningConfigDelete{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &warningConfigDeleteValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(warningConfigDeleteValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(warningConfigDeleteValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var WarningConfigService services.WarningConfigService
	f := WarningConfigService.Delete(warningConfigDeleteValidate.ID)
	if f {
		response.SuccessWithMessage(200, "删除成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "删除失败", (*context2.Context)(this.Ctx))
	return
}

// 获取获取具体某一条告警策略
func (this *WarningconfigController) GetOne() {
	warningConfigGetValidate := valid.WarningConfigGet{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &warningConfigGetValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(warningConfigGetValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(warningConfigGetValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(200, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var WarningConfigService services.WarningConfigService
	u, i := WarningConfigService.GetWarningConfigById(warningConfigGetValidate.ID)
	if i != 0 {
		// 获取成功
		response.SuccessWithDetailed(200, "success", u, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	// 获取失败
	response.SuccessWithMessage(400, "error", (*context2.Context)(this.Ctx))
	return
}

// 查询传感器下的字段
func (this *WarningconfigController) Field() {
	warningConfigFieldValidate := valid.WarningConfigField{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &warningConfigFieldValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(warningConfigFieldValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(warningConfigFieldValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(200, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	var AssetService services.AssetService
	var wfs []Warningconfigfield
	d, c := DeviceService.GetDeviceByID(warningConfigFieldValidate.DeviceID)
	if c > 0 {
		wl := AssetService.Widget(d.Type)
		if len(wl) > 0 {
			for _, wv := range wl {
				fl := AssetService.Field(d.Type, wv.Key)
				if len(fl) > 0 {
					for _, fv := range fl {
						if fv.Type == 0 || fv.Type == 1 {
							i := Warningconfigfield{
								Key:  fv.Key,
								Name: fv.Name,
							}
							wfs = append(wfs, i)
						}
					}
				}
			}
		}
	}
	if len(wfs) == 0 {
		wfs = []Warningconfigfield{}
	}
	response.SuccessWithDetailed(200, "success", wfs, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
