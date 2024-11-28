package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"project/internal/model"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var mqttClient *mqtt.Client
var gatewayMqttClient *mqtt.Client

// MQTT虚拟温湿度传感器
func TempHumSensor() {
	// 创建mqtt客户端
	createClient()

	// 发布遥测消息
	go publishTelemetryMessage("devices/telemetry")
	// 发布属性消息
	go publishAttributeMessage("devices/attributes/")
	// 发布事件消息
	go publishEventMessage("devices/event/")

	// 网关设备
	createGatewayClient()

	//发布遥测消息
	go publishGatewayTelemetryMessage("gateway/telemetry")

	go publishGatewayAttributeMessage("gateway/attributes/")

	go publishGatewayEventMessage("gateway/event/")
	select {}
}

// 创建mqtt客户端
func createClient() {
	// 初始化配置
	opts := MqttConfig{
		Broker: "localhost:1883",
		User:   "sensor1",
		Pass:   "",
	}
	mqttClient = CreateMqttClient(opts)
}

func createGatewayClient() {
	// 初始化配置
	opts := MqttConfig{
		Broker: "localhost:1883",
		//Broker: "47.92.253.145:1883",
		User: "3f07250e-bdcd-1692-ea2",
		Pass: "",
	}
	gatewayMqttClient = CreateMqttClient(opts)
}

// 发布遥测消息
func publishTelemetryMessage(topic string) {
	// 每隔10秒发布一次消息
	for {
		message := make(map[string]interface{})
		// -20到40度之间的随机数且保留两位小数
		t, err := generateRandomFloat()
		if err != nil {
			log.Println("generateRandomFloat failed:", err)
		}
		message["temperature"] = t
		// 保留两位小数
		message["temperature"] = float64(int(message["temperature"].(float64)*100)) / 100
		// 0到100%之间的随机整数
		h, err := generateRandomFloat()
		if err != nil {
			log.Println("generateRandomFloat failed:", err)
		}
		message["humidity"] = h
		// 转换为json格式
		var payload []byte
		payload, err = json.Marshal(message)
		if err != nil {
			log.Println("json.Marshal failed:", err)
			return
		}
		token := (*mqttClient).Publish(topic, 0, false, payload)
		isSuccess := token.Wait()
		if !isSuccess {
			log.Println("Publish message failed", string(payload))
		} else {
			log.Println("Publish message successful:", string(payload))
		}
		// 每隔10秒发布一次消息
		<-time.After(30 * time.Second)
	}
}

func publishAttributeMessage(topic string) {
	// 每隔30秒发布一次消息
	for {
		message := make(map[string]interface{})
		message["version"] = "1.0.0"
		message["status"] = "normal"
		message["mac"] = "00:11:22:33:44:55"
		// 转换为json格式
		var payload []byte
		payload, err := json.Marshal(message)
		if err != nil {
			log.Println("json.Marshal failed:", err)
			return
		}
		messageId := GetMessageID()
		token := (*mqttClient).Publish(topic+messageId, 0, false, payload)
		isSuccess := token.Wait()
		if !isSuccess {
			log.Println("Publish message failed", string(payload))
		} else {
			log.Println("Publish message successful:", string(payload))
		}
		// 每隔30秒发布一次消息
		<-time.After(120 * time.Second)
	}
}

func publishEventMessage(topic string) {
	// 每隔60秒发布一次消息
	for {
		message := make(map[string]interface{})

		message["method"] = "alert"
		// params是map类型
		message["params"] = map[string]interface{}{
			"level":   "warning",
			"message": "temperature is too high",
		}
		// 转换为json格式
		var payload []byte
		payload, err := json.Marshal(message)
		if err != nil {
			log.Println("json.Marshal failed:", err)
			return
		}
		messageId := GetMessageID()
		token := (*mqttClient).Publish(topic+messageId, 0, false, payload)
		isSuccess := token.Wait()
		if !isSuccess {
			log.Println("Publish message failed", string(payload))
		} else {
			log.Println("Publish message successful:", string(payload))
		}
		// 每隔60秒发布一次消息
		<-time.After(120 * time.Second)
	}
}

