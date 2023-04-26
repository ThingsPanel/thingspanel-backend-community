package controllers

import (
	response "ThingsPanel-Go/utils"
	"fmt"

	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpDataTransponController struct {
	beego.Controller
}

func (c *TpDataTransponController) List() {
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	fmt.Println(tenantId)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	response.Success(200, (*context2.Context)(c.Ctx))
}

func (c *TpDataTransponController) Add() {
	// 获取用户租户ID
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	fmt.Println(tenantId)

	// uuid := utils.GetUuid()
	// test := models.TpDataTranspon{
	// 	Id: uuid,
	// }

	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	response.Success(200, (*context2.Context)(c.Ctx))
}

func (c *TpDataTransponController) Detail() {
	response.Success(200, (*context2.Context)(c.Ctx))
}

func (c *TpDataTransponController) Edit() {
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	fmt.Println(tenantId)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	response.Success(200, (*context2.Context)(c.Ctx))
}

func (c *TpDataTransponController) Delete() {
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	fmt.Println(tenantId)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	response.Success(200, (*context2.Context)(c.Ctx))
}

func (c *TpDataTransponController) Switch() {
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	fmt.Println(tenantId)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	response.Success(200, (*context2.Context)(c.Ctx))
}
