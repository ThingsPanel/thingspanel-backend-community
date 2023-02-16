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

type TpWarningInformationController struct {
	beego.Controller
}

// 列表
func (TpWarningInformationController *TpWarningInformationController) List() {
	PaginationValidate := valid.TpWarningInformationPaginationValidate{}
	err := json.Unmarshal(TpWarningInformationController.Ctx.Input.RequestBody, &PaginationValidate)
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
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpWarningInformationController.Ctx))
			break
		}
		return
	}
	var TpWarningInformationService services.TpWarningInformationService
	d, t, err := TpWarningInformationService.GetTpWarningInformationList(PaginationValidate)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(TpWarningInformationController.Ctx))
		return
	}
	dd := valid.RspTpWarningInformationPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpWarningInformationController.Ctx))
}

// 编辑
func (TpWarningInformationController *TpWarningInformationController) Edit() {
	EditTpWarningInformationValidate := valid.TpWarningInformationValidate{}
	err := json.Unmarshal(TpWarningInformationController.Ctx.Input.RequestBody, &EditTpWarningInformationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(EditTpWarningInformationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(EditTpWarningInformationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpWarningInformationController.Ctx))
			break
		}
		return
	}
	if EditTpWarningInformationValidate.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(TpWarningInformationController.Ctx))
	}
	var TpWarningInformationService services.TpWarningInformationService
	d, err := TpWarningInformationService.EditTpWarningInformation(EditTpWarningInformationValidate)
	if err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(TpWarningInformationController.Ctx))
	} else {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(TpWarningInformationController.Ctx))
	}
}
