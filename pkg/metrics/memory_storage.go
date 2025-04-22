package metrics

import (
	"sync"
	"time"
)

const (
	// 默认保留24小时数据
	DefaultRetentionPeriod = 24 * time.Hour
	// 默认每5分钟采集一次数据
	DefaultCollectionInterval = 5 * time.Minute
)

// MemoryStorage 实现基于内存的指标历史数据存储
type MemoryStorage struct {
	sync.RWMutex
	cpuData    []MetricDataPoint // CPU使用率历史数据
	memoryData []MetricDataPoint // 内存使用率历史数据
	diskData   []MetricDataPoint // 磁盘使用率历史数据

	// 当前最新数据
	currentData SystemMetrics

	// 数据保留期限
	retentionPeriod time.Duration
	// 上次清理时间
	lastCleanup time.Time
}

// NewMemoryStorage 创建新的内存存储实例
func NewMemoryStorage() *MemoryStorage {
	storage := &MemoryStorage{
		cpuData:         make([]MetricDataPoint, 0, 288), // 24小时每5分钟一个点 = 24*12 = 288个点
		memoryData:      make([]MetricDataPoint, 0, 288),
		diskData:        make([]MetricDataPoint, 0, 288),
		retentionPeriod: DefaultRetentionPeriod,
		lastCleanup:     time.Now(),
	}

	// 启动定期清理过期数据
	go storage.periodicCleanup()

	return storage
}

// SaveMetrics 保存当前系统指标
func (s *MemoryStorage) SaveMetrics(timestamp time.Time, cpuUsage, memoryUsage, diskUsage float64) error {
	s.Lock()
	defer s.Unlock()

	// 更新当前数据
	s.currentData = SystemMetrics{
		CPUUsage:    cpuUsage,
		MemoryUsage: memoryUsage,
		DiskUsage:   diskUsage,
		Timestamp:   timestamp,
	}

	// 每5分钟保存一个数据点
	if len(s.cpuData) == 0 || time.Since(s.cpuData[len(s.cpuData)-1].Timestamp) >= DefaultCollectionInterval {
		point := MetricDataPoint{
			Timestamp: timestamp,
			Value:     cpuUsage,
		}
		s.cpuData = append(s.cpuData, point)

		point.Value = memoryUsage
		s.memoryData = append(s.memoryData, point)

		point.Value = diskUsage
		s.diskData = append(s.diskData, point)

		// 检查是否需要清理
		if time.Since(s.lastCleanup) > time.Hour {
			s.cleanup()
			s.lastCleanup = time.Now()
		}
	}

	return nil
}

// GetHistoryData 根据指标类型和时间范围获取历史数据
func (s *MemoryStorage) GetHistoryData(metric string, duration time.Duration) ([]MetricDataPoint, error) {
	s.RLock()
	defer s.RUnlock()

	var data []MetricDataPoint
	switch metric {
	case "cpu":
		data = s.cpuData
	case "memory":
		data = s.memoryData
	case "disk":
		data = s.diskData
	default:
		return nil, nil
	}

	// 如果未指定时间范围或时间范围大于保留期，则返回所有数据
	if duration <= 0 || duration >= s.retentionPeriod {
		result := make([]MetricDataPoint, len(data))
		copy(result, data)
		return result, nil
	}

	// 根据时间范围筛选数据
	cutoffTime := time.Now().Add(-duration)
	result := make([]MetricDataPoint, 0, len(data))

	for _, point := range data {
		if point.Timestamp.After(cutoffTime) {
			result = append(result, point)
		}
	}

	return result, nil
}

// GetCurrentData 获取最新的系统指标
func (s *MemoryStorage) GetCurrentData() (*SystemMetrics, error) {
	s.RLock()
	defer s.RUnlock()

	result := s.currentData
	return &result, nil
}

// 清理过期数据
func (s *MemoryStorage) cleanup() {
	cutoffTime := time.Now().Add(-s.retentionPeriod)

	s.cpuData = filterDataPoints(s.cpuData, cutoffTime)
	s.memoryData = filterDataPoints(s.memoryData, cutoffTime)
	s.diskData = filterDataPoints(s.diskData, cutoffTime)
}

// 定期清理过期数据
func (s *MemoryStorage) periodicCleanup() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		s.Lock()
		s.cleanup()
		s.lastCleanup = time.Now()
		s.Unlock()
	}
}

// 过滤数据点，保留指定时间之后的数据
func filterDataPoints(data []MetricDataPoint, cutoffTime time.Time) []MetricDataPoint {
	if len(data) == 0 {
		return data
	}

	index := 0
	for i, point := range data {
		if point.Timestamp.After(cutoffTime) {
			index = i
			break
		}
	}

	if index == 0 && data[0].Timestamp.Before(cutoffTime) {
		return data[:0] // 全部过期
	}

	// 将数据移到前面
	if index > 0 {
		copy(data, data[index:])
		return data[:len(data)-index]
	}

	return data
}
