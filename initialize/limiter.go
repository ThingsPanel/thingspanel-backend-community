package initialize

import (
	"sync"

	"golang.org/x/time/rate"
)

type AutomateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

var alimit *AutomateLimiter

func NewAutomateLimiter() *AutomateLimiter {
	if alimit == nil {
		alimit = &AutomateLimiter{
			limiters: make(map[string]*rate.Limiter),
		}
	}
	return alimit
}

func (rl *AutomateLimiter) GetLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, ok := rl.limiters[key]
	if !ok {
		limiter = rate.NewLimiter(rate.Limit(1.0/3.0), 10) // 每秒处理1个请求，最多允许10个并发请求
		rl.limiters[key] = limiter
	}
	return limiter
}

func (rl *AutomateLimiter) Allow(key string) bool {
	limiter := rl.GetLimiter(key)
	return limiter.Allow()
}

type DeviceAuthLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

var daLimiter *DeviceAuthLimiter

func NewDeviceAuthLimiter() *DeviceAuthLimiter {
	if daLimiter == nil {
		daLimiter = &DeviceAuthLimiter{
			limiters: make(map[string]*rate.Limiter),
		}
	}
	return daLimiter
}

func (dal *DeviceAuthLimiter) GetLimiter(key string) *rate.Limiter {
	dal.mu.Lock()
	defer dal.mu.Unlock()

	limiter, ok := dal.limiters[key]
	if !ok {
		limiter = rate.NewLimiter(rate.Limit(1.0/3.0), 1) // 每3秒1个请求，突发容量为1
		dal.limiters[key] = limiter
	}
	return limiter
}

func (dal *DeviceAuthLimiter) Allow(key string) bool {
	limiter := dal.GetLimiter(key)
	return limiter.Allow()
}
