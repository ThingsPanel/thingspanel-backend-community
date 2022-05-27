package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"github.com/mintance/go-uniqid"
)

type WidgetController struct {
	beego.Controller
}

// 添加组件
func (this *WidgetController) Add() {
	addWidgetValidate := valid.AddWidget{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &addWidgetValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(addWidgetValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(addWidgetValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var WidgetService services.WidgetService
	wf := WidgetService.GetRepeat(addWidgetValidate.DashboardID, addWidgetValidate.AssetID, addWidgetValidate.DeviceID, addWidgetValidate.WidgetIdentifier)
	if wf {
		// 更新
		_, c := WidgetService.GetWidgetDashboardId(addWidgetValidate.DashboardID)
		var slice_id int64
		randId := rand.New(rand.NewSource(time.Now().UnixNano()))
		slice_id = int64(randId.Int())
		var y int64
		if c == 0 {
			y = 0
		} else {
			y = c * 6
		}
		var AssetService services.AssetService
		var title string
		var template string
		exts := strings.Split(addWidgetValidate.WidgetIdentifier, ":")
		extConfig := AssetService.Widget(exts[0])
		for _, ev := range extConfig {
			if ev.Key == exts[1] {
				title = ev.Name
				template = ev.Template
			}
		}
		t := template[0:2]
		if t != "x_" {
			//template = strings.ToLower(exts[0] + "-" + template)
			template = strings.ToLower(template)
		}
		dc := services.DashboardConfig{
			SliceId:   slice_id,
			X:         0,
			Y:         y,
			W:         12,
			H:         6,
			Width:     360,
			Height:    210,
			I:         uniqid.New(uniqid.Params{Prefix: "dc", MoreEntropy: true}),
			ChartType: template,
			Title:     title,
		}
		dcJson, _ := json.Marshal(dc)
		config := string(dcJson)
		rf := WidgetService.ForAddEdit(addWidgetValidate.AssetID, addWidgetValidate.DeviceID, addWidgetValidate.DashboardID, addWidgetValidate.WidgetIdentifier, config)
		if rf {
			response.SuccessWithMessage(200, "插入成功", (*context2.Context)(this.Ctx))
			return
		} else {
			response.SuccessWithMessage(400, "插入失败", (*context2.Context)(this.Ctx))
			return
		}
	} else {
		// 新增
		_, c := WidgetService.GetWidgetDashboardId(addWidgetValidate.DashboardID)
		var slice_id int64
		randId := rand.New(rand.NewSource(time.Now().UnixNano()))
		slice_id = int64(randId.Int())
		var y int64
		if c == 0 {
			y = 0
		} else {
			y = c * 6
		}
		var AssetService services.AssetService
		var title string
		var template string
		exts := strings.Split(addWidgetValidate.WidgetIdentifier, ":")
		extConfig := AssetService.Widget(exts[0])
		for _, ev := range extConfig {
			if ev.Key == exts[1] {
				title = ev.Name
				template = ev.Template
			}
		}
		t := template[0:2]
		if t != "x_" {
			//template = strings.ToLower(exts[0] + "-" + template)
			template = strings.ToLower(template)
		}
		dc := services.DashboardConfig{
			SliceId:   slice_id,
			X:         0,
			Y:         y,
			W:         12,
			H:         6,
			Width:     360,
			Height:    210,
			I:         uniqid.New(uniqid.Params{Prefix: "dc", MoreEntropy: true}),
			ChartType: template,
			Title:     title,
		}
		dcJson, _ := json.Marshal(dc)
		config := string(dcJson)
		rf, _ := WidgetService.Add(addWidgetValidate.DashboardID, addWidgetValidate.AssetID, addWidgetValidate.DeviceID, addWidgetValidate.WidgetIdentifier, config)
		if rf {
			response.SuccessWithMessage(200, "插入成功", (*context2.Context)(this.Ctx))
			return
		} else {
			response.SuccessWithMessage(400, "插入失败", (*context2.Context)(this.Ctx))
			return
		}
	}
}

// 编辑组件
func (this *WidgetController) Edit() {
	editWidgetValidate := valid.EditWidget{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &editWidgetValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(editWidgetValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(editWidgetValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var WidgetService services.WidgetService
	_, c := WidgetService.GetWidgetDashboardId(editWidgetValidate.DashboardID)
	var slice_id int64
	var y int64
	if c == 0 {
		slice_id = 1
		y = 0
	} else {
		slice_id = c + 1
		y = c * 6
	}
	var AssetService services.AssetService
	var title string
	var template string
	exts := strings.Split(editWidgetValidate.WidgetIdentifier, ":")
	extConfig := AssetService.Widget(exts[0])
	for _, ev := range extConfig {
		if ev.Key == exts[1] {
			title = ev.Name
			template = ev.Template
		}
	}
	t := template[0:2]
	if t != "x_" {
		//template = strings.ToLower(exts[0] + "-" + template)
		template = strings.ToLower(template)
	}
	dc := services.DashboardConfig{
		SliceId:   slice_id,
		X:         0,
		Y:         y,
		W:         12,
		H:         6,
		Width:     360,
		Height:    210,
		I:         uniqid.New(uniqid.Params{Prefix: "dc", MoreEntropy: true}),
		ChartType: template,
		Title:     title,
	}
	dcJson, _ := json.Marshal(dc)
	config := string(dcJson)
	f := WidgetService.Edit(editWidgetValidate.ID, editWidgetValidate.DashboardID, editWidgetValidate.AssetID, editWidgetValidate.DeviceID, editWidgetValidate.WidgetIdentifier, config)
	if f {
		response.SuccessWithMessage(200, "编辑成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(this.Ctx))
	return
}

// 删除组件
func (this *WidgetController) Delete() {
	deleteWidgetValidate := valid.DeleteWidget{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &deleteWidgetValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(deleteWidgetValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(deleteWidgetValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var WidgetService services.WidgetService
	f := WidgetService.Delete(deleteWidgetValidate.ID)
	if f {
		// 删除成功
		response.SuccessWithMessage(200, "success", (*context2.Context)(this.Ctx))
		return
	}
	// 删除失败
	response.SuccessWithMessage(400, "error", (*context2.Context)(this.Ctx))
	return
}

// 修改扩展功能
func (widgetController *WidgetController) UpdateExtend() {
	extendWidgetValidate := valid.ExtendWidget{}
	err := json.Unmarshal(widgetController.Ctx.Input.RequestBody, &extendWidgetValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(extendWidgetValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(extendWidgetValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(widgetController.Ctx))
			break
		}
		return
	}
	var WidgetService services.WidgetService
	f := WidgetService.EditExtend(extendWidgetValidate.ID, extendWidgetValidate.Extend)
	if f {
		// 修改成功
		response.SuccessWithMessage(200, "success", (*context2.Context)(widgetController.Ctx))
		return
	}
	// 修改失败
	response.SuccessWithMessage(400, "error", (*context2.Context)(widgetController.Ctx))
}
