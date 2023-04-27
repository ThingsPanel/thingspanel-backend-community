package controllers

import (
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"time"

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

	dataTranspondId := utils.GetUuid()
	dataTranspond := models.TpDataTranspon{
		Id:         dataTranspondId,
		Name:       reqData.Name,
		Desc:       reqData.Desc,
		Status:     0, // 默认关闭
		TenantId:   tenantId,
		Script:     reqData.Script,
		CreateTime: time.Now().Unix(),
	}

	var dataTranspondDetail []models.TpDataTransponDetail
	var dataTranspondTarget []models.TpDataTransponTarget

	// 组装 dataTranspondDetail
	for _, v := range reqData.DeviceInfo {
		tmp := models.TpDataTransponDetail{
			Id:              utils.GetUuid(),
			DataTranspondId: dataTranspondId,
			DeviceId:        v.DeviceId,
			MessageType:     v.MessageType,
		}
		dataTranspondDetail = append(dataTranspondDetail, tmp)
	}

	// 组装 dataTranspondTarget 发送到URL
	if len(reqData.TargetInfo.URL) != 0 {
		tmp := models.TpDataTransponTarget{
			Id:              utils.GetUuid(),
			DataTranspondId: dataTranspondId,
			DataType:        models.DataTypeURL,
			Target:          reqData.TargetInfo.URL,
		}
		dataTranspondTarget = append(dataTranspondTarget, tmp)
	}

	// 组装 dataTranspondTarget 发送到MQTT
	if len(reqData.TargetInfo.MQTT.Host) != 0 {
		tmp := models.TpDataTransponTarget{
			Id:              utils.GetUuid(),
			DataTranspondId: dataTranspondId,
			DataType:        models.DataTypeMQTT,
			Target:          reqData.TargetInfo.URL,
		}
		dataTranspondTarget = append(dataTranspondTarget, tmp)
	}

	var create services.TpDataTranspondService
	create.AddTpDataTranspond(dataTranspond, dataTranspondDetail, dataTranspondTarget)

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
