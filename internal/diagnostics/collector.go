package diagnostics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"project/pkg/global"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Config 诊断配置
type Config struct {
	Enabled            bool          // 是否启用
	MaxFailures        int           // 保留失败记录数
	BatchFlushInterval time.Duration // 批量刷新间隔
}

// Collector 诊断收集器（单例，异步缓冲）
type Collector struct {
	metrics *Metrics
	config  Config
	logger  *logrus.Logger

	// 异步缓冲
	buffer      []failureItem
	counts      map[string]Stats
	dropped     bool
	bufferMu    sync.Mutex
	stopCh      chan struct{}
	doneCh      chan struct{}
	flushTicker *time.Ticker

	// 初始化标志
	initialized bool
	initOnce    sync.Once
	stopOnce    sync.Once
}

// failureItem 缓冲项
type failureItem struct {
	deviceID string
	record   FailureRecord
}

var (
	instance *Collector
	once     sync.Once
)

// GetInstance 获取单例实例
func GetInstance() *Collector {
	once.Do(func() {
		instance = &Collector{
			buffer: make([]failureItem, 0, 10),
			counts: make(map[string]Stats),
			stopCh: make(chan struct{}),
			doneCh: make(chan struct{}),
			logger: logrus.StandardLogger(),
			config: DefaultConfig(),
		}
	})
	return instance
}

// DefaultConfig 默认配置
func DefaultConfig() Config {
	return Config{
		Enabled:            true,
		MaxFailures:        5,
		BatchFlushInterval: 1 * time.Second,
	}
}

// Init 初始化收集器（从配置加载）
func (c *Collector) Init(config Config, redisClient interface{}) error {
	var err error
	c.initOnce.Do(func() {
		// 如果未启用，直接返回
		if !config.Enabled {
			c.logger.Info("diagnostics disabled, skipping initialization")
			return
		}

		// 创建 Metrics 实例
		// 优先使用传入的 redisClient，否则使用 global.REDIS
		var rds *redis.Client
		if redisClient != nil {
			if r, ok := redisClient.(*redis.Client); ok {
				rds = r
			}
		}
		if rds == nil {
			rds = global.REDIS
		}
		if rds == nil {
			err = ErrRedisNotInitialized
			return
		}
		c.metrics = NewMetrics(rds)

		c.config = config
		c.initialized = true

		// 启动后台刷新协程
		if config.BatchFlushInterval <= 0 {
			config.BatchFlushInterval = DefaultConfig().BatchFlushInterval
		}
		c.flushTicker = time.NewTicker(config.BatchFlushInterval)
		go c.run()

		c.logger.Info("diagnostics collector initialized")
	})
	return err
}

// run 后台刷新协程
func (c *Collector) run() {
	defer close(c.doneCh)

	for {
		select {
		case <-c.stopCh:
			c.flush()
			return
		case <-c.flushTicker.C:
			c.flush()
		}
	}
}

// flush 刷新缓冲区
const maxBufferedDevices = 2048
const maxBufferedFailures = 256

func (c *Collector) flush() {
	c.bufferMu.Lock()
	if len(c.buffer) == 0 && len(c.counts) == 0 && !c.dropped {
		c.bufferMu.Unlock()
		return
	}
	failures, counts, dropped := c.buffer, c.counts, c.dropped
	c.buffer, c.counts, c.dropped = nil, make(map[string]Stats), false
	c.bufferMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := c.metrics.flush(ctx, counts, failures, c.config.MaxFailures)
	if dropped || err != nil {
		c.logger.WithField("buffer_full", dropped).WithError(err).Warn("device diagnostics batch incomplete")
	}
}

// Stop 停止收集器
func (c *Collector) Stop(timeout time.Duration) {
	if !c.initialized {
		return
	}

	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.flushTicker.Stop()
	})

	select {
	case <-c.doneCh:
		c.logger.Info("diagnostics collector stopped gracefully")
	case <-time.After(timeout):
		c.logger.Warn("diagnostics collector stop timeout")
	}
}

// IsEnabled returns true when the diagnostics collector is initialized and enabled.
// This provides a cheap fast-path check for callers to avoid repeated short-circuit calls.
func (c *Collector) IsEnabled() bool {
	if c == nil {
		return false
	}
	return c.initialized && c.config.Enabled
}

