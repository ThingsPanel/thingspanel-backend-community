package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Metrics Redis 操作封装
type Metrics struct {
	redisClient *redis.Client
	ctx         context.Context
}

// NewMetrics 创建 Metrics 实例
func NewMetrics(redisClient *redis.Client) *Metrics {
	return &Metrics{
		redisClient: redisClient,
		ctx:         context.Background(),
	}
}

// getStatsKey 获取统计指标 Key
func (m *Metrics) getStatsKey(deviceID string) string {
	return fmt.Sprintf("device:%s:diagnostics:stats", deviceID)
}

// getFailuresKey 获取失败记录 Key
func (m *Metrics) getFailuresKey(deviceID string) string {
	return fmt.Sprintf("device:%s:diagnostics:failures", deviceID)
}

func (m *Metrics) flush(ctx context.Context, counts map[string]Stats, failures []failureItem, maxFailures int) error {
	pipe := m.redisClient.Pipeline()
	for deviceID, stats := range counts {
		key := m.getStatsKey(deviceID)
		fields := [...]struct {
			name  string
			value int64
		}{
			{"uplink_total", stats.UplinkTotal},
			{"uplink_failed", stats.UplinkFailed},
			{"storage_failed", stats.StorageFailed},
			{"downlink_total", stats.DownlinkTotal},
			{"downlink_failed", stats.DownlinkFailed},
		}
		for _, field := range fields {
			if field.value != 0 {
				pipe.HIncrBy(ctx, key, field.name, field.value)
			}
		}
	}
	if maxFailures > 0 {
		updated := make(map[string]struct{})
		for _, item := range failures {
			data, err := json.Marshal(item.record)
			if err != nil {
				return err
			}
			key := m.getFailuresKey(item.deviceID)
			pipe.LPush(ctx, key, data)
			updated[key] = struct{}{}
		}
		for key := range updated {
			pipe.LTrim(ctx, key, 0, int64(maxFailures-1))
			pipe.Expire(ctx, key, 7*24*time.Hour)
		}
	}
	_, err := pipe.Exec(ctx)
	return err
}

// GetStats 获取统计指标
func (m *Metrics) GetStats(deviceID string) (*Stats, error) {
	key := m.getStatsKey(deviceID)

	// 获取所有字段
	vals, err := m.redisClient.HGetAll(m.ctx, key).Result()
	if err != nil {
		return nil, err
	}

	stats := &Stats{}

	// 解析字段（如果不存在则为0）
	if val, ok := vals["uplink_total"]; ok && val != "" {
		fmt.Sscanf(val, "%d", &stats.UplinkTotal)
	}
	if val, ok := vals["uplink_failed"]; ok && val != "" {
		fmt.Sscanf(val, "%d", &stats.UplinkFailed)
	}
	if val, ok := vals["storage_failed"]; ok && val != "" {
		fmt.Sscanf(val, "%d", &stats.StorageFailed)
	}
	if val, ok := vals["downlink_total"]; ok && val != "" {
		fmt.Sscanf(val, "%d", &stats.DownlinkTotal)
	}
	if val, ok := vals["downlink_failed"]; ok && val != "" {
		fmt.Sscanf(val, "%d", &stats.DownlinkFailed)
	}

	return stats, nil
}

// GetFailures 获取失败记录列表
func (m *Metrics) GetFailures(deviceID string, limit int) ([]FailureRecord, error) {
	key := m.getFailuresKey(deviceID)

	// 获取列表数据（LRANGE 0 limit-1）
	vals, err := m.redisClient.LRange(m.ctx, key, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	failures := make([]FailureRecord, 0, len(vals))
	for _, val := range vals {
		var record FailureRecord
		if err := json.Unmarshal([]byte(val), &record); err != nil {
			continue // 跳过解析失败的记录
		}
		failures = append(failures, record)
	}

	return failures, nil
}
