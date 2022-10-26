package utils

import (
	"time"

	"github.com/go-basic/uuid"
)

// 生成users主键
func GetUuid() string {
	uuid := uuid.New()
	return uuid
}

//时间转时间戳
func Strtime2Int(datetime string) int64 {
	//日期转化为时间戳
	timeLayout := "2006-01-02 15:04:05" //转化所需模板
	tmp, _ := time.ParseInLocation(timeLayout, datetime, time.Local)
	timestamp := tmp.Unix() //转化为时间戳 类型是int64
	return timestamp
}
