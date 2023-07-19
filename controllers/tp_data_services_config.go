package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpDataServicesConfigController struct {
	beego.Controller
}

// 列表
func (c *TpDataServicesConfigController) List() {
	reqData := valid.TpDataServicesConfigPaginationValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	var tpdataservicesconfig services.TpDataServicesConfig
	isSuccess, d, t := tpdataservicesconfig.GetTpDataServicesConfigList(reqData)
	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspTpDataServicesConfigPaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 新增
func (c *TpDataServicesConfigController) Add() {
	reqData := valid.AddTpDataServicesConfigValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	//获取appkey
	appkey, ok := c.Ctx.Input.GetData("X-OpenAPI-AppKey").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	//获取signature
	secretKey, ok := c.Ctx.Input.GetData("secretKey").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var tpdataservicesconfig services.TpDataServicesConfig
	d, rsp_err := tpdataservicesconfig.AddTpDataServicesConfig(reqData, appkey, secretKey)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
	}
}

// 编辑
func (c *TpDataServicesConfigController) Edit() {
	reqData := valid.EditTpDataServicesConfigValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	var tpdataservicesconfig services.TpDataServicesConfig
	err := tpdataservicesconfig.EditTpDataServicesConfig(reqData)
	if err == nil {
		d := tpdataservicesconfig.GetTpDataServicesConfigDetail(reqData.Id)
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
	}
}

// 删除
func (c *TpDataServicesConfigController) Del() {
	reqData := valid.TpDataServicesConfigIdValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	if reqData.Id == "" {
		utils.SuccessWithMessage(1000, "id不能为空", (*context2.Context)(c.Ctx))
		return
	}

	var tpdataservicesconfig services.TpDataServicesConfig
	tpdataservicesconfigmodel := models.TpDataServicesConfig{
		Id: reqData.Id,
	}
	req_err := tpdataservicesconfig.DeleteTpDataServicesConfig(tpdataservicesconfigmodel)
	if req_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, "编辑失败", (*context2.Context)(c.Ctx))
	}

}

//调试
func (c *TpDataServicesConfigController) Quize() {
	reqData := valid.TpDataServicesConfigQuizeValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	if reqData.DataSql == "" {
		utils.SuccessWithMessage(1000, "sql不能为空", (*context2.Context)(c.Ctx))
		return
	}

	var tpdataservicesconfig services.TpDataServicesConfig
	d, rsp_err := tpdataservicesconfig.QuizeTpDataServicesConfig(reqData.DataSql)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
	}

}
