package main

import (
	"strconv"
	"time"
)

func main() {
	go TempHumSensor()
	select {}
}

// 获取消息id
func GetMessageID() string {
	// 获取当前Unix时间戳
	timestamp := time.Now().Unix()
	// 将时间戳转换为字符串
	timestampStr := strconv.FormatInt(timestamp, 10)
	// 截取后七位
	messageID := timestampStr[len(timestampStr)-7:]

	return messageID
}
