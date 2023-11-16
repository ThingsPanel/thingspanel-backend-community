package redis

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	"encoding/json"
	"log"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/spf13/viper"
	"gopkg.in/redis.v5"
)

var redisCache *redis.Client

// 创建 redis 客户端
func createClient(redisHost string, password string, dataBase int) *redis.Client {
	log.Println("连接redis...")
	client := redis.NewClient(&redis.Options{
		Addr:         redisHost,
		Password:     password,
		DB:           dataBase,
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 1 * time.Minute,
		PoolTimeout:  2 * time.Minute,
		IdleTimeout:  10 * time.Minute,
		PoolSize:     1000,
	})

	// 通过 cient.Ping() 来检查是否成功连接到了 redis 服务器
	_, err := client.Ping().Result()
	if err != nil {
		logs.Error("连接redis连接失败,", err)
	} else {
		log.Println("连接redis成完成...")
	}

	return client
}

func Init() {
	redisHost := viper.GetString("db.redis.conn")
	dataBase := viper.GetInt("db.redis.dbNum")
	password := viper.GetString("db.redis.password")

	redisCache = createClient(redisHost, password, dataBase)
}

func SetStr(key, value string, time time.Duration) (err error) {
	err = redisCache.Set(key, value, time).Err()
	if err != nil {
		logs.Error("set key:", utils.ReplaceUserInput(key), ",value:", utils.ReplaceUserInput(value), err)
	}
	return err
}

func GetStr(key string) (value string) {
	v, _ := redisCache.Get(key).Result()
	return v
}

func DelKey(key string) (err error) {
	err = redisCache.Del(key).Err()
	return err
}

// SetNX 尝试获取锁
func SetNX(key, value string, expiration time.Duration) (ok bool, err error) {
	ok, err = redisCache.SetNX(key, value, expiration).Result()
	return
}

// SetNX 释放锁
func DelNX(key string) (err error) {
	err = redisCache.Del(key).Err()
	return
}

// setRedis 将任何类型的对象序列化为 JSON 并存储在 Redis 中
func SetRedisForJsondata(key string, value interface{}, expiration time.Duration) error {
	jsonData, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return redisCache.Set(key, jsonData, expiration).Err()
}

// getRedis 从 Redis 中获取 JSON 并反序列化到指定对象
func GetRedisForJsondata(key string, dest interface{}) error {
	val, err := redisCache.Get(key).Result()
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(val), dest)
}

// 通过token从redis中获取设备信息
// 先从redis中获取设备id，如果没有则从数据库中获取设备信息，并将设备信息和token存入redis
func GetDeviceByToken(token string) (*models.Device, error) {
	var device *models.Device
	deviceId := GetStr(token)
	if deviceId == "" {
		result := psql.Mydb.Where("token = ?", token).First(device)
		if result.Error != nil {
			return nil, result.Error
		}
		// 修改token的时候，需要删除旧的token
		// 将token存入redis
		err := SetStr(token, device.ID, 0)
		if err != nil {
			return nil, err
		}
		// 将设备信息存入redis
		err = SetRedisForJsondata(device.ID, *device, 0)
		if err != nil {
			return nil, err
		}
	}
	device, err := GetDeviceById(deviceId)
	if err != nil {
		return nil, err
	}
	return device, nil
}

// 通过设备id从redis中获取设备信息
// 先从redis中获取设备信息，如果没有则从数据库中获取设备信息，并将设备信息存入redis
func GetDeviceById(deviceId string) (*models.Device, error) {
	var device models.Device
	err := GetRedisForJsondata(deviceId, &device)
	if err != nil {
		result := psql.Mydb.Where("id = ?", deviceId).First(&device)
		if result.Error != nil {
			return nil, result.Error
		}
		// 将设备信息存入redis
		err = SetRedisForJsondata(deviceId, device, 0)
		if err != nil {
			return nil, err
		}
	}
	return &device, nil
}
