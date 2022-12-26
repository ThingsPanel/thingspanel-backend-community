package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type LogoController struct {
	beego.Controller
}

// 列表
func (logoController *LogoController) Index() {
	var LogoService services.LogoService
	d := LogoService.GetLogo()
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(logoController.Ctx))
}

// 编辑
func (logoController *LogoController) Edit() {
	editLogoValidate := valid.LogoValidate{}
	err := json.Unmarshal(logoController.Ctx.Input.RequestBody, &editLogoValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(editLogoValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(editLogoValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(logoController.Ctx))
			break
		}
		return
	}
	var LogoService services.LogoService
	Logo := models.Logo{
		Id:             editLogoValidate.Id,
		SystemName:     editLogoValidate.SystemName,
		Theme:          editLogoValidate.Theme,
		LogoOne:        editLogoValidate.LogoOne,
		LogoTwo:        editLogoValidate.LogoTwo,
		HomeBackground: editLogoValidate.HomeBackground,
		LogoThree:      editLogoValidate.LogoThree,
	}
	d := LogoService.GetLogo()
	var isSucess bool
	if d == (models.Logo{}) {
		var uuid = uuid.GetUuid()
		Logo.Id = uuid
		isSucess, _ = LogoService.Add(Logo)
	} else { //修改
		if editLogoValidate.Id == "" {
			response.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(logoController.Ctx))
		}
		isSucess = LogoService.Edit(Logo)
	}
	if isSucess {
		response.SuccessWithDetailed(200, "success", Logo, map[string]string{}, (*context2.Context)(logoController.Ctx))
	} else {
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(logoController.Ctx))
	}
}
