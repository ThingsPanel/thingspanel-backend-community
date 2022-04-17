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

type OpenController struct {
	beego.Controller
}

func (openController *OpenController) GetData() {
	OpenValidate := valid.OpenValidate{}
	err := json.Unmarshal(openController.Ctx.Input.RequestBody, &OpenValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(OpenValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(OpenValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(openController.Ctx))
			break
		}
		return
	}
	//如果有图片或者文件就保存到本地，并将数据入库
	var OpenService services.OpenService
	isSucess, err := OpenService.SaveData(OpenValidate)
	if isSucess {
		response.SuccessWithMessage(200, "接收成功", (*context2.Context)(openController.Ctx))
	} else {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(openController.Ctx))
	}
}
