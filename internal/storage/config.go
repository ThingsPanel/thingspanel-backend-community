package storage

import "time"

// Config 存储层配置
type Config struct {
	// 输入channel缓冲区大小
	ChannelBufferSize int

	// 遥测数据批量大小
	TelemetryBatchSize int

	// 遥测数据flush间隔(毫秒)，0表示关闭定时flush
	TelemetryFlushInterval int

	// 是否启用Prometheus监控
	EnableMetrics bool

	// HistoryMode selects how telemetry history reaches durable storage:
	// "" or "postgres" writes it inside the relational transaction (default);
	// "intent" records durable intents and replays them into HistorySink.
	HistoryMode string
	// HistoryGeneration is the idempotency generation stamped on new intents.
	HistoryGeneration int64
	// HistorySink receives replayed intents. Nil selects the PostgreSQL history table.
	HistorySink HistorySink
	// HistoryReplayIntervalMillis is the idle poll interval; 0 means 500.
	HistoryReplayIntervalMillis int
	// HistoryReplayBatch is the number of intents claimed per replay; 0 means 200.
	HistoryReplayBatch int
	// HistoryReplayLeaseSeconds is the claim lease; 0 means 60.
	HistoryReplayLeaseSeconds int
}

const (
	HistoryModePostgres = "postgres"
	HistoryModeIntent   = "intent"
)

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		ChannelBufferSize:      10000,
		TelemetryBatchSize:     500,
		TelemetryFlushInterval: 1000, // 1秒
		EnableMetrics:          true,
	}
}

// GetFlushDuration 获取flush间隔时长
func (c Config) GetFlushDuration() time.Duration {
	if c.TelemetryFlushInterval <= 0 {
		return 0 // 关闭定时flush
	}
	return time.Duration(c.TelemetryFlushInterval) * time.Millisecond
}
