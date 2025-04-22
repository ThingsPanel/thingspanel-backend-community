// pkg/metrics/metrics.go
package metrics

import (
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
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
	DiskUsage       prometheus.Gauge   // 磁盘使用率
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

	// 历史数据存储
	historyStorage HistoryStorage
}

// HistoryStorage 定义历史数据存储接口
type HistoryStorage interface {
	SaveMetrics(timestamp time.Time, cpuUsage, memoryUsage, diskUsage float64) error
	GetHistoryData(metric string, duration time.Duration) ([]MetricDataPoint, error)
	GetCurrentData() (*SystemMetrics, error)
}

// MetricDataPoint 表示一个指标数据点
type MetricDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// SystemMetrics 系统指标当前值
type SystemMetrics struct {
	CPUUsage    float64   `json:"cpu_usage"`    // CPU使用率百分比
	MemoryUsage float64   `json:"memory_usage"` // 内存使用率百分比
	DiskUsage   float64   `json:"disk_usage"`   // 磁盘使用率百分比
	Timestamp   time.Time `json:"timestamp"`    // 时间戳
}

// MetricsTimePoint 表示某一时间点的所有指标数据
type MetricsTimePoint struct {
	Timestamp   time.Time `json:"timestamp"` // 时间戳
	CPUUsage    float64   `json:"cpu"`       // CPU使用率
	MemoryUsage float64   `json:"memory"`    // 内存使用率
	DiskUsage   float64   `json:"disk"`      // 磁盘使用率
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

		DiskUsage: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "disk_usage_percent",
				Help:      "Disk usage percentage",
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

// SetHistoryStorage 设置历史数据存储实现
func (m *Metrics) SetHistoryStorage(storage HistoryStorage) {
	m.historyStorage = storage
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

			// 进程 CPU 使用率 - 使用更可靠的方法
			cpuPercent := 0.0

			// 方法1: 使用process.Percent计算CPU使用率
			percent, err := process.Percent(time.Second)
			if err == nil && percent > 0 {
				cpuPercent = percent
			} else {
				// 方法2: 使用整个系统CPU使用率作为备选
				cpuStat, err := cpu.Percent(time.Second, false)
				if err == nil && len(cpuStat) > 0 {
					cpuPercent = cpuStat[0]
				}
			}

			m.CPUUsage.Set(cpuPercent)

			// 磁盘使用率
			var diskUsagePercent float64
			// 在Windows上使用C盘，在其他系统上使用根目录
			diskPath := "/"
			if runtime.GOOS == "windows" {
				diskPath = "C:\\"
			}

			diskStat, err := disk.Usage(diskPath)
			if err == nil {
				diskUsagePercent = diskStat.UsedPercent
				m.DiskUsage.Set(diskUsagePercent)
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

			// 存储历史数据
			if m.historyStorage != nil {
				// 获取当前内存使用率百分比
				var memoryUsagePercent float64
				if memStats.Sys > 0 {
					memoryUsagePercent = float64(memStats.Alloc) / float64(memStats.Sys) * 100
				}

				// 保存指标历史记录
				err := m.historyStorage.SaveMetrics(
					time.Now(),
					cpuPercent,
					memoryUsagePercent,
					diskUsagePercent,
				)
				if err != nil {
					logrus.Warnf("Failed to save metrics history: %v", err)
				}
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

// GetHistoryData 获取历史数据
func (m *Metrics) GetHistoryData(metric string, duration time.Duration) ([]MetricDataPoint, error) {
	if m.historyStorage == nil {
		return nil, nil
	}
	return m.historyStorage.GetHistoryData(metric, duration)
}

// GetCurrentMetrics 获取当前系统指标
func (m *Metrics) GetCurrentMetrics() (*SystemMetrics, error) {
	if m.historyStorage == nil {
		return nil, nil
	}
	return m.historyStorage.GetCurrentData()
}

// GetCombinedHistoryData 获取组合式历史数据，每个时间点包含所有指标
func (m *Metrics) GetCombinedHistoryData(duration time.Duration) ([]MetricsTimePoint, error) {
	if m.historyStorage == nil {
		return nil, nil
	}

	// 获取各项指标的历史数据
	cpuData, err := m.historyStorage.GetHistoryData("cpu", duration)
	if err != nil {
		return nil, err
	}

	memoryData, err := m.historyStorage.GetHistoryData("memory", duration)
	if err != nil {
		return nil, err
	}

	diskData, err := m.historyStorage.GetHistoryData("disk", duration)
	if err != nil {
		return nil, err
	}

	// 构建时间点映射
	timeMap := make(map[time.Time]MetricsTimePoint)

	// 添加CPU数据
	for _, point := range cpuData {
		timeMap[point.Timestamp] = MetricsTimePoint{
			Timestamp: point.Timestamp,
			CPUUsage:  point.Value,
		}
	}

	// 添加内存数据
	for _, point := range memoryData {
		if tp, exists := timeMap[point.Timestamp]; exists {
			tp.MemoryUsage = point.Value
			timeMap[point.Timestamp] = tp
		} else {
			timeMap[point.Timestamp] = MetricsTimePoint{
				Timestamp:   point.Timestamp,
				MemoryUsage: point.Value,
			}
		}
	}

	// 添加磁盘数据
	for _, point := range diskData {
		if tp, exists := timeMap[point.Timestamp]; exists {
			tp.DiskUsage = point.Value
			timeMap[point.Timestamp] = tp
		} else {
			timeMap[point.Timestamp] = MetricsTimePoint{
				Timestamp: point.Timestamp,
				DiskUsage: point.Value,
			}
		}
	}

	// 将map转换为slice并按时间排序
	result := make([]MetricsTimePoint, 0, len(timeMap))
	for _, point := range timeMap {
		result = append(result, point)
	}

	// 按时间排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result, nil
}
