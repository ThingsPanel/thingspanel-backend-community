package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpAutomationLogDetailController struct {
	beego.Controller
}

// 列表
func (TpAutomationLogDetailController *TpAutomationLogDetailController) List() {
	PaginationValidate := valid.TpAutomationLogDetailPaginationValidate{}
	err := json.Unmarshal(TpAutomationLogDetailController.Ctx.Input.RequestBody, &PaginationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(PaginationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(PaginationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpAutomationLogDetailController.Ctx))
			break
		}
		return
	}
	var TpAutomationLogDetailService services.TpAutomationLogDetailService
	d, t, err := TpAutomationLogDetailService.GetTpAutomationLogDetailList(PaginationValidate)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(TpAutomationLogDetailController.Ctx))
		return
	}
	dd := valid.RspTpAutomationLogDetailPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpAutomationLogDetailController.Ctx))
}
