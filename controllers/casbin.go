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
)

type CasbinController struct {
	beego.Controller
}

// 获取操作日志
func (this *CasbinController) Index() {
	var OperationLogService services.OperationLogService
	w, _ := OperationLogService.List(0, 100)
	response.SuccessWithDetailed(200, "success", w, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 分页获取告警日志
func (this *CasbinController) List() {
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
	var OperationLogService services.OperationLogService
	o, c := OperationLogService.Paginate(operationLogListValidateValidate.Page, operationLogListValidateValidate.Limit, operationLogListValidateValidate.Ip, operationLogListValidateValidate.Path)
	d := PaginateOperationlog{
		CurrentPage: operationLogListValidateValidate.Page,
		Data:        o,
		Total:       c,
		PerPage:     operationLogListValidateValidate.Limit,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
