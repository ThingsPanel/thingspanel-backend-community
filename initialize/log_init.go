package initialize

import (
	"fmt"
	"log"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type customFormatter struct {
	logrus.TextFormatter
}

func (*customFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var levelColor string
	var levelText string
	switch entry.Level {
	case logrus.DebugLevel:
		levelColor, levelText = "34", "DEBUG" // 蓝色
	case logrus.InfoLevel:
		levelColor, levelText = "36", "INFO " // 青色
	case logrus.WarnLevel:
		levelColor, levelText = "33", "WARN " // 黄色
	case logrus.ErrorLevel:
		levelColor, levelText = "31", "ERROR" // 红色
	case logrus.FatalLevel, logrus.PanicLevel:
		levelColor, levelText = "31", "FATAL" // 红色，更重要的错误
	default:
		levelColor, levelText = "0", "UNKNOWN" // 默认颜色
	}

	// 获取调用者信息
	var fileAndLine string
	if entry.HasCaller() {
		// 只保留从项目根目录开始的相对路径
		filePath := entry.Caller.File
		// 从完整路径中提取thingspanel-backend-community之后的部分
		if idx := strings.Index(filePath, "thingspanel-backend-community"); idx != -1 {
			filePath = filePath[idx+len("thingspanel-backend-community"):]
			// 确保路径以./开头
			if strings.HasPrefix(filePath, "/") || strings.HasPrefix(filePath, "\\") {
				filePath = "." + filePath
			} else {
				filePath = "./" + filePath
			}
		}
		fileAndLine = fmt.Sprintf("%s:%d", filePath, entry.Caller.Line)
	}

	// 处理 fields
	var fieldsStr string
	if len(entry.Data) > 0 {
		fieldsStr = " "
		for k, v := range entry.Data {
			fieldsStr += fmt.Sprintf("%s=%v ", k, v)
		}
	}

	// 组装格式化字符串，将路径移到最后
	msg := fmt.Sprintf("\033[1;%sm%s\033[0m \033[4;1;%sm[%s]\033[0m %s%s \033[1;%sm[%s]\033[0m\n",
		levelColor, levelText, // 日志级别，带颜色
		levelColor, entry.Time.Format("2006-01-02 15:04:05.9999"), // 时间戳，下划线加颜色
		entry.Message,           // 日志消息
		fieldsStr,               // fields 信息
		levelColor, fileAndLine, // 文件名:行号，带颜色，移到最后面
	)

	return []byte(msg), nil
}

func LogInIt() error {
	// 初始化 Logrus,不创建logrus实例，直接使用包级别的函数，这样可以在项目的任何地方使用logrus，目前不考虑多日志模块的情况
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&customFormatter{logrus.TextFormatter{
		ForceColors:   true,
		FullTimestamp: true,
	}})

	logLevels := map[string]logrus.Level{
		"panic": logrus.PanicLevel,
		"fatal": logrus.FatalLevel,
		"error": logrus.ErrorLevel,
		"warn":  logrus.WarnLevel,
		"info":  logrus.InfoLevel,
		"debug": logrus.DebugLevel,
		"trace": logrus.TraceLevel,
	}

	levelStr := viper.GetString("log.level")
	if level, ok := logLevels[levelStr]; ok {
		logrus.SetLevel(level)
	} else {
		logrus.Error("Invalid log level in config, setting to default level")
		logrus.SetLevel(logrus.InfoLevel) // 设置默认级别
	}

	log.Println("Logrus设置完成......")
	return nil
}
