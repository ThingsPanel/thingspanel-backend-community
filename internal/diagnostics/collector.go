package diagnostics

import (
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
	BatchFlushSize     int           // 批量刷新大小
	BatchFlushInterval time.Duration // 批量刷新间隔
}

// Collector 诊断收集器（单例，异步缓冲）
type Collector struct {
	metrics *Metrics
	config  Config
	logger  *logrus.Logger

	// 异步缓冲
	buffer      []failureItem
	bufferMu    sync.Mutex
	stopCh      chan struct{}
	doneCh      chan struct{}
	flushTicker *time.Ticker

	// 初始化标志
	initialized bool
	initOnce    sync.Once
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
		BatchFlushSize:     10,
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
		if config.BatchFlushInterval > 0 {
			c.flushTicker = time.NewTicker(config.BatchFlushInterval)
			go c.run()
		}

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
func (c *Collector) flush() {
	c.bufferMu.Lock()
	if len(c.buffer) == 0 {
		c.bufferMu.Unlock()
		return
	}

	// 取出当前批次
	batch := c.buffer
	c.buffer = make([]failureItem, 0, c.config.BatchFlushSize)
	c.bufferMu.Unlock()

	// 批量写入 Redis
	for _, item := range batch {
		if err := c.metrics.AddFailure(item.deviceID, item.record, c.config.MaxFailures); err != nil {
			c.logger.WithFields(logrus.Fields{
				"device_id": item.deviceID,
				"error":     err,
			}).Error("failed to add failure record")
		}
	}
}

// Stop 停止收集器
func (c *Collector) Stop(timeout time.Duration) {
	if !c.initialized {
		return
	}

	close(c.stopCh)

	if c.flushTicker != nil {
		c.flushTicker.Stop()
	}

	select {
	case <-c.doneCh:
		c.logger.Info("diagnostics collector stopped gracefully")
	case <-time.After(timeout):
		c.logger.Warn("diagnostics collector stop timeout")
	}
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

	// 加入缓冲区
	c.bufferMu.Lock()
	c.buffer = append(c.buffer, item)
	shouldFlush := len(c.buffer) >= c.config.BatchFlushSize
	c.bufferMu.Unlock()

	// 如果缓冲区满了，立即刷新
	if shouldFlush {
		c.flush()
	}
}

// RecordUplinkTotal 记录上行消息总数
func (c *Collector) RecordUplinkTotal(deviceID string) {
	if !c.initialized || !c.config.Enabled {
		return
	}
	if deviceID == "" {
		return
	}
	if err := c.metrics.IncrementUplinkTotal(deviceID); err != nil {
		c.logger.WithFields(logrus.Fields{
			"device_id": deviceID,
			"error":     err,
		}).Error("failed to increment uplink total")
	}
}

// RecordUplinkFailed 记录上行处理失败
func (c *Collector) RecordUplinkFailed(deviceID string, stage Stage, errMsg string) {
	if !c.initialized || !c.config.Enabled {
		return
	}
	if deviceID == "" {
		return
	}

	// 更新统计
	if err := c.metrics.IncrementUplinkFailed(deviceID); err != nil {
		c.logger.WithFields(logrus.Fields{
			"device_id": deviceID,
			"error":     err,
		}).Error("failed to increment uplink failed")
	}

	// 记录失败详情
	c.RecordFailure(deviceID, DirectionUplink, stage, errMsg)
}

// RecordStorageFailed 记录存储失败
func (c *Collector) RecordStorageFailed(deviceID string, errMsg string) {
	if !c.initialized || !c.config.Enabled {
		return
	}
	if deviceID == "" {
		return
	}

	// 更新统计
	if err := c.metrics.IncrementStorageFailed(deviceID); err != nil {
		c.logger.WithFields(logrus.Fields{
			"device_id": deviceID,
			"error":     err,
		}).Error("failed to increment storage failed")
	}

	// 记录失败详情
	c.RecordFailure(deviceID, DirectionUplink, StageStorage, errMsg)
}

// RecordDownlinkTotal 记录下行指令总数
func (c *Collector) RecordDownlinkTotal(deviceID string) {
	if !c.initialized || !c.config.Enabled {
		return
	}
	if deviceID == "" {
		return
	}
	if err := c.metrics.IncrementDownlinkTotal(deviceID); err != nil {
		c.logger.WithFields(logrus.Fields{
			"device_id": deviceID,
			"error":     err,
		}).Error("failed to increment downlink total")
	}
}

// RecordDownlinkFailed 记录下行失败
func (c *Collector) RecordDownlinkFailed(deviceID string, stage Stage, errMsg string) {
	if !c.initialized || !c.config.Enabled {
		return
	}
	if deviceID == "" {
		return
	}

	// 更新统计
	if err := c.metrics.IncrementDownlinkFailed(deviceID); err != nil {
		c.logger.WithFields(logrus.Fields{
			"device_id": deviceID,
			"error":     err,
		}).Error("failed to increment downlink failed")
	}

	// 记录失败详情
	c.RecordFailure(deviceID, DirectionDownlink, stage, errMsg)
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
			Uplink:   calculateMetric(stats.UplinkTotal, stats.UplinkTotal-stats.UplinkFailed),
			Downlink: calculateMetric(stats.DownlinkTotal, stats.DownlinkTotal-stats.DownlinkFailed),
			Storage:  calculateMetric(stats.UplinkTotal, stats.UplinkTotal-stats.StorageFailed),
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
