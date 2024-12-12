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

// ErrorManager 错误码管理器
type ErrorManager struct {
	messages        map[int]map[string]string // code -> language -> message
	cache           *cache.Cache
	defaultLanguage string
	configPath      string
}

// NewErrorManager 创建错误码管理器实例
func NewErrorManager(configPath string) *ErrorManager {
	manager := &ErrorManager{
		messages:        make(map[int]map[string]string),
		cache:           cache.New(10*time.Minute, 20*time.Minute), // 缓存10分钟，每20分钟清理
		defaultLanguage: "zh_CN",                                   // 默认使用中文
		configPath:      configPath,
	}
	return manager
}

// LoadMessages 加载错误码配置
func (m *ErrorManager) LoadMessages() error {
	// 读取配置文件
	data, err := os.ReadFile(filepath.Clean(m.configPath))
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析配置
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 验证错误码格式
	for code := range config.Messages {
		if !m.validateCode(code) {
			return fmt.Errorf("无效的错误码格式: %d", code)
		}
	}

	// 更新内存中的消息
	m.messages = config.Messages

	// 清空缓存
	m.cache.Flush()

	return nil
}

// GetMessage 获取指定错误码的消息
// 参数：
//   - code: 错误码
//   - lang: 语言代码，如果为空则使用默认语言
func (m *ErrorManager) GetMessage(code int, lang string) string {
	if lang == "" {
		lang = m.defaultLanguage
	}

	// 尝试从缓存获取
	cacheKey := fmt.Sprintf("%d:%s", code, lang)
	if msg, found := m.cache.Get(cacheKey); found {
		return msg.(string)
	}

	// 从内存中获取消息
	if messages, ok := m.messages[code]; ok {
		// 优先使用请求的语言
		if msg, ok := messages[lang]; ok {
			m.cache.Set(cacheKey, msg, cache.DefaultExpiration)
			return msg
		}
		// 回退到默认语言
		if msg, ok := messages[m.defaultLanguage]; ok {
			m.cache.Set(cacheKey, msg, cache.DefaultExpiration)
			return msg
		}
	}

	// 找不到消息时返回默认错误信息
	defaultMsg := "未知错误"
	if lang != "zh_CN" {
		defaultMsg = "Unknown Error"
	}
	return defaultMsg
}

func (m *ErrorManager) validateCode(code int) bool {
	// 特殊处理成功码
	if code == 200 {
		return true
	}

	// 检查长度和第一位数字
	if code < 100000 || code > 299999 {
		return false
	}

	// 检查错误类型（1或2）
	firstDigit := code / 100000
	if firstDigit != 1 && firstDigit != 2 {
		return false
	}

	return true
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
