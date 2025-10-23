package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 总配置结构
type Config struct {
	DeviceType string         `yaml:"device_type"` // "direct" 或 "gateway"
	MQTT       MQTTConfig     `yaml:"mqtt"`
	Device     DeviceConfig   `yaml:"device"`
	Gateway    GatewayConfig  `yaml:"gateway"`  // 网关配置(当 device_type="gateway" 时使用)
	Database   DatabaseConfig `yaml:"database"`
	API        APIConfig      `yaml:"api"`
	Test       TestConfig     `yaml:"test"`
}

// MQTTConfig MQTT配置
type MQTTConfig struct {
	Broker       string `yaml:"broker"`
	ClientID     string `yaml:"client_id"`
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`
	QoS          byte   `yaml:"qos"`
	CleanSession bool   `yaml:"clean_session"`
	KeepAlive    int    `yaml:"keep_alive"`
}

// DeviceConfig 设备配置
type DeviceConfig struct {
	DeviceID     string `yaml:"device_id"`
	DeviceNumber string `yaml:"device_number"`
}

// SubDeviceConfig 子设备配置
type SubDeviceConfig struct {
	SubDeviceNumber string `yaml:"sub_device_number"`
	DeviceID        string `yaml:"device_id"`
	Description     string `yaml:"description"`
}

// SubGatewayConfig 子网关配置
type SubGatewayConfig struct {
	SubGatewayNumber string            `yaml:"sub_gateway_number"`
	DeviceID         string            `yaml:"device_id"`
	Description      string            `yaml:"description"`
	SubDevices       []SubDeviceConfig `yaml:"sub_devices"`
}

// GatewayConfig 网关配置
type GatewayConfig struct {
	SubDevices  []SubDeviceConfig  `yaml:"sub_devices"`
	SubGateways []SubGatewayConfig `yaml:"sub_gateways"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	DBName       string `yaml:"dbname"`
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`
	SSLMode      string `yaml:"sslmode"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

// APIConfig API配置
type APIConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Timeout int    `yaml:"timeout"`
}

// TestConfig 测试配置
type TestConfig struct {
	WaitDBSyncSeconds       int    `yaml:"wait_db_sync_seconds"`
	WaitMQTTResponseSeconds int    `yaml:"wait_mqtt_response_seconds"`
	RetryTimes              int    `yaml:"retry_times"`
	LogLevel                string `yaml:"log_level"`
}

// Load 加载配置文件
func Load(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// Validate 验证配置
func (c *Config) Validate() error {
	// 验证设备类型
	if c.DeviceType == "" {
		c.DeviceType = "direct" // 默认为直连设备
	}
	if c.DeviceType != "direct" && c.DeviceType != "gateway" {
		return fmt.Errorf("device_type must be 'direct' or 'gateway', got: %s", c.DeviceType)
	}

	if c.MQTT.Broker == "" {
		return fmt.Errorf("mqtt broker is required")
	}
	if c.Device.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if c.Device.DeviceNumber == "" {
		return fmt.Errorf("device_number is required")
	}
	if c.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if c.API.BaseURL == "" {
		return fmt.Errorf("api base_url is required")
	}
	if c.API.APIKey == "" {
		return fmt.Errorf("api api_key is required")
	}
	return nil
}

// GetDSN 获取数据库连接字符串
func (c *DatabaseConfig) GetDSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.Username, c.Password, c.DBName, c.SSLMode)
}
