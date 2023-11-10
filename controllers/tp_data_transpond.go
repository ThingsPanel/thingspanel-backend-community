package controllers

import (
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"time"

	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpDataTransponController struct {
	beego.Controller
}

type DataTransponList struct {
	CurrentPage int         `json:"current_page"`
	Data        interface{} `json:"data"`
	Total       int64       `json:"total"`
	PerPage     int         `json:"per_page"`
}

type DataTranspondDetail struct {
	Id                string                    `json:"id"`
	Name              string                    `json:"name"`
	Desc              string                    `json:"desc"`
	Script            string                    `json:"script"`
	WarningStrategyId string                    `json:"warning_strategy_id"`
	WarningSwitch     int                       `json:"warning_switch"`
	TargetInfo        DataTranspondTarget       `json:"target_info"`
	DeviceInfo        []DataTranspondDeviceInfo `json:"device_info"`
}

type DataTranspondTarget struct {
	URL  string                     `json:"url"`
	MQTT DataTransponTargetInfoMQTT `json:"mqtt"`
}

type DataTransponTargetInfoMQTT struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	UserName string `json:"username"`
	Password string `json:"password"`
	ClientId string `json:"client_id"`
	Topic    string `json:"topic"`
}

type DataTranspondDeviceInfo struct {
	DeviceId     string `json:"device_id"`
	MessageType  int    `json:"message_type"`
	BusinessId   string `json:"business_id"`
	AssetGroupId string `json:"asset_group_id"`
}

func (c *TpDataTransponController) List() {
	reqData := valid.TpDataTransponListValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	offset := (reqData.CurrentPage - 1) * reqData.PerPage
	var dataTranspondService services.TpDataTranspondService
	data, count := dataTranspondService.GetListByTenantId(offset, reqData.PerPage, tenantId)
	d := DataTransponList{
		CurrentPage: reqData.CurrentPage,
		Total:       count,
		PerPage:     reqData.PerPage,
		Data:        data,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))

}

