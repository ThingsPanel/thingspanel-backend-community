package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
)

// Metrics Redis 操作封装
type Metrics struct {
	redisClient   *redis.Client
	logicalClient *redis.Client
	ctx           context.Context
}

// NewMetrics 创建 Metrics 实例
func NewMetrics(redisClient *redis.Client) *Metrics {
	var logical *redis.Client
	if redisClient != nil {
		opts := *redisClient.Options()
		opts.ContextTimeoutEnabled = true
		opts.DialTimeout = 500 * time.Millisecond
		opts.ReadTimeout = 500 * time.Millisecond
		opts.WriteTimeout = 500 * time.Millisecond
		opts.PoolTimeout = 500 * time.Millisecond
		opts.MaxRetries = -1
		opts.PoolSize = 2
		opts.MinIdleConns = 0
		opts.MaxIdleConns = 2
		opts.MaxActiveConns = 2
		logical = redis.NewClient(&opts)
	}
	return &Metrics{
		redisClient:   redisClient,
		logicalClient: logical,
		ctx:           context.Background(),
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

// IncrementUplinkTotal 增加上行消息总数
func (m *Metrics) IncrementUplinkTotal(deviceID string) error {
	key := m.getStatsKey(deviceID)
	return m.redisClient.HIncrBy(m.ctx, key, "uplink_total", 1).Err()
}

// IncrementUplinkFailed 增加上行处理失败数
func (m *Metrics) IncrementUplinkFailed(deviceID string) error {
	key := m.getStatsKey(deviceID)
	return m.redisClient.HIncrBy(m.ctx, key, "uplink_failed", 1).Err()
}

// IncrementStorageFailed 增加存储失败数
func (m *Metrics) IncrementStorageFailed(deviceID string) error {
	key := m.getStatsKey(deviceID)
	return m.redisClient.HIncrBy(m.ctx, key, "storage_failed", 1).Err()
}

// IncrementDownlinkTotal 增加下行指令总数
func (m *Metrics) IncrementDownlinkTotal(deviceID string) error {
	key := m.getStatsKey(deviceID)
	return m.redisClient.HIncrBy(m.ctx, key, "downlink_total", 1).Err()
}

// IncrementDownlinkFailed 增加下行失败数
func (m *Metrics) IncrementDownlinkFailed(deviceID string) error {
	key := m.getStatsKey(deviceID)
	return m.redisClient.HIncrBy(m.ctx, key, "downlink_failed", 1).Err()
}

// AddFailure 添加失败记录（保留最新 N 条）
func (m *Metrics) AddFailure(deviceID string, record FailureRecord, maxFailures int) error {
	key := m.getFailuresKey(deviceID)

	// 序列化失败记录
	jsonData, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal failure record failed: %w", err)
	}

	// 使用 Pipeline 原子操作：LPUSH + LTRIM
	pipe := m.redisClient.Pipeline()
	pipe.LPush(m.ctx, key, jsonData)
	pipe.LTrim(m.ctx, key, 0, int64(maxFailures-1)) // 保留 0 到 maxFailures-1，共 maxFailures 条
	pipe.Expire(m.ctx, key, 7*24*time.Hour)         // 7天过期（防止内存泄漏）

	_, err = pipe.Exec(m.ctx)
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
	// Do not reinterpret old per-point counters as logical-message success.
	logical, err := m.redisClient.HGetAll(m.ctx, m.logicalKeys(deviceID)[0]).Result()
	if err != nil {
		return nil, err
	}
	stats.UplinkTotal, stats.UplinkFailed, stats.StorageFailed = 0, 0, 0
	for field, target := range map[string]*int64{"uplink_total": &stats.UplinkTotal, "uplink_failed": &stats.UplinkFailed, "uplink_success": &stats.UplinkSuccess, "storage_total": &stats.StorageTotal, "storage_failed": &stats.StorageFailed, "storage_success": &stats.StorageSuccess} {
		if value := logical[field]; value != "" {
			fmt.Sscanf(value, "%d", target)
		}
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
	logical, err := m.redisClient.LRange(m.ctx, m.logicalKeys(deviceID)[3], 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	vals = append(vals, logical...)

	failures := make([]FailureRecord, 0, len(vals))
	for _, val := range vals {
		var record FailureRecord
		if err := json.Unmarshal([]byte(val), &record); err != nil {
			continue // 跳过解析失败的记录
		}
		failures = append(failures, record)
	}

	sort.Slice(failures, func(i, j int) bool { return failures[i].Timestamp.After(failures[j].Timestamp) })
	if len(failures) > limit {
		failures = failures[:limit]
	}
	return failures, nil
}
