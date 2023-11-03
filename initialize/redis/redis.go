package redis

import (
	"ThingsPanel-Go/utils"
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

func init() {
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
	return
}

func GetStr(key string) (value string) {
	v, _ := redisCache.Get(key).Result()
	return v
}

func DelKey(key string) (err error) {
	err = redisCache.Del(key).Err()
	return
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
