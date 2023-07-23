package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	sendmqtt "ThingsPanel-Go/modules/dataService/mqtt/sendMqtt"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/beego/beego/v2/core/logs"
	"github.com/bitly/go-simplejson"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"time"
)

type OpenapiDeviceService struct {
	OpenApiCommonService
}

// 获取设备列表设备在线离线状态
func (*OpenapiDeviceService) GetDeviceOnlineStatus(deviceIdList valid.DeviceIdListValidate) (map[string]interface{}, error) {
	var deviceOnlineStatus = make(map[string]interface{})
	for _, deviceId := range deviceIdList.DeviceIdList {
		var device models.Device
		//根据阈值判断设备是否在线
		result := psql.Mydb.Where("id = ?", deviceId).First(&device)
		if result.Error != nil {
			logs.Error(result.Error)
			if result.Error == gorm.ErrRecordNotFound {
				deviceOnlineStatus[deviceId] = "0"
				continue
			}
		}
		if device.Protocol == "mqtt" || device.Protocol == "MQTT" {

			if device.AdditionalInfo != "" {
				aJson, err := simplejson.NewJson([]byte(device.AdditionalInfo))
				if err == nil {
					thresholdTime, err := aJson.Get("runningInfo").Get("thresholdTime").Int64()

					if err == nil && thresholdTime != 0 {
						//获取最新的数据时间
						var latest_ts int64
						result = psql.Mydb.Model(&models.TSKVLatest{}).Select("max(ts) as ts").Where("entity_id = ? ", deviceId).Group("entity_type").First(&latest_ts)
						if result.Error != nil {
							logs.Error(result.Error)
						}
						if latest_ts != 0 {
							if time.Now().UnixMicro()-latest_ts >= int64(thresholdTime*1e6) {
								deviceOnlineStatus[deviceId] = "0"
							} else {
								deviceOnlineStatus[deviceId] = "1"
							}
							continue
						}
					}
				}
			}

		}

		//原流程
		var tskvLatest models.TSKVLatest
		result = psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_id = ? and key = 'SYS_ONLINE'", deviceId).First(&tskvLatest)
		logs.Info("------------------------------------------------ceshi")
		if result.Error != nil {
			logs.Error(result.Error)
			deviceOnlineStatus[deviceId] = "0"
		} else {
			deviceOnlineStatus[deviceId] = tskvLatest.StrV
		}
	}
	return deviceOnlineStatus, nil
}

func (*OpenapiDeviceService) GetDeviceEvnetHistoryListByDeviceId(
	offset int, pageSize int, deviceId string) ([]models.DeviceEvnetHistory, int64) {

	var evnetHistroy []models.DeviceEvnetHistory
	var count int64

	tx := psql.Mydb.Model(&models.DeviceEvnetHistory{})
	tx.Where("device_id = ?", deviceId)

	err := tx.Count(&count).Error
	if err != nil {
		logs.Error(err.Error())
		return evnetHistroy, count
	}

	err = tx.Order("report_time desc").Limit(pageSize).Offset(offset).Find(&evnetHistroy).Error
	if err != nil {
		logs.Error(err.Error())
		return evnetHistroy, count
	}
	return evnetHistroy, count
}

func (*OpenapiDeviceService) GetDeviceCommandHistoryListByDeviceId(
	offset int, pageSize int, deviceId string) ([]models.DeviceCommandHistory, int64) {

	var commandHistroy []models.DeviceCommandHistory
	var count int64

	tx := psql.Mydb.Model(&models.DeviceCommandHistory{})
	tx.Where("device_id = ?", deviceId)

	err := tx.Count(&count).Error
	if err != nil {
		logs.Error(err.Error())
		return commandHistroy, count
	}

	err = tx.Order("send_time desc").Limit(pageSize).Offset(offset).Find(&commandHistroy).Error
	if err != nil {
		logs.Error(err.Error())
		return commandHistroy, count
	}
	return commandHistroy, count
}

// Token 获取设备token
func (*OpenapiDeviceService) Token(id string) (*models.Device, int64) {
	var device models.Device
	result := psql.Mydb.Where("id = ?", id).First(&device)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return nil, 0
	}
	return &device, result.RowsAffected
}

