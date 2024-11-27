package initialize

import (
	"fmt"
	"log"
	"path/filepath"

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
		dir := filepath.Dir(entry.Caller.File)
		fileAndLine = fmt.Sprintf("%s/%s:%d", filepath.Base(dir), filepath.Base(entry.Caller.File), entry.Caller.Line)
	}

	// 组装格式化字符串
	msg := fmt.Sprintf("\033[1;%sm%s\033[0m \033[4;1;%sm[%s]\033[0m \033[1;%sm[%s]\033[0m %s\n",
		levelColor, levelText, // 日志级别，带颜色
		levelColor, entry.Time.Format("2006-01-02 15:04:05.9999"), // 时间戳，下划线加颜色
		levelColor, fileAndLine, // 文件名:行号，带颜色
		entry.Message, // 日志消息
	)

	return []byte(msg), nil
}
func LogInIt() {

	// 初始化 Logrus,不创建logrus实例，直接使用包级别的函数，这样可以在项目的任何地方使用logrus，目前不考虑多日志模块的情况
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&customFormatter{logrus.TextFormatter{
		ForceColors:   true,
		FullTimestamp: true,
	}})

	var logLevels = map[string]logrus.Level{
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

	log.Println("Logrus设置完成...")
}
