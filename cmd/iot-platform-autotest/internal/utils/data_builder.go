package utils

import (
	"fmt"
	"math/rand"
	"time"
)

// GenerateMessageID 生成消息ID(毫秒时间戳后7位)
func GenerateMessageID() string {
	ts := time.Now().UnixMilli()
	return fmt.Sprintf("%d", ts%10000000)
}

// GenerateTimestamp 生成时间戳(秒)
func GenerateTimestamp() int64 {
	return time.Now().Unix()
}

// BuildTelemetryData 构建遥测数据
func BuildTelemetryData() map[string]interface{} {
	return map[string]interface{}{
		"temperature": 20.0 + rand.Float64()*20.0, // 20-40度
		"humidity":    40.0 + rand.Float64()*40.0, // 40-80%
		"switch":      rand.Intn(2) == 1,
	}
}

// BuildAttributeData 构建属性数据
func BuildAttributeData() map[string]interface{} {
	return map[string]interface{}{
		"ip":   fmt.Sprintf("192.168.1.%d", rand.Intn(255)),
		"mac":  fmt.Sprintf("00:11:22:33:44:%02X", rand.Intn(255)),
		"port": 1883,
	}
}

// BuildEventData 构建事件数据
func BuildEventData(method string, params map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"method": method,
		"params": params,
	}
}

// BuildResponseData 构建响应数据
func BuildResponseData(success bool, method string) map[string]interface{} {
	data := map[string]interface{}{
		"ts":      GenerateTimestamp(),
		"message": "success",
	}

	if success {
		data["result"] = 0
	} else {
		data["result"] = 1
		data["errcode"] = "UNKNOWN_ERROR"
		data["message"] = "operation failed"
	}

	if method != "" {
		data["method"] = method
	}

	return data
}