func (c *TpDataTransponController) Add() {
	// 验证入参
	reqData := valid.TpDataTransponAddValid{}
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

	warningStratedyId := ""
	// 如果有告警策略，优先创建告警策略
	if reqData.WarningSwitch == 1 {
		warningStratedyId = uuid.GetUuid()
		var s services.TpWarningStrategyService
		data := models.TpWarningStrategy{
			Id:                  warningStratedyId,
			WarningStrategyName: reqData.WarningStrategy.WarningStrategyName,
			WarningLevel:        reqData.WarningStrategy.WarningLevel,
			RepeatCount:         reqData.WarningStrategy.RepeatCount,
			TriggerCount:        0,
			InformWay:           reqData.WarningStrategy.InformWay,
			Remark:              "",
			WarningDescription:  reqData.WarningStrategy.WarningDesc,
		}
		_, err := s.AddTpWarningStrategy(data)
		if err != nil {
			utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
			return
		}

	}

	dataTranspondId := utils.GetUuid()
	dataTranspond := models.TpDataTranspon{
		Id:                dataTranspondId,
		Name:              reqData.Name,
		Desc:              reqData.Desc,
		Status:            0, // 默认关闭
		TenantId:          tenantId,
		Script:            reqData.Script,
		CreateTime:        time.Now().Unix(),
		WarningStrategyId: warningStratedyId,
		WarningSwitch:     reqData.WarningSwitch,
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
			BusinessId:      v.BusinessId,
			AssetGroupId:    v.AssetGroupId,
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
	reqData := valid.TpDataTransponDetailValid{}
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

	var find services.TpDataTranspondService
	dataTranspond, e := find.GetDataTranspondByDataTranspondId(reqData.DataTranspondId)
	// 如果数据库查询失败 或 租户ID不符，返回错误
	if !e || tenantId != dataTranspond.TenantId {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	// 查询详情表
	dataTranspondDetail, e := find.GetDataTranspondDetailByDataTranspondId(reqData.DataTranspondId)
	if !e {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	// 查询数据目标表
	dataTranspondTarget, e := find.GetDataTranspondTargetByDataTranspondId(reqData.DataTranspondId)
	if !e {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	var deviceInfo []DataTranspondDeviceInfo
	for _, v := range dataTranspondDetail {
		everyDevice := DataTranspondDeviceInfo{
			DeviceId:     v.DeviceId,
			MessageType:  v.MessageType,
			BusinessId:   v.BusinessId,
			AssetGroupId: v.AssetGroupId,
		}
		deviceInfo = append(deviceInfo, everyDevice)
	}

	var targetInfo DataTranspondTarget
	for _, v := range dataTranspondTarget {
		switch v.DataType {
		case models.DataTypeURL:
			// 如果是URL，填入URL
			targetInfo.URL = v.Target
		case models.DataTypeMQTT:
			// 如果是MQTT，填入MQTT
			var d DataTransponTargetInfoMQTT
			err := json.Unmarshal([]byte(v.Target), &d)
			if err != nil {
				response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
				return
			}
			targetInfo.MQTT = d
		}

	}

	d := DataTranspondDetail{
		Id:                reqData.DataTranspondId,
		Name:              dataTranspond.Name,
		Desc:              dataTranspond.Desc,
		Script:            dataTranspond.Script,
		WarningStrategyId: dataTranspond.WarningStrategyId,
		WarningSwitch:     dataTranspond.WarningSwitch,
		DeviceInfo:        deviceInfo,
		TargetInfo:        targetInfo,
	}

	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

func (c *TpDataTransponController) Delete() {
	reqData := valid.TpDataTransponDetailValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	var operate services.TpDataTranspondService
	dataTranspond, e := operate.GetDataTranspondByDataTranspondId(reqData.DataTranspondId)
	// 如果数据库查询失败 或 租户ID不符，返回错误
	if !e || tenantId != dataTranspond.TenantId {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	// 删除数据
	operate.DeleteCacheByDataTranspondId(reqData.DataTranspondId)
	res := operate.DeletaByDataTranspondId(reqData.DataTranspondId)
	if !res {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	response.Success(200, (*context2.Context)(c.Ctx))
}

func (c *TpDataTransponController) Switch() {
	reqData := valid.TpDataTransponSwitchValid{}
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

	var find services.TpDataTranspondService
	dataTranspond, e := find.GetDataTranspondByDataTranspondId(reqData.DataTranspondId)
	// 如果数据库查询失败 或 租户ID不符，返回错误
	if !e || tenantId != dataTranspond.TenantId {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	// 如果要修改的状态与数据库一致 直接返回成功
	if reqData.Switch == dataTranspond.Status {
		response.Success(200, (*context2.Context)(c.Ctx))
		return
	}

	// 不一致，则修改数据库的状态
	var update services.TpDataTranspondService
	if ok := update.UpdateDataTranspondStatusByDataTranspondId(reqData.DataTranspondId, reqData.Switch); !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	find.DeleteCacheByDataTranspondId(reqData.DataTranspondId)
	response.Success(200, (*context2.Context)(c.Ctx))
}

func (c *TpDataTransponController) Edit() {
	reqData := valid.TpDataTransponEditValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var operate services.TpDataTranspondService
	dataTranspond, e := operate.GetDataTranspondByDataTranspondId(reqData.Id)
	// 如果数据库查询失败 或 租户ID不符，返回错误
	if !e || tenantId != dataTranspond.TenantId {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	operate.DeleteCacheByDataTranspondId(reqData.Id)

	// 以前有，先删除
	if dataTranspond.WarningStrategyId != "" {
		var s services.TpWarningStrategyService
		delete := models.TpWarningStrategy{
			Id: dataTranspond.WarningStrategyId,
		}
		err := s.DeleteTpWarningStrategy(delete)
		if err != nil {
			utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
			return
		}
	}

	// 创建
	warningStratedyId := ""
	// 如果有告警策略，优先创建告警策略
	if reqData.WarningSwitch == 1 {
		warningStratedyId = uuid.GetUuid()
		var s services.TpWarningStrategyService
		data := models.TpWarningStrategy{
			Id:                  warningStratedyId,
			WarningStrategyName: reqData.WarningStrategy.WarningStrategyName,
			WarningLevel:        reqData.WarningStrategy.WarningLevel,
			RepeatCount:         reqData.WarningStrategy.RepeatCount,
			TriggerCount:        0,
			InformWay:           reqData.WarningStrategy.InformWay,
			Remark:              "",
			WarningDescription:  reqData.WarningStrategy.WarningDesc,
		}
		_, err := s.AddTpWarningStrategy(data)
		if err != nil {
			utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
			return
		}

	}

	updateData := models.TpDataTranspon{
		Id:                reqData.Id,
		Name:              reqData.Name,
		Desc:              reqData.Desc,
		Status:            0,
		TenantId:          tenantId,
		Script:            reqData.Script,
		CreateTime:        time.Now().Unix(),
		WarningStrategyId: warningStratedyId,
		WarningSwitch:     reqData.WarningSwitch,
	}

	// 更新Transpond表，
	if ok := operate.UpdateDataTranspond(updateData); !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	// 删除Detail,Target表，
	if ok := operate.DeletaDeviceTargetByDataTranspondId(reqData.Id); !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	var dataTranspondDetail []models.TpDataTransponDetail
	var dataTranspondTarget []models.TpDataTransponTarget

	// 插入Detail,Target表
	// 组装 dataTranspondDetail
	for _, v := range reqData.DeviceInfo {
		tmp := models.TpDataTransponDetail{
			Id:              utils.GetUuid(),
			DataTranspondId: reqData.Id,
			DeviceId:        v.DeviceId,
			MessageType:     v.MessageType,
			BusinessId:      v.BusinessId,
			AssetGroupId:    v.AssetGroupId,
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
			DataTranspondId: reqData.Id,
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
			return
		}

		tmp := models.TpDataTransponTarget{
			Id:              utils.GetUuid(),
			DataTranspondId: reqData.Id,
			DataType:        models.DataTypeMQTT,
			Target:          string(mqttInfoJson),
		}
		dataTranspondTarget = append(dataTranspondTarget, tmp)
	}

	if ok := operate.AddTpDataTranspondForEdit(dataTranspondDetail, dataTranspondTarget); !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	response.Success(200, (*context2.Context)(c.Ctx))
}
