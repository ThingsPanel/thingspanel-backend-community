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

type TpGenerateDeviceController struct {
	beego.Controller
}

// 列表
func (TpGenerateDeviceController *TpGenerateDeviceController) ActivateDevice() {
	ActivateDeviceValidate := valid.ActivateDeviceValidate{}
	err := json.Unmarshal(TpGenerateDeviceController.Ctx.Input.RequestBody, &ActivateDeviceValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(ActivateDeviceValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(ActivateDeviceValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(TpGenerateDeviceController.Ctx))
			break
		}
		return
	}
	var TpGenerateDeviceService services.TpGenerateDeviceService
	rsp_err := TpGenerateDeviceService.ActivateDevice(ActivateDeviceValidate.ActivationCode, ActivateDeviceValidate.AccessId, ActivateDeviceValidate.Name)
	if rsp_err != nil {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(TpGenerateDeviceController.Ctx))
		return
	}
	utils.SuccessWithMessage(200, "success", (*context2.Context)(TpGenerateDeviceController.Ctx))
}
