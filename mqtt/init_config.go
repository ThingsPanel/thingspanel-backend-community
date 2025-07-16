package mqtt

import (
	"encoding/json"
	"fmt"
	"project/internal/model"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// MqttDirectResponseFunc mqtt直连设置回复
type MqttDirectResponseFunc = func(response model.MqttResponse) error

var MqttConfig Config

// MqttResponseFuncMap mqtt直连设置回复
var MqttResponseFuncMap map[string]chan model.MqttResponse

func MqttInit() error {
	// 初始化配置
	err := loadConfig()
	if err != nil {
		return err
	}
	// 初始化回复map
	MqttResponseFuncMap = make(map[string]chan model.MqttResponse)
	return nil
}

func loadConfig() error {
	var configMap map[string]interface{}
	// 将 map 映射到 MQTTConfig 结构体
	// 注意！！！yml文件中带_的key，是无法通过UnmarshalKey解析的
	err := viper.Unmarshal(&configMap)
	if err != nil {
		return fmt.Errorf("unable to decode into struct, %s", err)
	}
	// 将 map 转换为 json
	jsonStr, err := json.Marshal(configMap["mqtt"])
	if err != nil {
		return fmt.Errorf("unable to marshal config, %s", err)
	}
	// 将 json 转换为结构体
	err = json.Unmarshal(jsonStr, &MqttConfig)
	if err != nil {
		return fmt.Errorf("unable to unmarshal config, %s", err)
	}

	// 打印配置
	logrus.Debug("mqtt config:", MqttConfig)

	// 单独获取 broker 配置
	broker := viper.GetString("mqtt.broker")
	if broker == "" {
		broker = "localhost:1883"
		logrus.Println("Using default broker:", broker)
	}
	MqttConfig.Broker = broker

	// 单独获取 user 配置
	user := viper.GetString("mqtt.user")
	if user == "" {
		user = "root"
		logrus.Println("Using default user:", user)
	}
	MqttConfig.User = user

	// 单独获取 pass 配置
	pass := viper.GetString("mqtt.pass")
	if pass == "" {
		pass = "root"
		logrus.Println("Using default pass:", pass)
	}
	MqttConfig.Pass = pass

	// 单独获取 channel_buffer_size 配置
	channelBufferSize := viper.GetInt("mqtt.channel_buffer_size")
	if channelBufferSize == 0 {
		channelBufferSize = 10000
		logrus.Println("Using default channel_buffer_size:", channelBufferSize)
	}
	MqttConfig.ChannelBufferSize = channelBufferSize

	// 单独获取 write_workers 配置
	writeWorkers := viper.GetInt("mqtt.write_workers")
	if writeWorkers == 0 {
		writeWorkers = 10
		logrus.Println("Using default write_workers:", writeWorkers)
	}
	MqttConfig.WriteWorkers = writeWorkers

	// 单独获取 pool_size 配置
	poolSize := viper.GetInt("mqtt.telemetry.pool_size")
	if poolSize == 0 {
		poolSize = 100
		logrus.Println("Using default pool_size:", poolSize)
	}
	MqttConfig.Telemetry.PoolSize = poolSize

	// 单独获取 batch_size 配置
	batchSize := viper.GetInt("mqtt.telemetry.batch_size")
	if batchSize == 0 {
		batchSize = 100
		logrus.Println("Using default batch_size:", batchSize)
	}
	return nil
}