// RecordFailure 记录失败（异步缓冲）
func (c *Collector) RecordFailure(deviceID string, direction Direction, stage Stage, errMsg string) {
	if !c.initialized || !c.config.Enabled {
		return
	}

	if deviceID == "" {
		c.logger.Warn("device_id is empty, skipping failure record")
		return
	}

	record := FailureRecord{
		Timestamp: time.Now(),
		Direction: direction,
		Stage:     stage,
		Error:     errMsg,
	}

	item := failureItem{
		deviceID: deviceID,
		record:   record,
	}

	c.bufferMu.Lock()
	if len(c.buffer) < maxBufferedFailures {
		c.buffer = append(c.buffer, item)
	} else {
		c.dropped = true
	}
	c.bufferMu.Unlock()
}

func (c *Collector) record(deviceID string, delta Stats) {
	if !c.IsEnabled() || deviceID == "" {
		return
	}
	c.bufferMu.Lock()
	defer c.bufferMu.Unlock()
	counts, exists := c.counts[deviceID]
	if !exists && len(c.counts) >= maxBufferedDevices {
		c.dropped = true
		return
	}
	counts.UplinkTotal += delta.UplinkTotal
	counts.UplinkFailed += delta.UplinkFailed
	counts.StorageFailed += delta.StorageFailed
	counts.DownlinkTotal += delta.DownlinkTotal
	counts.DownlinkFailed += delta.DownlinkFailed
	c.counts[deviceID] = counts
}

func (c *Collector) RecordUplinkPoints(deviceID string, count int) {
	if count > 0 {
		c.record(deviceID, Stats{UplinkTotal: int64(count)})
	}
}

func (c *Collector) RecordUplinkFailed(deviceID string, stage Stage, errMsg string) {
	c.record(deviceID, Stats{UplinkFailed: 1})
	c.RecordFailure(deviceID, DirectionUplink, stage, errMsg)
}

func (c *Collector) RecordStorageFailed(deviceID string, errMsg string) {
	c.RecordStorageFailedPoints(deviceID, 1, errMsg)
}

func (c *Collector) RecordStorageFailedPoints(deviceID string, count int, errMsg string) {
	if count > 0 {
		c.record(deviceID, Stats{StorageFailed: int64(count)})
		c.RecordFailure(deviceID, DirectionUplink, StageStorage, errMsg)
	}
}

func (c *Collector) RecordDownlinkTotal(deviceID string) {
	c.record(deviceID, Stats{DownlinkTotal: 1})
}

func (c *Collector) RecordDownlinkFailed(deviceID string, stage Stage, errMsg string) {
	c.record(deviceID, Stats{DownlinkFailed: 1})
	c.RecordFailure(deviceID, DirectionDownlink, stage, errMsg)
}

// RecordDownlinkPublish counts a broker publish attempt and its result together.
func (c *Collector) RecordDownlinkPublish(deviceID string, publishErr error) {
	delta := Stats{DownlinkTotal: 1}
	if publishErr != nil {
		delta.DownlinkFailed = 1
	}
	c.record(deviceID, delta)
	if publishErr != nil {
		c.RecordFailure(deviceID, DirectionDownlink, StagePublish, "broker publish did not confirm success")
	}
}

// GetDiagnostics 获取诊断数据（供 API 使用）
func (c *Collector) GetDiagnostics(deviceID string) (*DiagnosticsResponse, error) {
	if !c.initialized {
		return nil, ErrNotInitialized
	}

	// 获取统计指标
	stats, err := c.metrics.GetStats(deviceID)
	if err != nil {
		return nil, err
	}

	// 获取失败记录
	failures, err := c.metrics.GetFailures(deviceID, c.config.MaxFailures)
	if err != nil {
		return nil, err
	}

	// 计算成功率
	response := &DiagnosticsResponse{
		DeviceID:       deviceID,
		RecentFailures: failures,
		Stats: &StatsResponse{
			Uplink:   calculateMetric(stats.UplinkTotal, max(0, stats.UplinkTotal-stats.UplinkFailed)),
			Downlink: calculateMetric(stats.DownlinkTotal, max(0, stats.DownlinkTotal-stats.DownlinkFailed)),
			Storage:  calculateMetric(stats.UplinkTotal, max(0, stats.UplinkTotal-stats.StorageFailed)),
		},
	}

	return response, nil
}

// calculateMetric 计算指标响应
func calculateMetric(total, success int64) *MetricResponse {
	var rate float64
	if total > 0 {
		rate = float64(success) / float64(total) * 100
	}

	return &MetricResponse{
		SuccessRate: rate,
		Total:       total,
		Success:     success,
	}
}

// 错误定义
var (
	ErrNotInitialized      = fmt.Errorf("diagnostics collector not initialized")
	ErrRedisNotInitialized = fmt.Errorf("redis not initialized")
)
