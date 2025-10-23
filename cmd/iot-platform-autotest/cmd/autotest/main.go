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
		zap.String("device_type", cfg.DeviceType),
		zap.String("device_id", cfg.Device.DeviceID))

	// 创建设备
	dev, err := device.NewDevice(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to create device", zap.Error(err))
	}
	if err := dev.Connect(); err != nil {
		logger.Fatal("Failed to connect device", zap.Error(err))
	}
	defer dev.Disconnect()

	logger.Info("Device connected successfully")

	// 订阅所有主题
	if err := dev.SubscribeAll(); err != nil {
		logger.Fatal("Failed to subscribe topics", zap.Error(err))
	}

	// 根据模式执行测试
	switch *testMode {
	case "telemetry":
		runTelemetryTest(dev, cfg, logger)
	case "attribute":
		runAttributeTest(dev, cfg, logger)
	case "event":
		runEventTest(dev, cfg, logger)
	case "all":
		runTelemetryTest(dev, cfg, logger)
		time.Sleep(2 * time.Second)
		runAttributeTest(dev, cfg, logger)
		time.Sleep(2 * time.Second)
		runEventTest(dev, cfg, logger)
	default:
		logger.Error("Unknown test mode", zap.String("mode", *testMode))
	}

	logger.Info("Test completed successfully")
}

func runTelemetryTest(dev device.Device, cfg *config.Config, logger *zap.Logger) {
	logger.Info("Running telemetry test...")

	data := utils.BuildTelemetryData()
	if err := dev.PublishTelemetry(data); err != nil {
		logger.Error("Failed to publish telemetry", zap.Error(err))
		return
	}

	logger.Info("Telemetry data published", zap.Any("data", data))
}

func runAttributeTest(dev device.Device, cfg *config.Config, logger *zap.Logger) {
	logger.Info("Running attribute test...")

	messageID := utils.GenerateMessageID()
	data := utils.BuildAttributeData()

	if err := dev.PublishAttribute(data, messageID); err != nil {
		logger.Error("Failed to publish attribute", zap.Error(err))
		return
	}

	logger.Info("Attribute data published",
		zap.String("message_id", messageID),
		zap.Any("data", data))
}

func runEventTest(dev device.Device, cfg *config.Config, logger *zap.Logger) {
	logger.Info("Running event test...")

	messageID := utils.GenerateMessageID()
	method := "TestEvent"
	params := map[string]interface{}{
		"test_key": "test_value",
		"count":    1,
	}

	if err := dev.PublishEvent(method, params, messageID); err != nil {
		logger.Error("Failed to publish event", zap.Error(err))
		return
	}

	logger.Info("Event published",
		zap.String("message_id", messageID),
		zap.String("method", method),
		zap.Any("params", params))
}
