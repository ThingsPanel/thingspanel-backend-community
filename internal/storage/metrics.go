package storage

import (
	"sync/atomic"
	"time"
)

// Metrics 监控指标
type Metrics struct {
	// 遥测数据
	TelemetryReceived          int64     // 接收的遥测消息数
	TelemetryWritten           int64     // 成功写入的遥测数据点数
	TelemetryFailed            int64     // 写入失败的遥测数据点数
	TelemetryDuplicatesInBatch int64     // 批次内重复数
	TelemetryBatchCount        int64     // flush次数
	TelemetryAvgBatch          float64   // 平均批次大小
	TelemetryLastFlush         time.Time // 最后flush时间

	// 属性数据
	AttributeWritten int64 // 成功写入的属性数
	AttributeFailed  int64 // 写入失败的属性数

	// 事件数据
	EventWritten int64 // 成功写入的事件数
	EventFailed  int64 // 写入失败的事件数
}

// metricsCollector 内部指标收集器
type metricsCollector struct {
	telemetryReceived          int64
	telemetryWritten           int64
	telemetryFailed            int64
	telemetryDuplicatesInBatch int64
	telemetryBatchCount        int64
	telemetryTotalBatchSize    int64
	telemetryLastFlush         int64 // Unix nano

	attributeWritten int64
	attributeFailed  int64

	eventWritten int64
	eventFailed  int64
}

func newMetricsCollector() *metricsCollector {
	return &metricsCollector{}
}

// 遥测数据指标

func (m *metricsCollector) incTelemetryReceived() {
	atomic.AddInt64(&m.telemetryReceived, 1)
}

func (m *metricsCollector) addTelemetryWritten(count int64) {
	atomic.AddInt64(&m.telemetryWritten, count)
}

func (m *metricsCollector) addTelemetryFailed(count int64) {
	atomic.AddInt64(&m.telemetryFailed, count)
}

func (m *metricsCollector) addTelemetryDuplicates(count int64) {
	atomic.AddInt64(&m.telemetryDuplicatesInBatch, count)
}

func (m *metricsCollector) recordTelemetryBatch(batchSize int) {
	atomic.AddInt64(&m.telemetryBatchCount, 1)
	atomic.AddInt64(&m.telemetryTotalBatchSize, int64(batchSize))
	atomic.StoreInt64(&m.telemetryLastFlush, time.Now().UnixNano())
}

// 属性数据指标

func (m *metricsCollector) incAttributeWritten() {
	atomic.AddInt64(&m.attributeWritten, 1)
}

func (m *metricsCollector) incAttributeFailed() {
	atomic.AddInt64(&m.attributeFailed, 1)
}

// 事件数据指标

func (m *metricsCollector) incEventWritten() {
	atomic.AddInt64(&m.eventWritten, 1)
}

func (m *metricsCollector) incEventFailed() {
	atomic.AddInt64(&m.eventFailed, 1)
}

// GetMetrics 获取当前指标快照
func (m *metricsCollector) GetMetrics() Metrics {
	batchCount := atomic.LoadInt64(&m.telemetryBatchCount)
	totalBatchSize := atomic.LoadInt64(&m.telemetryTotalBatchSize)

	var avgBatch float64
	if batchCount > 0 {
		avgBatch = float64(totalBatchSize) / float64(batchCount)
	}

	lastFlushNano := atomic.LoadInt64(&m.telemetryLastFlush)
	var lastFlush time.Time
	if lastFlushNano > 0 {
		lastFlush = time.Unix(0, lastFlushNano)
	}

	return Metrics{
		TelemetryReceived:          atomic.LoadInt64(&m.telemetryReceived),
		TelemetryWritten:           atomic.LoadInt64(&m.telemetryWritten),
		TelemetryFailed:            atomic.LoadInt64(&m.telemetryFailed),
		TelemetryDuplicatesInBatch: atomic.LoadInt64(&m.telemetryDuplicatesInBatch),
		TelemetryBatchCount:        batchCount,
		TelemetryAvgBatch:          avgBatch,
		TelemetryLastFlush:         lastFlush,
		AttributeWritten:           atomic.LoadInt64(&m.attributeWritten),
		AttributeFailed:            atomic.LoadInt64(&m.attributeFailed),
		EventWritten:               atomic.LoadInt64(&m.eventWritten),
		EventFailed:                atomic.LoadInt64(&m.eventFailed),
	}
}
