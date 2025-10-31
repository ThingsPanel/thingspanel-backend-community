package diagnostics

import "time"

// Direction 消息方向
type Direction string

const (
	DirectionUplink   Direction = "uplink"   // 上行
	DirectionDownlink Direction = "downlink" // 下行
)

// Stage 处理阶段
type Stage string

const (
	StageAdapter   Stage = "adapter"   // 适配器（消息格式验证）
	StageProcessor Stage = "processor" // 处理器（脚本解码/编码）
	StageStorage   Stage = "storage"   // 存储（批量写入）
	StageEncode    Stage = "encode"    // 编码（下行脚本编码）
	StagePublish   Stage = "publish"   // 发布（MQTT 发布）
)

// FailureRecord 失败记录
type FailureRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Direction Direction `json:"direction"`
	Stage     Stage     `json:"stage"`
	Error     string    `json:"error"`
}

// Stats 统计指标
type Stats struct {
	UplinkTotal    int64 `json:"uplink_total"`
	UplinkFailed   int64 `json:"uplink_failed"`
	StorageFailed  int64 `json:"storage_failed"`
	DownlinkTotal  int64 `json:"downlink_total"`
	DownlinkFailed int64 `json:"downlink_failed"`
}

// DiagnosticsResponse API 响应结构
type DiagnosticsResponse struct {
	DeviceID       string           `json:"device_id"`
	Stats          *StatsResponse   `json:"stats"`
	RecentFailures []FailureRecord  `json:"recent_failures"`
}

// StatsResponse 统计响应结构
type StatsResponse struct {
	Uplink  *MetricResponse `json:"uplink"`
	Downlink *MetricResponse `json:"downlink"`
	Storage *MetricResponse `json:"storage"`
}

// MetricResponse 指标响应结构
type MetricResponse struct {
	SuccessRate float64 `json:"success_rate"` // 成功率（百分比）
	Total       int64   `json:"total"`         // 总数
	Success     int64   `json:"success"`       // 成功数
}
