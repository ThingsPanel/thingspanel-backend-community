// pkg/metrics/metrics.go
package metrics

import (
	"log"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/shirou/gopsutil/cpu"
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

	// 新增系统资源指标
	MemoryUsage     prometheus.Gauge   // 当前内存使用量
	MemoryAllocated prometheus.Gauge   // 已分配内存
	GCPauseLatency  prometheus.Summary // GC暂停时间
	NumGoroutines   prometheus.Gauge   // goroutine数量
	CPUUsage        prometheus.Gauge   // CPU使用率
}

// NewMetrics 创建核心指标收集器
func NewMetrics(namespace string) *Metrics {
	m := &Metrics{
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
		// 新增系统资源指标
		MemoryUsage: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "memory_usage_bytes",
				Help:      "Current memory usage in bytes",
			},
		),

		MemoryAllocated: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "memory_allocated_bytes",
				Help:      "Total allocated memory in bytes",
			},
		),

		GCPauseLatency: promauto.NewSummary(
			prometheus.SummaryOpts{
				Namespace: namespace,
				Name:      "gc_pause_latency_seconds",
				Help:      "GC pause latency distribution",
				Objectives: map[float64]float64{
					0.5:  0.05,  // 50th percentile with 5% error
					0.9:  0.01,  // 90th percentile with 1% error
					0.99: 0.001, // 99th percentile with 0.1% error
				},
			},
		),

		NumGoroutines: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "goroutines_total",
				Help:      "Current number of goroutines",
			},
		),

		CPUUsage: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "cpu_usage_percent",
				Help:      "Current CPU usage percentage",
			},
		),
	}

	// 启动资源监控协程
	// 启动资源监控协程
	go m.collectSystemMetrics()
	return m
}

// collectSystemMetrics 持续收集系统指标
func (m *Metrics) collectSystemMetrics() {
	// 创建定时器，每5秒收集一次
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// 收集内存统计
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		m.MemoryUsage.Set(float64(memStats.Alloc))
		m.MemoryAllocated.Set(float64(memStats.TotalAlloc))
		m.GCPauseLatency.Observe(float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e9)
		m.NumGoroutines.Set(float64(runtime.NumGoroutine()))

		// 收集CPU使用率
		percentage, err := cpu.Percent(time.Second, false)
		if err == nil && len(percentage) > 0 {
			m.CPUUsage.Set(percentage[0]) // percentage[0] 是总体CPU使用率
		} else {
			// 记录错误，但不影响其他指标的收集
			log.Printf("Failed to collect CPU usage: %v", err)
		}
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
