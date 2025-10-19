package service

// StatusPublisher 状态发布接口
// 由 HeartbeatMonitor 使用，在 app 层由 Flow Bus 实现
// 接口定义在使用方（service），避免循环依赖
type StatusPublisher interface {
	// PublishStatusOffline 发布设备离线状态
	// deviceID: 设备ID
	// source: 离线来源（如 "heartbeat_expired", "timeout_expired"）
	PublishStatusOffline(deviceID, source string) error
}
