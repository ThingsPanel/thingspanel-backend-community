package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateAPIKey 生成一个 API Key
func GenerateAPIKey() (string, error) {
	// 生成32字节的随机数
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成APIKey失败: %v", err)
	}

	// 添加sk_前缀并转为hex格式
	return fmt.Sprintf("sk_%s", hex.EncodeToString(bytes)), nil
}
