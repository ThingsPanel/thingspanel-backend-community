package utils

import "time"

// 时间相关
func GetUTCTime() time.Time {
	return time.Now().UTC()
}

func GetSecondTimestamp() int64 {
	return time.Now().Unix()
}

func IsToday(t time.Time) bool {
	now := time.Now()
	return t.Year() == now.Year() &&
		t.Month() == now.Month() &&
		t.Day() == now.Day()
}

func DaysAgo(n int) time.Time {
	now := time.Now()
	past := now.AddDate(0, 0, -n)
	return past
}

func MillisecondsTimestampDaysAgo(n int) int64 {
	// 获取当前时间
	now := time.Now()
	// 计算n天前的时间
	past := now.AddDate(0, 0, -n)
	// 转换为毫秒时间戳
	milliseconds := past.UnixNano() / int64(time.Millisecond)
	return milliseconds
}
