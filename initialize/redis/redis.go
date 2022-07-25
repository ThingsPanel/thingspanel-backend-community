package redis

import (
	"fmt"
	"os"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/beego/beego/v2/server/web"
	"gopkg.in/redis.v5"
)

var redisCache *redis.Client

// 创建 redis 客户端
func createClient(redisHost string, password string, dataBase int) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     redisHost,
		Password: password,
		DB:       dataBase,
	})

	// 通过 cient.Ping() 来检查是否成功连接到了 redis 服务器
	_, err := client.Ping().Result()
	if err != nil {
		logs.Error("连接失败,", err)
	} else {
		fmt.Println("redis连接成功...")
	}

	return client
}

func init() {
	redisHost := os.Getenv("TP_REDIS_HOST")
	if redisHost == "" {
		redisHost, _ = web.AppConfig.String("redis.conn")
	}
	dataBase, _ := web.AppConfig.Int("redis.dbNum")
	password, _ := web.AppConfig.String("redis.password")
	redisCache = createClient(redisHost, password, dataBase)
}

func SetStr(key, value string, time time.Duration) (err error) {
	err = redisCache.Set(key, value, time).Err()
	if err != nil {
		logs.Error("set key:", key, ",value:", value, err)
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
