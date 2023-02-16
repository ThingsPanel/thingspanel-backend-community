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

type TpScenarioLogController struct {
	beego.Controller
}

// 列表
func (TpScenarioLogController *TpScenarioLogController) List() {
	PaginationValidate := valid.TpScenarioLogPaginationValidate{}
	err := json.Unmarshal(TpScenarioLogController.Ctx.Input.RequestBody, &PaginationValidate)
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
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpScenarioLogController.Ctx))
			break
		}
		return
	}
	var TpScenarioLogService services.TpScenarioLogService
	d, t, err := TpScenarioLogService.GetTpScenarioLogList(PaginationValidate)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(TpScenarioLogController.Ctx))
		return
	}
	dd := valid.RspTpScenarioLogPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(TpScenarioLogController.Ctx))
}
