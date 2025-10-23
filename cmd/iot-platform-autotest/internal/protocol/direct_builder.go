package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// DirectMessageBuilder 直连设备消息构建器
type DirectMessageBuilder struct{}

// NewDirectMessageBuilder 创建直连设备消息构建器
func NewDirectMessageBuilder() *DirectMessageBuilder {
	return &DirectMessageBuilder{}
}

// BuildTelemetry 构建遥测数据报文(扁平JSON格式)
func (b *DirectMessageBuilder) BuildTelemetry(data interface{}) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal telemetry data: %w", err)
	}
	return payload, nil
}

// BuildAttribute 构建属性数据报文(扁平JSON格式)
func (b *DirectMessageBuilder) BuildAttribute(data interface{}) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal attribute data: %w", err)
	}
	return payload, nil
}

// BuildEvent 构建事件数据报文
func (b *DirectMessageBuilder) BuildEvent(method string, params interface{}) ([]byte, error) {
	eventData := map[string]interface{}{
		"method": method,
		"params": params,
	}

	payload, err := json.Marshal(eventData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}
	return payload, nil
}

// BuildResponse 构建响应报文
func (b *DirectMessageBuilder) BuildResponse(success bool, method string) ([]byte, error) {
	response := map[string]interface{}{
		"result":  0,
		"message": "success",
		"ts":      time.Now().Unix(),
	}

	if !success {
		response["result"] = 1
		response["message"] = "failed"
	}

	if method != "" {
		response["method"] = method
	}

	payload, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return payload, nil
}
