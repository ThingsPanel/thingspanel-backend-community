package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// GatewayMessageBuilder 网关设备消息构建器
type GatewayMessageBuilder struct {
	topology interface{} // 拓扑结构（用于构建嵌套数据）
}

// NewGatewayMessageBuilder 创建网关设备消息构建器
func NewGatewayMessageBuilder(topology interface{}) *GatewayMessageBuilder {
	return &GatewayMessageBuilder{
		topology: topology,
	}
}

// BuildTelemetry 构建遥测数据报文(嵌套JSON格式)
// data 参数应该是一个 map，包含 gateway_data, sub_device_data, sub_gateway_data
func (b *GatewayMessageBuilder) BuildTelemetry(data interface{}) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gateway telemetry data: %w", err)
	}
	return payload, nil
}

// BuildAttribute 构建属性数据报文(嵌套JSON格式)
func (b *GatewayMessageBuilder) BuildAttribute(data interface{}) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gateway attribute data: %w", err)
	}
	return payload, nil
}

// BuildEvent 构建事件数据报文(嵌套JSON格式)
// params 参数应该是完整的嵌套数据结构（包含 gateway_data, sub_device_data, sub_gateway_data）
// 与 BuildTelemetry 和 BuildAttribute 保持一致
func (b *GatewayMessageBuilder) BuildEvent(method string, params interface{}) ([]byte, error) {
	// 直接序列化传入的嵌套数据
	// params 已经是完整的网关事件结构，不需要再包装
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gateway event data: %w", err)
	}
	return payload, nil
}

// BuildResponse 构建响应报文(扁平格式)
func (b *GatewayMessageBuilder) BuildResponse(success bool, method string) ([]byte, error) {
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

// BuildNestedTelemetry 构建嵌套的遥测数据（辅助方法）
func BuildNestedTelemetry(gatewayData, subDeviceData, subGatewayData map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	if gatewayData != nil {
		result["gateway_data"] = gatewayData
	}

	if subDeviceData != nil {
		result["sub_device_data"] = subDeviceData
	}

	if subGatewayData != nil {
		result["sub_gateway_data"] = subGatewayData
	}

	return result
}

// BuildNestedAttributes 构建嵌套的属性数据（辅助方法）
func BuildNestedAttributes(gatewayData, subDeviceData, subGatewayData map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	if gatewayData != nil {
		result["gateway_data"] = gatewayData
	}

	if subDeviceData != nil {
		result["sub_device_data"] = subDeviceData
	}

	if subGatewayData != nil {
		result["sub_gateway_data"] = subGatewayData
	}

	return result
}

// BuildNestedEvents 构建嵌套的事件数据（辅助方法）
func BuildNestedEvents(gatewayEvent, subDeviceEvents, subGatewayEvents map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	if gatewayEvent != nil {
		result["gateway_data"] = gatewayEvent
	}

	if subDeviceEvents != nil {
		result["sub_device_data"] = subDeviceEvents
	}

	if subGatewayEvents != nil {
		result["sub_gateway_data"] = subGatewayEvents
	}

	return result
}