func getTelemetryMessageParams() *map[string]interface{} {
	message := make(map[string]interface{})
	t, err := generateRandomFloat()
	if err != nil {
		log.Println("generateRandomFloat failed:", err)
		return nil
	}
	// -20到40度之间的随机数且保留两位小数
	message["temperature"] = t
	// 保留两位小数
	message["temperature"] = float64(int(message["temperature"].(float64)*100)) / 100
	// 0到100%之间的随机整数
	h, err := generateRandomFloat()
	if err != nil {
		log.Println("generateRandomFloat failed:", err)
		return nil
	}
	message["humidity"] = h

	return &message
}

func generateRandomFloat() (float64, error) {
	// 生成整数部分 [10.00, 99.99]
	integer, err := rand.Int(rand.Reader, big.NewInt(90))
	if err != nil {
		return 0, fmt.Errorf("生成整数部分失败: %v", err)
	}
	integer = integer.Add(integer, big.NewInt(10))

	// 生成小数部分 [0, 99]
	decimal, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		return 0, fmt.Errorf("生成小数部分失败: %v", err)
	}

	// 组合整数和小数部分
	result := float64(integer.Int64()) + float64(decimal.Int64())/100.0

	return result, nil
}

func getAttributeMessageParams() *map[string]interface{} {
	message := make(map[string]interface{})
	message["version"] = "1.0.0"
	message["status"] = "normal"
	message["mac"] = "00:11:22:33:44:55"

	return &message
}

func getEventMessageParams() *map[string]interface{} {

	message := make(map[string]interface{})

	message["method"] = "alert"
	// params是map类型
	message["params"] = map[string]interface{}{
		"level":   "warning",
		"message": "temperature is too high",
	}

	return &message
}

// 网关发布遥测消息
func publishGatewayTelemetryMessage(topic string) {
	// 每隔10秒发布一次消息
	for {
		subDevice := make(map[string]map[string]interface{})
		subDevice["3d6bd6af"] = *getTelemetryMessageParams()
		payloads := &model.GatewayPublish{
			GatewayData:   getTelemetryMessageParams(),
			SubDeviceData: &subDevice,
		}
		// 转换为json格式
		var payload []byte
		payload, err := json.Marshal(payloads)
		if err != nil {
			log.Println("json.Marshal failed:", err)
			return
		}
		token := (*gatewayMqttClient).Publish(topic, 0, false, payload)
		token.Wait()
		log.Println("Publish message:", string(payload))
		// 每隔10秒发布一次消息
		<-time.After(50 * time.Second)
	}
}

// 网关发布遥测消息
func publishGatewayAttributeMessage(topic string) {
	// 每隔10秒发布一次消息
	for {
		subDevice := make(map[string]map[string]interface{})
		subDevice["3d6bd6af"] = *getAttributeMessageParams()
		payloads := &model.GatewayPublish{
			GatewayData:   getAttributeMessageParams(),
			SubDeviceData: &subDevice,
		}
		// 转换为json格式
		var payload []byte
		payload, err := json.Marshal(payloads)
		if err != nil {
			log.Println("json.Marshal failed:", err)
			return
		}
		messageId := GetMessageID()
		token := (*gatewayMqttClient).Publish(topic+messageId, 0, false, payload)
		token.Wait()
		log.Println("Publish message:", string(payload))
		// 每隔10秒发布一次消息
		<-time.After(40 * time.Second)
	}
}

// 网关发布遥测消息
func publishGatewayEventMessage(topic string) {
	// 每隔10秒发布一次消息
	for {
		subDevice := make(map[string]map[string]interface{})
		subDevice["3d6bd6af"] = *getEventMessageParams()
		payloads := &model.GatewayPublish{
			GatewayData:   getEventMessageParams(),
			SubDeviceData: &subDevice,
		}
		// 转换为json格式
		var payload []byte
		payload, err := json.Marshal(payloads)
		if err != nil {
			log.Println("json.Marshal failed:", err)
			return
		}
		messageId := GetMessageID()
		token := (*gatewayMqttClient).Publish(topic+messageId, 0, false, payload)
		token.Wait()
		log.Println("Publish message:", string(payload))
		// 每隔10秒发布一次消息
		<-time.After(30 * time.Second)
	}
}
