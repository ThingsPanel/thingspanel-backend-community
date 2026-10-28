package notification

import (
	"crypto/rand"
	"math/big"
	"time"

	"encore.app/l3-core/notification/contract"
)

// JitteredRetryPolicy adds positive jitter to the local exponential delay. A
// provider Retry-After value remains a lower bound; if it exceeds MaxDelay the
// attempt is terminal rather than retrying earlier than requested.
type JitteredRetryPolicy struct {
	BaseDelay time.Duration
	MaxDelay  time.Duration
	Jitter    func(max time.Duration) time.Duration
}

func NewJitteredRetryPolicy() JitteredRetryPolicy {
	return JitteredRetryPolicy{
		BaseDelay: time.Second,
		MaxDelay:  30 * time.Minute,
		Jitter:    securePositiveJitter,
	}
}

func (p JitteredRetryPolicy) NextRetry(result contract.PluginSendResultV1, attempts int, now, expiresAt time.Time) (time.Time, bool) {
	if attempts < 1 || attempts >= maxDeliveryAttempts || result.Outcome != "rejected" || result.Error == nil || !result.Error.Retryable || now.IsZero() || !expiresAt.After(now) {
		return time.Time{}, false
	}
	base := p.BaseDelay
	if base <= 0 {
		base = time.Second
	}
	maxDelay := p.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 30 * time.Minute
	}
	for n := 1; n < attempts; n++ {
		if base > maxDelay/2 {
			base = maxDelay
			break
		}
		base *= 2
	}
	delay := base
	if result.Error.RetryAfterSeconds != nil {
		retryAfter := *result.Error.RetryAfterSeconds
		if retryAfter < 0 || int64(retryAfter) > int64(maxDelay/time.Second) {
			return time.Time{}, false
		}
		providerDelay := time.Duration(retryAfter) * time.Second
		if providerDelay > delay {
			delay = providerDelay
		}
	}
	jitterMax := delay / 4
	jitter := p.Jitter
	if jitter == nil {
		jitter = securePositiveJitter
	}
	added := jitter(jitterMax)
	if added < 0 {
		added = 0
	}
	if added > jitterMax {
		added = jitterMax
	}
	if delay > maxDelay-added {
		delay = maxDelay
	} else {
		delay += added
	}
	next := now.Add(delay)
	if !next.After(now) || !next.Before(expiresAt) {
		return time.Time{}, false
	}
	return next, true
}

func securePositiveJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	bound := big.NewInt(int64(max) + 1)
	n, err := rand.Int(rand.Reader, bound)
	if err != nil {
		return max / 2
	}
	return time.Duration(n.Int64())
}
