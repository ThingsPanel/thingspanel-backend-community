package service

import (
	"time"

	"project/pkg/metrics"
)

// SystemMonitor 系统监控服务
type SystemMonitor struct{}

// 全局metrics管理器
var metricsManager *metrics.Metrics

// SetMetricsManager 设置metrics管理器
func SetMetricsManager(m *metrics.Metrics) {
	metricsManager = m
}

// GetCurrentMetrics 获取当前系统指标
func (s *SystemMonitor) GetCurrentMetrics() (*metrics.SystemMetrics, error) {
	if metricsManager == nil {
		return nil, nil
	}
	return metricsManager.GetCurrentMetrics()
}

// GetHistoryData 获取历史数据
func (s *SystemMonitor) GetHistoryData(metricType string, duration time.Duration) ([]metrics.MetricDataPoint, error) {
	if metricsManager == nil {
		return nil, nil
	}
	return metricsManager.GetHistoryData(metricType, duration)
}

// GetCombinedHistoryData 获取组合式历史数据
func (s *SystemMonitor) GetCombinedHistoryData(duration time.Duration) ([]metrics.MetricsTimePoint, error) {
	if metricsManager == nil {
		return nil, nil
	}
	return metricsManager.GetCombinedHistoryData(duration)
}
