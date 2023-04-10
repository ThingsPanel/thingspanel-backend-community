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

type OperationlogController struct {
	beego.Controller
}

type PaginateOperationlog struct {
	CurrentPage int                   `json:"current_page"`
	Data        []models.OperationLog `json:"data"`
	Total       int64                 `json:"total"`
	PerPage     int                   `json:"per_page"`
}

// 获取操作日志
func (this *OperationlogController) Index() {
	var OperationLogService services.OperationLogService
	w, _ := OperationLogService.List(0, 100)
	response.SuccessWithDetailed(200, "success", w, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 分页获取告警日志
func (this *OperationlogController) List() {
	operationLogListValidateValidate := valid.OperationLogListValidate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &operationLogListValidateValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(operationLogListValidateValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(operationLogListValidateValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	tenantId, ok := this.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(this.Ctx))
		return
	}
	var OperationLogService services.OperationLogService
	o, c := OperationLogService.Paginate(operationLogListValidateValidate.Page, operationLogListValidateValidate.Limit, operationLogListValidateValidate.Ip, operationLogListValidateValidate.Path, tenantId)
	d := PaginateOperationlog{
		CurrentPage: operationLogListValidateValidate.Page,
		Data:        o,
		Total:       c,
		PerPage:     operationLogListValidateValidate.Limit,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
