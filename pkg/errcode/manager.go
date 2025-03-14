// Package errcode 提供错误码管理和验证功能
// pkg/errcode/manager.go
package errcode

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/patrickmn/go-cache"
	"gopkg.in/yaml.v3"
)

// Config 定义配置文件结构
type Config struct {
	Messages map[int]map[string]string `yaml:"messages"` // code -> language -> message
	Metadata struct {
		Version            string   `yaml:"version"`
		LastUpdated        string   `yaml:"last_updated"`
		SupportedLanguages []string `yaml:"supported_languages"`
	} `yaml:"metadata"`
}

// StringConfig 定义字符串配置文件结构
type StringConfig struct {
	Messages map[string]map[string]string `yaml:"messages"` // key -> language -> message
	Metadata struct {
		Version            string   `yaml:"version"`
		LastUpdated        string   `yaml:"last_updated"`
		SupportedLanguages []string `yaml:"supported_languages"`
	} `yaml:"metadata"`
}

// ErrorManager 错误码管理器
type ErrorManager struct {
	messages        map[int]map[string]string    // code -> language -> message
	messageStr      map[string]map[string]string // key -> language -> message
	cache           *cache.Cache
	defaultLanguage string
	configPath      string
	strConfigPath   string
}

// NewErrorManager 创建错误码管理器实例
func NewErrorManager(configPath string, strConfigPath string) *ErrorManager {
	manager := &ErrorManager{
		messages:        make(map[int]map[string]string),
		messageStr:      make(map[string]map[string]string),
		cache:           cache.New(10*time.Minute, 20*time.Minute), // 缓存10分钟，每20分钟清理
		defaultLanguage: "zh_CN",                                   // 默认使用中文
		configPath:      configPath,
		strConfigPath:   strConfigPath,
	}
	return manager
}

// LoadMessages 加载错误码和字符串配置
func (m *ErrorManager) LoadMessages() error {
	// 加载错误码配置
	if err := m.loadErrorMessages(); err != nil {
		return fmt.Errorf("加载错误码配置失败: %w", err)
	}

	// 加载字符串配置
	if err := m.loadStringMessages(); err != nil {
		return fmt.Errorf("加载字符串配置失败: %w", err)
	}

	return nil
}

// loadErrorMessages 加载错误码配置
func (m *ErrorManager) loadErrorMessages() error {
	data, err := os.ReadFile(filepath.Clean(m.configPath))
	if err != nil {
		return fmt.Errorf("读取错误码配置文件失败: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("解析错误码配置文件失败: %w", err)
	}

	for code := range config.Messages {
		if !m.validateCode(code) {
			return fmt.Errorf("无效的错误码格式: %d", code)
		}
	}

	m.messages = config.Messages
	return nil
}

// loadStringMessages 加载字符串配置
func (m *ErrorManager) loadStringMessages() error {
	data, err := os.ReadFile(filepath.Clean(m.strConfigPath))
	if err != nil {
		return fmt.Errorf("读取字符串配置文件失败: %w", err)
	}

	var config StringConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("解析字符串配置文件失败: %w", err)
	}

	m.messageStr = config.Messages
	return nil
}

// GetMessage 获取指定错误码的消息
// 参数：
//   - code: 错误码
//   - lang: 语言代码，如果为空则使用默认语言
func (m *ErrorManager) GetMessage(code int, acceptLanguage string) string {
	// 如果没有指定语言，使用默认语言
	if acceptLanguage == "" {
		return m.getMessageForLanguage(code, m.defaultLanguage)
	}

	// 解析 Accept-Language 头
	languages := ParseAcceptLanguage(acceptLanguage)

	// 按优先级尝试每个语言
	for _, lang := range languages {
		normalizedLang := NormalizeLanguage(lang.Tag)
		if msg := m.getMessageForLanguage(code, normalizedLang); msg != "" {
			return msg
		}
	}

	// 如果都没找到，使用默认语言
	return m.getMessageForLanguage(code, m.defaultLanguage)
}

// GetMessageStr 获取指定key的字符串消息
func (m *ErrorManager) GetMessageStr(key string, acceptLanguage string) string {
	if acceptLanguage == "" {
		return m.getStrMessageForLanguage(key, m.defaultLanguage)
	}

	languages := ParseAcceptLanguage(acceptLanguage)
	for _, lang := range languages {
		normalizedLang := NormalizeLanguage(lang.Tag)
		if msg := m.getStrMessageForLanguage(key, normalizedLang); msg != "" {
			return msg
		}
	}

	return m.getStrMessageForLanguage(key, m.defaultLanguage)
}

// getMessageForLanguage 获取指定语言的消息
func (m *ErrorManager) getMessageForLanguage(code int, lang string) string {
	// 尝试从缓存获取
	cacheKey := fmt.Sprintf("%d:%s", code, lang)
	if msg, found := m.cache.Get(cacheKey); found {
		return msg.(string)
	}

	// 从内存中获取消息
	if messages, ok := m.messages[code]; ok {
		if msg, ok := messages[lang]; ok {
			m.cache.Set(cacheKey, msg, cache.DefaultExpiration)
			return msg
		}
	}

	// 如果是非默认语言且找不到消息，返回空字符串
	// 这样可以继续尝试其他语言选项
	if lang != m.defaultLanguage {
		return ""
	}

	// 使用默认语言的默认错误消息
	defaultMsg := "未知错误"
	if lang != "zh_CN" {
		defaultMsg = "Unknown Error"
	}
	return defaultMsg
}

// getStrMessageForLanguage 获取指定语言的字符串消息
func (m *ErrorManager) getStrMessageForLanguage(key string, lang string) string {
	cacheKey := fmt.Sprintf("str:%s:%s", key, lang)
	if msg, found := m.cache.Get(cacheKey); found {
		return msg.(string)
	}

	if messages, ok := m.messageStr[key]; ok {
		if msg, ok := messages[lang]; ok {
			m.cache.Set(cacheKey, msg, cache.DefaultExpiration)
			return msg
		}
	}

	if lang != m.defaultLanguage {
		return ""
	}

	// 如果找不到对应的字符串,返回key本身
	return key
}

func (m *ErrorManager) validateCode(code int) bool {
	// 特殊处理成功码
	if code == 200 {
		return true
	}

	// 检查长度和范围
	if code < 100000 || code > 599999 {
		return false
	}

	// 检查第一位数字允许的值（1, 2, 3, 4, 5）
	firstDigit := code / 100000
	switch firstDigit {
	case 1, 2, 3, 4, 5:
		return true
	default:
		return false
	}
}

// SetDefaultLanguage 设置默认语言
func (m *ErrorManager) SetDefaultLanguage(lang string) {
	m.defaultLanguage = lang
}

// ClearCache 清空缓存
func (m *ErrorManager) ClearCache() {
	m.cache.Flush()
}

// 使用示例：
/*
func main() {
    // 创建错误码管理器
    manager := NewErrorManager("config/messages.yaml")

    // 加载配置
    if err := manager.LoadMessages(); err != nil {
        log.Fatalf("加载错误码配置失败: %v", err)
    }

    // 获取错误消息
    msg := manager.GetMessage("100001", "zh_CN")
    fmt.Println(msg) // 输出：服务暂时不可用

    // 使用默认语言
    msg = manager.GetMessage("100001", "")
    fmt.Println(msg) // 输出：服务暂时不可用

    // 使用英文
    msg = manager.GetMessage("100001", "en_US")
    fmt.Println(msg) // 输出：Service Temporarily Unavailable
}
*/
