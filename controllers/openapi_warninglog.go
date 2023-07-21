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

type OpenapiWarninglogController struct {
	beego.Controller
	services.OpenApiCommonService
}

func (this *OpenapiWarninglogController) GetDeviceWarningList() {
	deviceWarningLogListValidate := valid.DeviceWarningLogListValidate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &deviceWarningLogListValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	if !this.IsAccessDeviceId(this.Ctx, deviceWarningLogListValidate.DeviceId) {
		response.SuccessWithMessage(401, "无设备访问权限", this.Ctx)
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
	// 获取用户租户id
	tenantId, ok := this.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(this.Ctx))
		return
	}
	var WarningLogService services.WarningLogService
	w := WarningLogService.WarningForWid(deviceWarningLogListValidate.DeviceId, tenantId, deviceWarningLogListValidate.Limit)
	d := ViewWarninglog{
		Data: w,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
}
