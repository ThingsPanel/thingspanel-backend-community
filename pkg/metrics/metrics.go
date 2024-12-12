// pkg/metrics/metrics.go
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics 封装核心监控指标
type Metrics struct {
	// API 调用相关
	APIRequestTotal *prometheus.CounterVec  // API 总调用次数
	APIErrorTotal   *prometheus.CounterVec  // API 错误次数(按错误类型)
	APILatency      prometheus.HistogramVec // API 延迟分布

	// 业务错误相关
	BusinessErrorTotal *prometheus.CounterVec // 业务错误次数(按模块)
	CriticalErrorTotal prometheus.Counter     // 严重错误次数

	// 性能相关
	SlowRequestTotal   *prometheus.CounterVec // 慢请求统计(>1s)
	LargeResponseTotal *prometheus.CounterVec // 大响应统计(>1MB)
}

// NewMetrics 创建核心指标收集器
func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		// API 调用监控
		APIRequestTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "api_requests_total",
				Help:      "Total number of API requests by path and method",
			},
			[]string{"path", "method"},
		),

		APIErrorTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "api_errors_total",
				Help:      "Total number of API errors by error type",
			},
			[]string{"type"}, // system/business/panic
		),

		APILatency: *promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "api_latency_seconds",
				Help:      "API latency in seconds",
				Buckets:   []float64{0.1, 0.3, 0.5, 1.0, 2.0, 5.0}, // 关注较大延迟
			},
			[]string{"path"},
		),

		// 业务错误监控
		BusinessErrorTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "business_errors_total",
				Help:      "Total number of business errors by module",
			},
			[]string{"module", "code"}, // 业务模块和错误码
		),

		CriticalErrorTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "critical_errors_total",
				Help:      "Total number of critical errors that need immediate attention",
			},
		),

		// 性能监控
		SlowRequestTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "slow_requests_total",
				Help:      "Total number of slow requests (>1s) by path",
			},
			[]string{"path"},
		),

		LargeResponseTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "large_responses_total",
				Help:      "Total number of large responses (>1MB) by path",
			},
			[]string{"path"},
		),
	}
}

// RecordAPIRequest 记录 API 请求
func (m *Metrics) RecordAPIRequest(path, method string) {
	m.APIRequestTotal.WithLabelValues(path, method).Inc()
}

// RecordAPIError 记录 API 错误
func (m *Metrics) RecordAPIError(errorType string) {
	m.APIErrorTotal.WithLabelValues(errorType).Inc()
}

// RecordAPILatency 记录 API 延迟
func (m *Metrics) RecordAPILatency(path string, duration float64) {
	m.APILatency.WithLabelValues(path).Observe(duration)
	// 记录慢请求
	if duration > 1.0 {
		m.SlowRequestTotal.WithLabelValues(path).Inc()
	}
}

// RecordBusinessError 记录业务错误
func (m *Metrics) RecordBusinessError(module, code string) {
	m.BusinessErrorTotal.WithLabelValues(module, code).Inc()
}

// RecordCriticalError 记录严重错误
func (m *Metrics) RecordCriticalError() {
	m.CriticalErrorTotal.Inc()
}

// RecordResponseSize 记录响应大小
func (m *Metrics) RecordResponseSize(path string, sizeBytes float64) {
	if sizeBytes > 1024*1024 { // >1MB
		m.LargeResponseTotal.WithLabelValues(path).Inc()
	}
}
