package service

import (
	"context"
	"fmt"
	"project/global"
	tpErrors "project/internal/errors"
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

func (l *LoginLock) getLockKey(username string) string {
	return fmt.Sprintf("user:%s:lock_until", username)
}

func (l *LoginLock) getKey(username string) string {
	return fmt.Sprintf("user:%s:failed_attempts", username)
}

func (l *LoginLock) GetAllowLogin(ctx context.Context, username string) error {

	lockKey := l.getLockKey(username)

	// Check if the account is locked
	lockUntil, err := global.REDIS.Get(lockKey).Result()
	if err == nil {
		lockUntilTime, err := time.Parse(time.RFC3339, lockUntil)
		if err == nil && time.Now().Before(lockUntilTime) {
			//return errors.Errorf("Account %s is locked. Try again later.", username)
			// return errors.Errorf("您已连续登录失败%d次，账号锁定%d分钟，解锁时间为：%s,请你耐心等待！",
			//	l.MaxFailedAttempts, l.LockDuration/time.Minute, lockUntilTime.Format(time.DateTime))
			return tpErrors.Wrap(errors.Errorf("您已连续登录失败%d次，账号锁定%d分钟，解锁时间为：%s,请你耐心等待！",
				l.MaxFailedAttempts, l.LockDuration/time.Minute, lockUntilTime.Format(time.DateTime)), tpErrors.ErrTooManyAttempts)
		}
	}
	return nil
}

func (l *LoginLock) LoginSuccess(ctx context.Context, username string) error {
	key := l.getKey(username)
	return global.REDIS.Del(key).Err()
}

func (l *LoginLock) LoginFail(ctx context.Context, username string) error {
	key := l.getKey(username)
	lockKey := l.getLockKey(username)
	failedAttempts, err := global.REDIS.Incr(key).Result()
	if err != nil {
		return errors.Errorf("Error incrementing failed attempts for %s: %v", username, err)
	}

	if failedAttempts >= l.MaxFailedAttempts {
		lockUntilTime := time.Now().Add(l.LockDuration)
		global.REDIS.Set(lockKey, lockUntilTime.Format(time.RFC3339), l.LockDuration)
	}

	return nil
}
