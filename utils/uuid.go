package utils

import "github.com/go-basic/uuid"

// 生成users主键
func GetUuid() string {
	uuid := uuid.New()
	return uuid
}
