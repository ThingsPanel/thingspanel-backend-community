package initialize

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"project/internal/dal"
	model "project/internal/model"
	global "project/pkg/global"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

func RedisInit() (*redis.Client, error) {
	conf, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("加载redis配置失败:%v", err)
	}

	statusConf, err := loadStatusConfig()
	if err != nil {
		return nil, fmt.Errorf("加载redis配置失败:%v", err)
	}

	client := connectRedis(conf)
	statusClient := connectRedis(statusConf)

	if checkRedisClient(client) != nil {
		return nil, fmt.Errorf("连接redis失败:%v", err)
	}
	if checkRedisClient(statusClient) != nil {
		return nil, fmt.Errorf("连接redis失败:%v", err)
	}
	global.REDIS = client
	global.STATUS_REDIS = statusClient
	// 启动SSE
	go global.InitSSEManager()
	return client, nil
}

func connectRedis(conf *RedisConfig) *redis.Client {

	redisClient := redis.NewClient(&redis.Options{
		Addr:     conf.Addr,
		Password: conf.Password,
		DB:       conf.DB,
	})
	// 如果返回nil，就创建这个DB

	return redisClient
}

func checkRedisClient(redisClient *redis.Client) error {
	// 通过 cient.Ping() 来检查是否成功连接到了 redis 服务器
	_, err := redisClient.Ping(context.Background()).Result()
	if err != nil {
		return err
	} else {
		log.Println("连接redis成完成...")
		return nil
	}
}

func loadConfig() (*RedisConfig, error) {
	redisConfig := &RedisConfig{
		Addr:     viper.GetString("db.redis.addr"),
		Password: viper.GetString("db.redis.password"),
		DB:       viper.GetInt("db.redis.db"),
	}

	if redisConfig.Addr == "" {
		redisConfig.Addr = "localhost:6379"
	}
	return redisConfig, nil
}

func loadStatusConfig() (*RedisConfig, error) {
	db := viper.GetInt("db.redis.db1")
	if db == 0 {
		db = 10 // 默认使用第11个DB
	}
	redisConfig := &RedisConfig{
		Addr:     viper.GetString("db.redis.addr"),
		Password: viper.GetString("db.redis.password"),
		DB:       db,
	}

	if redisConfig.Addr == "" {
		redisConfig.Addr = "localhost:6379"
	}
	return redisConfig, nil
}

// setRedis 将map或者结构体对象序列化为 JSON字符串 并存储在 Redis 中
func SetRedisForJsondata(key string, value interface{}, expiration time.Duration) error {
	jsonData, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return global.REDIS.Set(context.Background(), key, jsonData, expiration).Err()
}

// getRedis 从 Redis 中获取 JSON 并反序列化到指定对象
func GetRedisForJsondata(key string, dest interface{}) error {
	val, err := global.REDIS.Get(context.Background(), key).Result()
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(val), dest)
}

// 通过设备id从redis中获取设备信息
// 先从redis中获取设备信息，如果没有则从数据库中获取设备信息，并将设备信息存入redis
func GetDeviceCacheById(deviceId string) (*model.Device, error) {
	var device model.Device
	err := GetRedisForJsondata(deviceId, &device)
	if err == nil {
		return &device, nil
	}
	// 从数据库中获取设备信息
	deviceFromDB, err := dal.GetDeviceCacheById(deviceId)
	if err != nil {
		return nil, err
	}
	// 将设备信息存入redis
	err = SetRedisForJsondata(deviceId, deviceFromDB, 0)
	if err != nil {
		return nil, err
	}
	return deviceFromDB, nil
}

// 通过设备和脚本类型从redis中获取脚本
func GetScriptByDeviceAndScriptType(device *model.Device, script_type string) (*model.DataScript, error) {
	var script *model.DataScript
	script = &model.DataScript{}
	key := device.ID + "_" + script_type + "_script"
	err := GetRedisForJsondata(key, script)
	if err != nil {
		logrus.Debug("Get redis_cache key:"+key+" failed with err:", err.Error())
		script, err = dal.GetDataScriptByDeviceConfigIdAndScriptType(device.DeviceConfigID, script_type)
		if err != nil {
			return nil, err
		}
		if script == nil {
			return nil, nil
		}
		err = SetRedisForJsondata(key, script, 0)
		if err != nil {
			logrus.Debug("Set redis_cache key:"+key+" failed with err:", err.Error())
			return nil, err
		}
		logrus.Debug("Set redis_cache key:"+key+" successed with ", script)
	}
	return script, nil
}

// 清除设备信息缓存
func DelDeviceCache(deviceId string) error {
	err := global.REDIS.Del(context.Background(), deviceId).Err()
	if err != nil {
		logrus.Warn("del redis_cache key(deviceId):", deviceId, " failed with err:", err.Error())
	}
	return err
}

// 清除设备配置信息缓存
func DelDeviceConfigCache(deviceConfigId string) error {
	err := global.REDIS.Del(context.Background(), deviceConfigId+"_config").Err()
	if err != nil {
		logrus.Warn("del redis_cache key(deviceConfigId):", deviceConfigId+"_config", " failed with err:", err.Error())
	}
	return err
}

// 清除设备对应的脚本缓存
func DelDeviceDataScriptCache(deviceID string) error {
	scriptType := []string{"A", "B", "C", "D", "E", "F"}
	var key []string
	for _, scriptType := range scriptType {
		key = append(key, deviceID+"_"+scriptType+"_script")
	}

	err := global.REDIS.Del(context.Background(), key...).Err()
	if err != nil {
		logrus.Warn("del redis_cache key:", key, " failed with err:", err.Error())
	}
	return err
}
