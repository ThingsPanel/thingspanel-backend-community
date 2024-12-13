package service

import (
	"context"
	"fmt"
	"project/pkg/errcode"
	"project/pkg/global"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
)

type LoginLock struct {
	MaxFailedAttempts int64
	LockDuration      time.Duration
}

// 获取登录锁定规则
func NewLoginLock() *LoginLock {
	maxFailedAttempts := viper.GetInt64("classified-protect.login-max-fail-times")
	lockDuration := viper.GetDuration("classified-protect.login-fail-locked-seconds")
	return &LoginLock{
		MaxFailedAttempts: maxFailedAttempts,
		LockDuration:      lockDuration * time.Second,
	}
}

func (*LoginLock) getLockKey(username string) string {
	return fmt.Sprintf("user:%s:lock_until", username)
}

func (*LoginLock) getKey(username string) string {
	return fmt.Sprintf("user:%s:failed_attempts", username)
}

func (l *LoginLock) GetAllowLogin(_ context.Context, username string) error {

	lockKey := l.getLockKey(username)

	// Check if the account is locked
	lockUntil, err := global.REDIS.Get(context.Background(), lockKey).Result()
	if err == nil {
		lockUntilTime, err := time.Parse(time.RFC3339, lockUntil)
		// 业务代码
		if err == nil && time.Now().Before(lockUntilTime) {
			return errcode.WithVars(errcode.CodeTooManyAttempts, map[string]interface{}{
				"attempts":    l.MaxFailedAttempts,
				"duration":    l.LockDuration / time.Minute,
				"unlock_time": lockUntilTime.Format(time.DateTime),
			})
		}
	}
	return nil
}

func (l *LoginLock) LoginSuccess(_ context.Context, username string) error {
	key := l.getKey(username)
	return global.REDIS.Del(context.Background(), key).Err()
}

func (l *LoginLock) LoginFail(_ context.Context, username string) error {
	key := l.getKey(username)
	lockKey := l.getLockKey(username)
	failedAttempts, err := global.REDIS.Incr(context.Background(), key).Result()
	if err != nil {
		return errors.Errorf("Error incrementing failed attempts for %s: %v", username, err)
	}

	if failedAttempts >= l.MaxFailedAttempts {
		lockUntilTime := time.Now().Add(l.LockDuration)
		global.REDIS.Set(context.Background(), lockKey, lockUntilTime.Format(time.RFC3339), l.LockDuration)
	}

	return nil
}
