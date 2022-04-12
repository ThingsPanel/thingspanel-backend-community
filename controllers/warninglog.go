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

type WarninglogController struct {
	beego.Controller
}

type PaginateWarninglog struct {
	CurrentPage int                 `json:"current_page"`
	Data        []models.WarningLog `json:"data"`
	Total       int64               `json:"total"`
	PerPage     int                 `json:"per_page"`
}

// 获取告警日志
func (this *WarninglogController) Index() {
	var WarningLogService services.WarningLogService
	w, _ := WarningLogService.Paginate("", 0, 10, "", "")
	response.SuccessWithDetailed(200, "success", w, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 分页获取告警日志
func (this *WarninglogController) List() {
	warningLogListValidate := valid.WarningLogListValidate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &warningLogListValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(warningLogListValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(warningLogListValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var WarningLogService services.WarningLogService
	w, c := WarningLogService.Paginate("", warningLogListValidate.Page, warningLogListValidate.Limit, warningLogListValidate.StartDate, warningLogListValidate.EndDate)
	d := PaginateWarninglog{
		CurrentPage: warningLogListValidate.Page,
		Data:        w,
		Total:       c,
		PerPage:     warningLogListValidate.Limit,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

type ViewWarninglog struct {
	Data []models.WarningLogView `json:"data"`
}

func (this *WarninglogController) GetDeviceWarningList() {
	deviceWarningLogListValidate := valid.DeviceWarningLogListValidate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &deviceWarningLogListValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(deviceWarningLogListValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(deviceWarningLogListValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var WarningLogService services.WarningLogService
	w := WarningLogService.WarningForWid(deviceWarningLogListValidate.Wid, deviceWarningLogListValidate.Limit)
	d := ViewWarninglog{
		Data: w,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
}
