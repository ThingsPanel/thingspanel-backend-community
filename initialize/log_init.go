package initialize

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/natefinch/lumberjack.v2"
)

// sqlLogFileWriter SQL 日志文件 writer（全局变量，供数据库初始化使用）
var sqlLogFileWriter *lumberjack.Logger

// customFormatter 控制台输出格式化器（带颜色）
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

// fileHook 文件输出 Hook
type fileHook struct {
	writer    io.Writer
	formatter logrus.Formatter
}

// Levels 返回 hook 要处理的日志级别
func (hook *fileHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire 处理日志条目
func (hook *fileHook) Fire(entry *logrus.Entry) error {
	formatted, err := hook.formatter.Format(entry)
	if err != nil {
		return err
	}
	_, err = hook.writer.Write(formatted)
	return err
}

// fileFormatter 文件输出格式化器（不带颜色）
type fileFormatter struct {
	logrus.TextFormatter
}

func (*fileFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var levelText string
	switch entry.Level {
	case logrus.DebugLevel:
		levelText = "DEBUG"
	case logrus.InfoLevel:
		levelText = "INFO "
	case logrus.WarnLevel:
		levelText = "WARN "
	case logrus.ErrorLevel:
		levelText = "ERROR"
	case logrus.FatalLevel, logrus.PanicLevel:
		levelText = "FATAL"
	default:
		levelText = "UNKNOWN"
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

	// 组装格式化字符串（不带颜色代码）
	msg := fmt.Sprintf("%s [%s] %s%s [%s]\n",
		levelText, // 日志级别
		entry.Time.Format("2006-01-02 15:04:05.9999"), // 时间戳
		entry.Message, // 日志消息
		fieldsStr,     // fields 信息
		fileAndLine,   // 文件名:行号
	)

	return []byte(msg), nil
}

func LogInIt() error {
	// 初始化 Logrus,不创建logrus实例，直接使用包级别的函数，这样可以在项目的任何地方使用logrus，目前不考虑多日志模块的情况
	logrus.SetReportCaller(true)

	logLevels := map[string]logrus.Level{
		"panic": logrus.PanicLevel,
		"fatal": logrus.FatalLevel,
		"error": logrus.ErrorLevel,
		"warn":  logrus.WarnLevel,
		"info":  logrus.InfoLevel,
		"debug": logrus.DebugLevel,
		"trace": logrus.TraceLevel,
	}

	// 读取日志级别配置
	levelStr := viper.GetString("log.level")
	if level, ok := logLevels[levelStr]; ok {
		logrus.SetLevel(level)
	} else {
		logrus.Error("Invalid log level in config, setting to default level")
		logrus.SetLevel(logrus.InfoLevel) // 设置默认级别
	}

	// 读取 adapter_type 配置
	// 0-控制台输出 1-文件输出 2-文件和控制台输出
	adapterType := viper.GetInt("log.adapter_type")
	if adapterType < 0 || adapterType > 2 {
		adapterType = 0 // 默认控制台输出
		logrus.Warn("Invalid log adapter_type in config, setting to default (0 - console)")
	}

	// 读取文件配置
	maxdays := viper.GetInt("log.maxdays")
	if maxdays <= 0 {
		maxdays = 7 // 默认7天
	}
	maxsize := viper.GetInt("log.maxsize")
	if maxsize <= 0 {
		maxsize = 100 // 默认100MB
	}

	// 根据 adapter_type 设置输出目标

	// 文件输出
	if adapterType == 1 || adapterType == 2 {
		// 读取日志路径配置，如果没有配置则使用默认值 ./files/logs
		logDir := viper.GetString("log.path")
		if logDir == "" {
			logDir = "./files/logs" // 默认路径
		}
		// 确保日志目录存在
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}

		// 配置日志文件轮转
		logFile := filepath.Join(logDir, "app.log")
		fileWriter := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    maxsize, // 每个文件最大大小（MB），由配置决定
			MaxBackups: maxdays, // 保留最近 maxdays 天的文件
			MaxAge:     maxdays, // 文件保留天数
			Compress:   true,    // 压缩旧文件
			LocalTime:  true,    // 使用本地时间
		}

		// 为文件输出创建 hook（使用不带颜色的格式化器）
		fileHook := &fileHook{
			writer:    fileWriter,
			formatter: &fileFormatter{logrus.TextFormatter{FullTimestamp: true}},
		}
		logrus.AddHook(fileHook)

		// 初始化 SQL 日志文件 writer（单独的文件，便于区分）
		sqlLogFile := filepath.Join(logDir, "sql.log")
		sqlLogFileWriter = &lumberjack.Logger{
			Filename:   sqlLogFile,
			MaxSize:    maxsize, // 每个文件最大大小（MB），由配置决定
			MaxBackups: maxdays, // 保留最近 maxdays 天的文件
			MaxAge:     maxdays, // 文件保留天数
			Compress:   true,    // 压缩旧文件
			LocalTime:  true,    // 使用本地时间
		}
	}

	// 设置输出目标
	if adapterType == 0 {
		// 仅控制台输出
		logrus.SetFormatter(&customFormatter{logrus.TextFormatter{
			ForceColors:   true,
			FullTimestamp: true,
		}})
		logrus.SetOutput(os.Stdout)
	} else if adapterType == 1 {
		// 仅文件输出（hook 已经处理，这里不需要设置 output）
		// 但为了确保日志能正常输出，我们需要设置一个空的 output 或者使用 hook
		// logrus 的 hook 会在 formatter 之后执行，所以我们需要确保有 output
		logrus.SetOutput(io.Discard) // 丢弃默认输出，只使用 hook
	} else if adapterType == 2 {
		// 文件和控制台都输出
		// 控制台使用带颜色的格式化器
		logrus.SetFormatter(&customFormatter{logrus.TextFormatter{
			ForceColors:   true,
			FullTimestamp: true,
		}})
		logrus.SetOutput(os.Stdout)
		// 文件输出由 hook 处理
	}

	log.Printf("Logrus设置完成，adapter_type=%d, level=%s, maxdays=%d, maxsize=%dMB", adapterType, levelStr, maxdays, maxsize)
	return nil
}

// GetSQLLogWriter 获取 SQL 日志的 Writer
// 根据 log.adapter_type 配置返回合适的 Writer：
// 0 - 仅控制台输出
// 1 - 仅文件输出
// 2 - 文件和控制台都输出
func GetSQLLogWriter() io.Writer {
	adapterType := viper.GetInt("log.adapter_type")
	if adapterType < 0 || adapterType > 2 {
		adapterType = 0 // 默认控制台输出
	}

	var writers []io.Writer

	// 控制台输出
	if adapterType == 0 || adapterType == 2 {
		writers = append(writers, os.Stdout)
	}

	// 文件输出
	if (adapterType == 1 || adapterType == 2) && sqlLogFileWriter != nil {
		writers = append(writers, sqlLogFileWriter)
	}

	// 如果只有一个 writer，直接返回
	if len(writers) == 1 {
		return writers[0]
	}

	// 如果有多个 writer，使用 MultiWriter 同时写入
	if len(writers) > 1 {
		return io.MultiWriter(writers...)
	}

	// 默认返回控制台输出
	return os.Stdout
}
