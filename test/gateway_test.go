package test

import (
	"context"
	"project/initialize"
	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/mqtt"
	"project/mqtt/publish"
	"project/mqtt/subscribe"
	"testing"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

func init() {
	initialize.ViperInit("../configs/conf.yml")
	initialize.LogInIt()
	db := initialize.PgInit()
	initialize.RedisInit()
	query.SetDefault(db)

	mqtt.MqttInit()
	subscribe.SubscribeInit()
	publish.PublishInit()
}

func getDeviceInfo() *model.Device {
	result, _ := dal.GetDeviceById("6c21e49c-d0dc-7315-bd5c-5702ad789936")
	return result
}
func TestCommandSend(T *testing.T) {
	messageID := uuid.New()
	parmas := make(map[string]interface{})
	parmas["123"] = "2323"
	SubDeviceData := make(map[string]model.EventInfo, 0)
	SubDeviceData["112233445566"] = model.EventInfo{
		Method: "action",
		Params: parmas,
	}
	command := model.GatewayCommandPulish{
		GatewayData: &model.EventInfo{
			Method: "action",
			Params: parmas,
		},
		SubDeviceData: &SubDeviceData,
	}
	var ok bool
	err := publish.GatewayPublishCommandMessage(context.Background(), *getDeviceInfo(), messageID, command, func(gr model.GatewayResponse) error {

		logrus.Debug(gr.GatewayData)
		ok = true
		return nil
	})
	for {
		if ok {
			break
		}
	}
	if err != nil {
		T.Error(err)
	}
}

/*
  - Result  int    `json:"result"`
    Errcode string `json:"errcode"`
    Message string `json:"message"`
    Ts      int64  `json:"ts"`
    Method  string `json:"method"`
*/
func TestEventSend(T *testing.T) {
	messageID := uuid.New()
	command := model.GatewayResponse{
		GatewayData: &model.MqttResponse{
			Method:  "action",
			Result:  001,
			Message: "success",
		},
	}
	err := publish.GatewayPublishResponseEventMessage(context.Background(), *getDeviceInfo(), messageID, command)
	if err != nil {
		T.Error(err)
	}
}

func TestTelemetrySend(T *testing.T) {
	messageID := uuid.New()
	command := model.GatewayPublish{
		GatewayData: &map[string]interface{}{
			"test": 124,
			"cash": "wrere",
		},
		SubDeviceData: &map[string]map[string]interface{}{
			"test": map[string]interface{}{
				"asf": 232,
			},
		},
	}
	err := publish.GatewayPublishTelemetryMessage(context.Background(), *getDeviceInfo(), messageID, command)
	if err != nil {
		T.Error(err)
	}
}

func TestAttributeGet(T *testing.T) {
	messageID := uuid.New()
	command := model.GatewayAttributeGet{
		GatewayData: &[]string{
			"test",
			"cash",
			"wrere",
		},
		SubDeviceData: &map[string][]string{
			"test": []string{
				"test",
				"cash",
				"wrere",
			},
		},
	}
	err := publish.GatewayPublishGetAttributesMessage(context.Background(), *getDeviceInfo(), messageID, command)
	if err != nil {
		T.Error(err)
	}
}

func TestAttributeSet(T *testing.T) {
	messageID := uuid.New()
	command := model.GatewayPublish{
		GatewayData: &map[string]interface{}{
			"test": 124,
			"cash": "wrere",
		},
		SubDeviceData: &map[string]map[string]interface{}{
			"test": map[string]interface{}{
				"asf": 232,
			},
		},
	}
	var ok bool
	err := publish.GatewayPublishSetAttributesMessage(context.Background(), *getDeviceInfo(), messageID, command, func(gr model.GatewayResponse) error {
		logrus.Debug(gr.GatewayData)
		ok = true
		return nil
	})
	for {
		if ok {
			break
		}
	}
	if err != nil {
		T.Error(err)
	}
}
