package utils

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// ValidateTelemetryValue 验证遥测数据值
func ValidateTelemetryValue(expected, actual interface{}) error {
	switch exp := expected.(type) {
	case bool:
		act, ok := actual.(*bool)
		if !ok || act == nil {
			return fmt.Errorf("expected bool value, got %T", actual)
		}
		if *act != exp {
			return fmt.Errorf("value mismatch: expected %v, got %v", exp, *act)
		}
	case float64:
		act, ok := actual.(*float64)
		if !ok || act == nil {
			return fmt.Errorf("expected number value, got %T", actual)
		}
		if math.Abs(*act-exp) > 0.0001 {
			return fmt.Errorf("value mismatch: expected %v, got %v", exp, *act)
		}
	case string:
		act, ok := actual.(*string)
		if !ok || act == nil {
			return fmt.Errorf("expected string value, got %T", actual)
		}
		if *act != exp {
			return fmt.Errorf("value mismatch: expected %v, got %v", exp, *act)
		}
	default:
		return fmt.Errorf("unsupported type: %T", expected)
	}
	return nil
}

// ValidateTelemetryData 验证遥测数据
func ValidateTelemetryData(expectedData map[string]interface{}, actualKey string, actualBoolV *bool, actualNumberV *float64, actualStringV *string) error {
	expected, ok := expectedData[actualKey]
	if !ok {
		return fmt.Errorf("unexpected key: %s", actualKey)
	}

	switch exp := expected.(type) {
	case bool:
		return ValidateTelemetryValue(exp, actualBoolV)
	case float64:
		return ValidateTelemetryValue(exp, actualNumberV)
	case string:
		return ValidateTelemetryValue(exp, actualStringV)
	default:
		// 尝试作为数字处理
		if num, ok := expected.(int); ok {
			return ValidateTelemetryValue(float64(num), actualNumberV)
		}
		return fmt.Errorf("unsupported expected type: %T", expected)
	}
}

// ValidateAttributeData 验证属性数据
func ValidateAttributeData(expectedData map[string]interface{}, actualKey string, actualBoolV *bool, actualNumberV *float64, actualStringV *string) error {
	return ValidateTelemetryData(expectedData, actualKey, actualBoolV, actualNumberV, actualStringV)
}

// ValidateEventData 验证事件数据
func ValidateEventData(expectedMethod string, expectedParams map[string]interface{}, actualData string) error {
	var actual map[string]interface{}
	if err := json.Unmarshal([]byte(actualData), &actual); err != nil {
		return fmt.Errorf("failed to parse event data: %w", err)
	}

	// 检查数据格式: 可能是完整格式 {"method": "xxx", "params": {...}}
	// 也可能只有 params {...}

	// 情况1: 完整格式,包含 method 和 params
	if method, ok := actual["method"].(string); ok {
		if method != expectedMethod {
			return fmt.Errorf("method mismatch: expected %s, got %s", expectedMethod, method)
		}

		actualParams, ok := actual["params"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("params not found in event data")
		}

		return validateParams(expectedParams, actualParams)
	}

	// 情况2: 只有 params,没有 method (method 存在 identify 字段)
	// 直接验证整个 actual 作为 params
	return validateParams(expectedParams, actual)
}

// validateParams 验证参数
func validateParams(expected, actual map[string]interface{}) error {
	for key, expectedValue := range expected {
		actualValue, ok := actual[key]
		if !ok {
			return fmt.Errorf("param key %s not found", key)
		}

		// 对于数字类型,需要统一处理
		if expNum, ok := expectedValue.(float64); ok {
			if actNum, ok := actualValue.(float64); ok {
				if expNum != actNum {
					return fmt.Errorf("param %s mismatch: expected %v, got %v", key, expectedValue, actualValue)
				}
				continue
			}
		}

		// 其他类型直接比较
		if fmt.Sprintf("%v", expectedValue) != fmt.Sprintf("%v", actualValue) {
			return fmt.Errorf("param %s mismatch: expected %v, got %v", key, expectedValue, actualValue)
		}
	}

	return nil
}

// ValidateResponse 验证响应格式
func ValidateResponse(response map[string]interface{}) error {
	result, ok := response["result"]
	if !ok {
		return fmt.Errorf("result field not found")
	}

	resultNum, ok := result.(float64)
	if !ok {
		return fmt.Errorf("result field is not a number")
	}

	if resultNum != 0 {
		errcode, _ := response["errcode"].(string)
		message, _ := response["message"].(string)
		return fmt.Errorf("response indicates failure: result=%v, errcode=%s, message=%s",
			resultNum, errcode, message)
	}

	return nil
}

// ValidateTimestamp 验证时间戳
func ValidateTimestamp(expected time.Time, actual int64, toleranceSeconds int) error {
	actualTime := time.Unix(actual, 0)
	diff := math.Abs(float64(actualTime.Sub(expected).Seconds()))

	if diff > float64(toleranceSeconds) {
		return fmt.Errorf("timestamp out of tolerance: expected %v, got %v, diff %.0f seconds",
			expected, actualTime, diff)
	}

	return nil
}

// ValidateJSON 验证JSON字符串格式
func ValidateJSON(jsonStr string) error {
	var js interface{}
	if err := json.Unmarshal([]byte(jsonStr), &js); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// ParseMessageFromTopic 从主题中解析 message_id
func ParseMessageFromTopic(topic string, pattern string) (string, error) {
	// 简单实现,假设 message_id 是主题的最后一部分
	// 例如: devices/attributes/set/666666666/1234567 -> 1234567
	// 可以根据需要使用正则表达式增强

	// 这里简化处理
	return "", fmt.Errorf("not implemented")
}
