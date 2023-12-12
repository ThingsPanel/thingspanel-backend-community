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

type DashBoardController struct {
	beego.Controller
}

type PaginateDashBoard struct {
	CurrentPage int                `json:"current_page"`
	Data        []models.DashBoard `json:"data"`
	Total       int64              `json:"total"`
	PerPage     int                `json:"per_page"`
}

type PropertyData struct {
	ID             string          `json:"id"`
	AdditionalInfo string          `json:"additional_info"`
	CustomerID     string          `json:"customer_id"`
	Name           string          `json:"name"`
	Label          string          `json:"label"`
	SearchText     string          `json:"search_text"`
	Type           string          `json:"type"`
	ParentID       string          `json:"parent_id"`
	Tier           int64           `json:"tier"`
	BusinessID     string          `json:"business_id"`
	Two            []PropertyData2 `json:"two"`
}

type PropertyData2 struct {
	ID             string          `json:"id"`
	AdditionalInfo string          `json:"additional_info"`
	CustomerID     string          `json:"customer_id"`
	Name           string          `json:"name"`
	Label          string          `json:"label"`
	SearchText     string          `json:"search_text"`
	Type           string          `json:"type"`
	ParentID       string          `json:"parent_id"`
	Tier           int64           `json:"tier"`
	BusinessID     string          `json:"business_id"`
	There          []PropertyData3 `json:"there"`
}

type InserttimeData struct {
	StartTime    string `json:"start_time"`
	EndTime      string `json:"end_time"`
	Theme        int64  `json:"theme"`
	IntervalTime int64  `json:"interval_time"`
	BgTheme      int64  `json:"bg_theme"`
}

type PropertyData3 struct {
	ID             string `json:"id"`
	AdditionalInfo string `json:"additional_info"`
	CustomerID     string `json:"customer_id"`
	Name           string `json:"name"`
	Label          string `json:"label"`
	SearchText     string `json:"search_text"`
	Type           string `json:"type"`
	ParentID       string `json:"parent_id"`
	Tier           int64  `json:"tier"`
	BusinessID     string `json:"business_id"`
}

type GettimeData struct {
	ID                string         `json:"id"`
	Configuration     string         `json:"configuration"`
	AssignedCustomers string         `json:"assigned_customers"`
	SearchText        string         `json:"search_text"`
	Title             string         `json:"title"`
	BusinessID        string         `json:"business_id"`
	Config            InserttimeData `json:"config"`
}

type RealtimeData struct {
	EndTime   string `json:"end_time"`
	StartTime string `json:"start_time"`
}

type DashBoardData struct {
	ID        string                           `json:"id"`
	SliceId   int64                            `json:"slice_id"`
	X         int64                            `json:"x"`
	Y         int64                            `json:"y"`
	W         int64                            `json:"w"`
	H         int64                            `json:"h"`
	Width     int64                            `json:"width"`
	Height    int64                            `json:"height"`
	I         string                           `json:"i"`
	ChartType string                           `json:"chart_type"`
	Title     string                           `json:"title"`
	Fields    []map[string]DashBoardFieldsData `json:"fields"`
}

type DashBoardConfig struct {
	SliceId   int64  `json:"slice_id"`
	X         int64  `json:"x"`
	Y         int64  `json:"y"`
	W         int64  `json:"w"`
	H         int64  `json:"h"`
	Width     int64  `json:"width"`
	Height    int64  `json:"height"`
	I         string `json:"i"`
	ChartType string `json:"chart_type"`
	Title     string `json:"title"`
}

type DashBoardFieldsData struct {
	Name   string `json:"name"`
	Type   int64  `json:"type"`
	Symbol string `json:"symbol"`
}

type WidgetIcon struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Thumbnail string `json:"thumbnail"`
}

// 视图列表
// func (this *DashBoardController) Index() {
// 	paginateDashBoardValidate := valid.PaginateDashBoard{}
// 	err := json.Unmarshal(this.Ctx.Input.RequestBody, &paginateDashBoardValidate)
// 	if err != nil {
// 		fmt.Println("参数解析失败", err.Error())
// 	}
// 	v := validation.Validation{}
// 	status, _ := v.Valid(paginateDashBoardValidate)
// 	if !status {
// 		for _, err := range v.Errors {
// 			// 获取字段别称
// 			alias := gvalid.GetAlias(paginateDashBoardValidate, err.Field)
// 			message := strings.Replace(err.Message, err.Field, alias, 1)
// 			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
// 			break
// 		}
// 		return
// 	}
// 	var DashBoardService services.DashBoardService
// 	offset := (paginateDashBoardValidate.Page - 1) * paginateDashBoardValidate.Limit
// 	u, c := DashBoardService.Paginate(paginateDashBoardValidate.Title, offset, paginateDashBoardValidate.Limit)
// 	d := PaginateDashBoard{
// 		CurrentPage: paginateDashBoardValidate.Page,
// 		Data:        u,
// 		Total:       c,
// 		PerPage:     paginateDashBoardValidate.Limit,
// 	}
// 	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
// 	return
// }

// 添加视图
func (this *DashBoardController) Add() {
	addDashBoardValidate := valid.AddDashBoard{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &addDashBoardValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(addDashBoardValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(addDashBoardValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var DashBoardService services.DashBoardService
	f, _ := DashBoardService.Add(addDashBoardValidate.BusinessId, addDashBoardValidate.Title)
	if f {
		response.SuccessWithMessage(200, "新增成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "新增失败", (*context2.Context)(this.Ctx))
	return
}

// 编辑图表
func (this *DashBoardController) Edit() {
	editDashBoardValidate := valid.EditDashBoard{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &editDashBoardValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(editDashBoardValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(editDashBoardValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var DashBoardService services.DashBoardService
	f := DashBoardService.Edit(editDashBoardValidate.ID, editDashBoardValidate.BusinessID, editDashBoardValidate.Title)
	if f {
		response.SuccessWithMessage(200, "编辑成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(this.Ctx))
	return
}

// 删除图表
func (this *DashBoardController) Delete() {
	deleteDashBoardValidate := valid.DeleteDashBoard{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &deleteDashBoardValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(deleteDashBoardValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(deleteDashBoardValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var DashBoardService services.DashBoardService
	f := DashBoardService.Delete(deleteDashBoardValidate.ID)
	if f {
		response.SuccessWithMessage(200, "删除成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "删除失败", (*context2.Context)(this.Ctx))
	return
}

func (this *DashBoardController) Device() {
	deviceDashBoardValidate := valid.DeviceDashBoard{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &deviceDashBoardValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(deviceDashBoardValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(deviceDashBoardValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	d, _ := DeviceService.GetDevicesByAssetID(deviceDashBoardValidate.AssetID)
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
