package notification

import (
	"math"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

func TestJitteredRetryPolicyRequiresExplicitRetryableRejection(t *testing.T) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	policy := JitteredRetryPolicy{BaseDelay: time.Second, MaxDelay: time.Minute, Jitter: func(time.Duration) time.Duration { return 0 }}
	result := contract.PluginSendResultV1{Outcome: "rejected", Error: &contract.PluginDeliveryErrorV1{Code: "rate_limited", Retryable: true}}
	got, ok := policy.NextRetry(result, 1, now, expires)
	if !ok || !got.Equal(now.Add(time.Second)) {
		t.Fatalf("retry=%v allowed=%v, want one second", got, ok)
	}
	for _, outcome := range []string{"unknown", "accepted"} {
		result.Outcome = outcome
		if _, ok := policy.NextRetry(result, 1, now, expires); ok {
			t.Fatalf("outcome %q authorized automatic retry", outcome)
		}
	}
	result.Outcome = "rejected"
	result.Error.Retryable = false
	if _, ok := policy.NextRetry(result, 1, now, expires); ok {
		t.Fatal("non-retryable rejection authorized retry")
	}
}

func TestJitteredRetryPolicyHonorsRetryAfterExpiryAndAttemptLimit(t *testing.T) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	expires := now.Add(5 * time.Minute)
	policy := JitteredRetryPolicy{BaseDelay: time.Second, MaxDelay: 2 * time.Minute, Jitter: func(max time.Duration) time.Duration { return max }}
	after := 5
	result := contract.PluginSendResultV1{Outcome: "rejected", Error: &contract.PluginDeliveryErrorV1{Code: "rate_limited", Retryable: true, RetryAfterSeconds: &after}}
	got, ok := policy.NextRetry(result, 2, now, expires)
	if !ok || got.Before(now.Add(5*time.Second)) || got.After(now.Add(7*time.Second)) {
		t.Fatalf("retry=%v allowed=%v, Retry-After was not a lower bound", got, ok)
	}
	tooLong := int(math.MaxInt)
	result.Error.RetryAfterSeconds = &tooLong
	if _, ok := policy.NextRetry(result, 1, now, expires); ok {
		t.Fatal("overflow-sized Retry-After authorized a retry")
	}
	result.Error.RetryAfterSeconds = nil
	if _, ok := policy.NextRetry(result, maxDeliveryAttempts, now, expires); ok {
		t.Fatal("attempt limit authorized another retry")
	}
	if _, ok := policy.NextRetry(result, 1, now, now); ok {
		t.Fatal("expired delivery authorized retry")
	}
}