func (*OpenapiDeviceService) SendCommandToDevice(
	device *models.Device,
	commandIdentifier string,
	commandData []byte,
	commandName string,
	commandDesc string,
) error {

	// 格式化内容：
	var sendStruct struct {
		Method string      `json:"method"`
		Params interface{} `json:"params"`
	}

	commandDataMap := make(map[string]interface{})
	err := json.Unmarshal(commandData, &commandDataMap)
	if err != nil {
		return err
	}
	sendStruct.Method = commandIdentifier
	sendStruct.Params = commandDataMap
	msg, err := json.Marshal(sendStruct)
	if err != nil {
		return err
	}

	fmt.Println("d", sendStruct)

	topic := viper.GetString("mqtt.topicToCommand") + "/"
	sendRes := 2
	switch device.DeviceType {

	case models.DeviceTypeDirect, models.DeviceTypeGatway:
		// 直连设备，网关，直接发
		topic += device.Token
		fmt.Println("topic-2:", topic)

		// 协议设备topic
		if device.Protocol != "mqtt" && device.Protocol != "MQTT" {
			var tpProtocolPluginService services.TpProtocolPluginService
			pp := tpProtocolPluginService.GetByProtocolType(device.Protocol, device.DeviceType)
			topic = pp.SubTopicPrefix + "command/" + device.Token
		}
		// 通过脚本
		msg, err := scriptDealB(device.ScriptId, msg, topic)
		if err != nil {
			return err
		}

		if sendmqtt.SendMQTT(msg, topic, 1) == nil {
			sendRes = 1
		}

		saveCommandSendHistory(
			device.ID,
			commandIdentifier,
			commandName,
			commandDesc,
			string(msg),
			sendRes)

	case models.DeviceTypeSubGatway:
		// 子网关，给网关发
		if len(device.ParentId) != 0 {
			var gatewayDevice *models.Device
			result := psql.Mydb.Where("id = ?", device.ParentId).First(&gatewayDevice) // 检测网关token是否存在
			if result.Error != nil {
				return result.Error
			}
			topic += gatewayDevice.Token

			// 协议设备topic
			if gatewayDevice.Protocol != "mqtt" && gatewayDevice.Protocol != "MQTT" {
				var tpProtocolPluginService services.TpProtocolPluginService
				pp := tpProtocolPluginService.GetByProtocolType(gatewayDevice.Protocol, gatewayDevice.DeviceType)
				topic = pp.SubTopicPrefix + "command/" + gatewayDevice.Token
			}

			msg, err := scriptDealB(gatewayDevice.ScriptId, msg, topic)
			if err != nil {
				return err
			}
			// 通过脚本
			if sendmqtt.SendMQTT(msg, topic, 1) == nil {
				sendRes = 1
			}

			saveCommandSendHistory(
				gatewayDevice.ID,
				commandIdentifier,
				commandName,
				commandDesc,
				string(msg),
				sendRes)
		}

	default:
		break
	}
	return nil
}

// 记录发送日志
func saveCommandSendHistory(
	deviceId, identify, name, desc, data string,
	sendStatus int,
) {
	m := models.DeviceCommandHistory{
		ID:              utils.GetUuid(),
		DeviceId:        deviceId,
		CommandIdentify: identify,
		Data:            data,
		Desc:            desc,
		CommandName:     name,
		SendTime:        time.Now().Unix(),
		SendStatus:      int64(sendStatus),
	}
	err := psql.Mydb.Create(&m)
	if err.Error != nil {
		errors.Is(err.Error, gorm.ErrRecordNotFound)
	}
}

// 脚本处理
func scriptDealB(script_id string, device_data []byte, topic string) ([]byte, error) {
	if script_id == "" {
		logs.Info("脚本id不存在:", script_id)
		return device_data, nil
	}
	var tp_script models.TpScript
	result_b := psql.Mydb.Where("id = ?", script_id).First(&tp_script)
	if result_b.Error == nil {
		logs.Info("脚本信息存在")
		req_str, err_a := utils.ScriptDeal(tp_script.ScriptContentB, device_data, topic)
		if err_a != nil {
			return device_data, err_a
		} else {
			return []byte(req_str), nil
		}
	} else {
		logs.Info("脚本信息不存在")
		return device_data, nil
	}
}
