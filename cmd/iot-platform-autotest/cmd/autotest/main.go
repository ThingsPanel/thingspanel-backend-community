package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"

	"iot-platform-autotest/internal/config"
	"iot-platform-autotest/internal/device"
	"iot-platform-autotest/internal/utils"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	testMode := flag.String("mode", "telemetry", "Test mode: telemetry, attribute, event, all")
	flag.Parse()

	// 初始化日志
	logger, err := zap.NewDevelopment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
	}

	logger.Info("Starting IoT Platform Autotest",
		zap.String("mode", *testMode),
		zap.String("device_id", cfg.Device.DeviceID))

	// 创建MQTT设备
	mqttDevice := device.NewMQTTDevice(cfg, logger)
	if err := mqttDevice.Connect(); err != nil {
		logger.Fatal("Failed to connect MQTT", zap.Error(err))
	}
	defer mqttDevice.Disconnect()

	logger.Info("MQTT device connected successfully")

	// 订阅所有主题
	if err := mqttDevice.SubscribeAll(); err != nil {
		logger.Fatal("Failed to subscribe topics", zap.Error(err))
	}

	// 根据模式执行测试
	switch *testMode {
	case "telemetry":
		runTelemetryTest(mqttDevice, cfg, logger)
	case "attribute":
		runAttributeTest(mqttDevice, cfg, logger)
	case "event":
		runEventTest(mqttDevice, cfg, logger)
	case "all":
		runTelemetryTest(mqttDevice, cfg, logger)
		time.Sleep(2 * time.Second)
		runAttributeTest(mqttDevice, cfg, logger)
		time.Sleep(2 * time.Second)
		runEventTest(mqttDevice, cfg, logger)
	default:
		logger.Error("Unknown test mode", zap.String("mode", *testMode))
	}

	logger.Info("Test completed successfully")
}

func runTelemetryTest(device *device.MQTTDevice, cfg *config.Config, logger *zap.Logger) {
	logger.Info("Running telemetry test...")

	data := utils.BuildTelemetryData()
	if err := device.PublishTelemetry(data); err != nil {
		logger.Error("Failed to publish telemetry", zap.Error(err))
		return
	}

	logger.Info("Telemetry data published", zap.Any("data", data))
}

func runAttributeTest(device *device.MQTTDevice, cfg *config.Config, logger *zap.Logger) {
	logger.Info("Running attribute test...")

	messageID := utils.GenerateMessageID()
	data := utils.BuildAttributeData()

	if err := device.PublishAttribute(data, messageID); err != nil {
		logger.Error("Failed to publish attribute", zap.Error(err))
		return
	}

	logger.Info("Attribute data published",
		zap.String("message_id", messageID),
		zap.Any("data", data))
}

func runEventTest(device *device.MQTTDevice, cfg *config.Config, logger *zap.Logger) {
	logger.Info("Running event test...")

	messageID := utils.GenerateMessageID()
	method := "TestEvent"
	params := map[string]interface{}{
		"test_key": "test_value",
		"count":    1,
	}

	if err := device.PublishEvent(method, params, messageID); err != nil {
		logger.Error("Failed to publish event", zap.Error(err))
		return
	}

	logger.Info("Event published",
		zap.String("message_id", messageID),
		zap.String("method", method),
		zap.Any("params", params))
}
