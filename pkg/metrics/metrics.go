// pkg/metrics/metrics.go
package metrics

import (
	"os"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/shirou/gopsutil/process"
	"github.com/sirupsen/logrus"
)

// Metrics 封装所有监控指标
type Metrics struct {
	// 系统资源相关
	MemoryUsage     prometheus.Gauge   // 当前内存使用
	MemoryAllocated prometheus.Gauge   // 已分配内存
	MemoryObjects   prometheus.Gauge   // 对象数量
	CPUUsage        prometheus.Gauge   // CPU使用率
	GoroutinesTotal prometheus.Gauge   // goroutine数量
	GCPauseTotal    prometheus.Gauge   // GC暂停时间累计
	GCRuns          prometheus.Counter // GC运行次数

	// API 调用相关
	APIRequestTotal *prometheus.CounterVec   // API 总调用次数
	APIErrorTotal   *prometheus.CounterVec   // API 错误次数(按错误类型)
	APILatency      *prometheus.HistogramVec // API 延迟分布

	// 业务错误相关
	BusinessErrorTotal *prometheus.CounterVec // 业务错误次数(按模块)
	CriticalErrorTotal prometheus.Counter     // 严重错误次数

	// 性能相关
	SlowRequestTotal   *prometheus.CounterVec // 慢请求统计(>1s)
	LargeResponseTotal *prometheus.CounterVec // 大响应统计(>1MB)
}

// NewMetrics 创建监控指标收集器
func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		// 系统资源监控
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
				Help:      "Total memory allocated in bytes",
			},
		),

		MemoryObjects: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "memory_objects",
				Help:      "Number of allocated objects",
			},
		),

		CPUUsage: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "cpu_usage_percent",
				Help:      "CPU usage percentage",
			},
		),

		GoroutinesTotal: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "goroutines_total",
				Help:      "Total number of goroutines",
			},
		),

		GCPauseTotal: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "gc_pause_total_seconds",
				Help:      "Total GC pause time in seconds",
			},
		),

		GCRuns: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "gc_runs_total",
				Help:      "Total number of completed GC cycles",
			},
		),

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

		APILatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "api_latency_seconds",
				Help:      "API latency in seconds",
				Buckets:   []float64{0.1, 0.3, 0.5, 1.0, 2.0, 5.0},
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
			[]string{"module", "code"},
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

func (m *Metrics) StartMetricsCollection(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var lastPauseNs uint64
		var lastNumGC uint32

		// 获取进程 ID
		pid := os.Getpid()
		process, err := process.NewProcess(int32(pid))
		if err != nil {
			logrus.Warnf("Failed to get process: %v", err)
			return
		}

		for range ticker.C {
			// 内存统计
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			m.MemoryUsage.Set(float64(memStats.Alloc))
			m.MemoryAllocated.Set(float64(memStats.Sys))
			m.MemoryObjects.Set(float64(memStats.HeapObjects))

			// 进程 CPU 使用率
			cpuPercent, err := process.Percent(0)
			if err == nil {
				m.CPUUsage.Set(cpuPercent)
			}

			// Goroutine 统计
			m.GoroutinesTotal.Set(float64(runtime.NumGoroutine()))

			// GC 统计
			if memStats.NumGC > lastNumGC {
				diff := memStats.NumGC - lastNumGC
				m.GCRuns.Add(float64(diff))
				lastNumGC = memStats.NumGC
			}

			if pauseNs := memStats.PauseTotalNs; pauseNs > lastPauseNs {
				pauseDiff := float64(pauseNs - lastPauseNs)
				m.GCPauseTotal.Add(pauseDiff / 1e9)
				lastPauseNs = pauseNs
			}
		}
	}()
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
