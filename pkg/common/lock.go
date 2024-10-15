package common

import (
	"project/pkg/global"
	"time"

	"github.com/sirupsen/logrus"
)

// 获取分布式锁
func AcquireLock(lockKey string, expiration time.Duration) bool {
	// 尝试获取锁
	ok, err := global.REDIS.SetNX(lockKey, true, expiration).Result()
	if err != nil {
		return false
	}
	return ok
}

// 释放分布式锁
func ReleaseLock(lockKey string) {
	// 删除锁
	err := global.REDIS.Del(lockKey).Err()
	if err != nil {
		logrus.Error("Error releasing lock:", err)
	}
}
