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

	// 没有目标
	if len(reqData.TargetInfo.URL) == 0 && len(reqData.TargetInfo.MQTT.Host) == 0 {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
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

		mqttInfo := make(map[string]interface{})
		mqttInfo["host"] = reqData.TargetInfo.MQTT.Host
		mqttInfo["port"] = reqData.TargetInfo.MQTT.Port
		mqttInfo["username"] = reqData.TargetInfo.MQTT.UserName
		mqttInfo["password"] = reqData.TargetInfo.MQTT.Password
		mqttInfo["client_id"] = reqData.TargetInfo.MQTT.ClientId
		mqttInfo["topic"] = reqData.TargetInfo.MQTT.Topic

		mqttInfoJson, err := json.Marshal(mqttInfo)
		if err != nil {
			response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))

		}

		tmp := models.TpDataTransponTarget{
			Id:              utils.GetUuid(),
			DataTranspondId: dataTranspondId,
			DataType:        models.DataTypeMQTT,
			Target:          string(mqttInfoJson),
		}
		dataTranspondTarget = append(dataTranspondTarget, tmp)
	}

	var create services.TpDataTranspondService

	if ok := create.AddTpDataTranspond(dataTranspond, dataTranspondDetail, dataTranspondTarget); !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
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
