package tp_log

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/spf13/viper"
)

func Init() {
	log.SetFlags(log.Lshortfile | log.Ltime | log.Ldate)
	log.Println("系统日志初始化...")

	dateStr := time.Now().Format("2006-01-02")
	maxdays := viper.GetInt("log.maxdays")
	level := viper.GetInt("log.level")
	maxlines := viper.GetInt("log.maxlines")

	dataSource := &struct {
		Filename string `json:"filename"`
		Level    int    `json:"level"`
		Maxlines int    `json:"maxlines"`
		Maxsize  int    `json:"maxsize"`
		Daily    bool   `json:"daily"`
		Maxdays  int    `json:"maxdays"`
		Color    bool   `json:"color"`
	}{
		Filename: fmt.Sprintf("files/logs/%s/log.log", dateStr),
		Level:    level,
		Maxlines: maxlines,
		Maxsize:  0,
		Daily:    true,
		Maxdays:  maxdays,
		Color:    true,
	}
	dataSourceBytes, err := json.Marshal(dataSource)
	if err != nil {
		log.Println("无法创建dataSource:", err)
	}
	logs.SetLevel(level)
	adapter_type := viper.GetInt("log.adapter_type")

	// 日志输出选择
	switch adapter_type {
	case 0:
		logs.SetLogger(logs.AdapterConsole)
	case 1:
		logs.Reset()
		logs.SetLogger(logs.AdapterFile, string(dataSourceBytes))
	case 2:
		logs.SetLogger(logs.AdapterFile, string(dataSourceBytes))
	default:
		logs.SetLogger(logs.AdapterConsole)
	}
	// 是否记录日志的调用层级 默认是logs.SetLogFuncCallDepth(2)
	logs.EnableFuncCallDepth(true)
	logs.Async()
	log.Println("系统日志初始化完成")
}
