package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
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

type ConsoleController struct {
	beego.Controller
}

func (c *ConsoleController) Add() {
	input := valid.AddConsole{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(input)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(input, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}

	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	//获取用户id
	userID, ok := c.Ctx.Input.GetData("user_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	var ConsoleService services.ConsoleService
	err = ConsoleService.AddConsole(input.Name, userID, input.Data, input.Config, input.Template, input.Code, tenantId)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	utils.Success(200, c.Ctx)
}

func (c *ConsoleController) Edit() {
	input := valid.EditConsole{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(input)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(input, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}

	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	var ConsoleService services.ConsoleService

	err = ConsoleService.EditConsole(input.ID, input.Name, input.Data, input.Config, input.Template, input.Code, tenantId)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	utils.Success(200, c.Ctx)
}

func (c *ConsoleController) Delete() {
	input := valid.DetailAndDetailConsole{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(input)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(input, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}

	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	var ConsoleService services.ConsoleService
	err = ConsoleService.DeleteConsoleById(input.ID, tenantId)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	utils.Success(200, c.Ctx)
}

func (c *ConsoleController) List() {
	input := valid.ListConsole{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(input)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(input, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}

	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	offset := (input.CurrentPage - 1) * input.PerPage

	var ConsoleService services.ConsoleService
	err, data, count := ConsoleService.GetConsoleList(input.Name, offset, input.PerPage, tenantId)

	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	d := make(map[string]interface{})
	d["current_page"] = input.CurrentPage
	d["total"] = count
	d["per_page"] = input.PerPage
	d["data"] = data

	utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

func (c *ConsoleController) Detail() {
	input := valid.DetailAndDetailConsole{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(input)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(input, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}

	var ConsoleService services.ConsoleService
	data, err := ConsoleService.GetConsoleDetail(input.ID)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	utils.SuccessWithDetailed(200, "success", data, map[string]string{}, (*context2.Context)(c.Ctx))
}
