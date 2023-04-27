package controllers

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
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
	// 验证入参
	reqData := valid.TpDataTransponValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	// 根据 Authorization 获取租户ID
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	uuid := utils.GetUuid()
	DataTranspond := models.TpDataTranspon{
		Id:         uuid,
		Name:       reqData.Name,
		Desc:       reqData.Desc,
		Status:     0, // 默认未启动
		TenantId:   tenantId,
		Script:     reqData.Script,
		CreateTime: 11,
	}

	result := psql.Mydb.Create(&DataTranspond)
	fmt.Println(result)
	// {
	// 	"name": "速要识自",
	// 	"desc": "minim cupidatat et",
	// 	"script": "eiusmod esse ullamco enim consequat",
	// 	"tenant_id": "15",
	// 	"target_info": {
	// 		"mqtt": {
	// 			"host": "Lorem consectetur laborum est",
	// 			"topic": "http://dummyimage.com/400x400",
	// 			"password": "sit in",
	// 			"username": "傅霞",
	// 			"client_id": "61",
	// 			"port": "dolor nulla et occaecat in"
	// 		},
	// 		"url": "http://bidiaajwe.tk/cerlvpop"
	// 	},
	// 	"device_info": [
	// 		{
	// 			"device_id": "26",
	// 			"message_type": 80
	// 		}
	// 	]
	// }

	// uuid := utils.GetUuid()
	// test := models.TpDataTranspon{
	// 	Id: uuid,
	// }

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
