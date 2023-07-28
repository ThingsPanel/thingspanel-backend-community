package controller

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	services2 "ThingsPanel-Go/openapi/service"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type OpenapiWarninglogController struct {
	beego.Controller
}

func (c *OpenapiWarninglogController) GetDeviceWarningList() {
	warningLogListValidate := valid.TpWarningInformationPaginationValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &warningLogListValidate)
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
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var WarningLogService services2.OpenapiWaringService
	d, t, err := WarningLogService.GetTpWarningInformationList(warningLogListValidate, tenantId)
	if err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspTpWarningInformationPaginationValidate{
		CurrentPage: warningLogListValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     warningLogListValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))
}
