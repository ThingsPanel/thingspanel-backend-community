package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	"project/internal/dal"
	"project/internal/model"
)

// HeartbeatConfig 心跳配置
type HeartbeatConfig struct {
	Heartbeat     int `json:"heartbeat"`      // 心跳间隔(秒)
	OnlineTimeout int `json:"online_timeout"` // 在线超时(秒)
}

// HeartbeatService 心跳服务
type HeartbeatService struct {
	redis  *redis.Client
	logger *logrus.Logger
}

// NewHeartbeatService 创建心跳服务实例
func NewHeartbeatService(redis *redis.Client, logger *logrus.Logger) *HeartbeatService {
	return &HeartbeatService{
		redis:  redis,
		logger: logger,
	}
}

// GetConfig 获取设备的心跳配置
func (s *HeartbeatService) GetConfig(device *model.Device) (*HeartbeatConfig, error) {
	// 没有配置ID,返回nil表示无配置
	if device.DeviceConfigID == nil {
		return nil, nil
	}

	// 从数据库获取设备配置
	deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
	if err != nil {
		return nil, fmt.Errorf("failed to get device config: %w", err)
	}

	// other_config 为空
	if deviceConfig.OtherConfig == nil {
		return nil, nil
	}

	// 解析 other_config JSON
	var config HeartbeatConfig
	if err := json.Unmarshal([]byte(*deviceConfig.OtherConfig), &config); err != nil {
		return nil, fmt.Errorf("failed to parse other_config: %w", err)
	}

	// 如果两个配置都为0,返回nil表示无配置
	if config.Heartbeat == 0 && config.OnlineTimeout == 0 {
		return nil, nil
	}

	return &config, nil
}

// SetHeartbeat 设置心跳 key
func (s *HeartbeatService) SetHeartbeat(deviceID string, interval int) error {
	if interval <= 0 {
		return fmt.Errorf("invalid heartbeat interval: %d", interval)
	}

	key := fmt.Sprintf("device:%s:heartbeat", deviceID)
	ctx := context.Background()

	if err := s.redis.Set(ctx, key, 1, time.Duration(interval)*time.Second).Err(); err != nil {
		return fmt.Errorf("failed to set heartbeat key: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"device_id": deviceID,
		"interval":  interval,
		"key":       key,
	}).Debug("Heartbeat key set")

	return nil
}

// SetTimeout 设置超时 key
func (s *HeartbeatService) SetTimeout(deviceID string, timeout int) error {
	if timeout <= 0 {
		return fmt.Errorf("invalid timeout: %d", timeout)
	}

	key := fmt.Sprintf("device:%s:timeout", deviceID)
	ctx := context.Background()

	if err := s.redis.Set(ctx, key, 1, time.Duration(timeout)*time.Second).Err(); err != nil {
		return fmt.Errorf("failed to set timeout key: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"device_id": deviceID,
		"timeout":   timeout,
		"key":       key,
	}).Debug("Timeout key set")

	return nil
}

// RefreshHeartbeat 刷新心跳(根据配置自动选择heartbeat或timeout)
func (s *HeartbeatService) RefreshHeartbeat(device *model.Device, config *HeartbeatConfig) error {
	if config == nil {
		return nil
	}

	// 优先级: heartbeat > online_timeout
	if config.Heartbeat > 0 {
		return s.SetHeartbeat(device.ID, config.Heartbeat)
	} else if config.OnlineTimeout > 0 {
		return s.SetTimeout(device.ID, config.OnlineTimeout)
	}

	return nil
}

// DeleteHeartbeatKey 删除心跳key(用于设备删除等场景)
func (s *HeartbeatService) DeleteHeartbeatKey(deviceID string) error {
	ctx := context.Background()

	// 删除两种可能的key
	heartbeatKey := fmt.Sprintf("device:%s:heartbeat", deviceID)
	timeoutKey := fmt.Sprintf("device:%s:timeout", deviceID)

	if err := s.redis.Del(ctx, heartbeatKey, timeoutKey).Err(); err != nil {
		return fmt.Errorf("failed to delete heartbeat keys: %w", err)
	}

	s.logger.WithField("device_id", deviceID).Debug("Heartbeat keys deleted")
	return nil
}

// DeleteTimeoutKey 删除超时key
func (s *HeartbeatService) DeleteTimeoutKey(deviceID string) error {
	ctx := context.Background()
	timeoutKey := fmt.Sprintf("device:%s:timeout", deviceID)
	if err := s.redis.Del(ctx, timeoutKey).Err(); err != nil {
		return fmt.Errorf("failed to delete timeout key: %w", err)
	}
	return nil
}
