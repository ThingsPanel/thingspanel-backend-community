package service

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"project/pkg/global"
)

type runtimeDebugClientKey struct{}

// RuntimeDebugContext limits only this internal capability's Redis I/O. In
// go-redis v9.7 WithTimeout clones options and shares the existing connection
// pool; this view owns no pool and MUST NOT be closed. Public clients/options
// remain unchanged. Redis socket I/O can finish after ctx cancellation, but
// never waits indefinitely: network I/O is one second, source dial/pool waits
// are at most five seconds and at most one retry/backoff is permitted.
func RuntimeDebugContext(ctx context.Context) (context.Context, error) {
	source := global.REDIS
	if source == nil {
		return nil, errors.New("runtime debug Redis unavailable")
	}
	opts := source.Options()
	if opts.DialTimeout <= 0 || opts.DialTimeout > 5*time.Second || opts.PoolTimeout <= 0 || opts.PoolTimeout > 5*time.Second || opts.MaxRetries > 1 || opts.MaxRetries < 0 || opts.MinRetryBackoff > time.Second || opts.MaxRetryBackoff > time.Second || opts.OnConnect != nil {
		return nil, errors.New("runtime debug Redis timeouts are not bounded")
	}
	return context.WithValue(ctx, runtimeDebugClientKey{}, source.WithTimeout(time.Second)), nil
}

func deviceDebugRedis(ctx context.Context) *redis.Client {
	if client, ok := ctx.Value(runtimeDebugClientKey{}).(*redis.Client); ok {
		return client
	}
	return global.REDIS
}
