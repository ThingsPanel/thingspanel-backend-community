// pkg/errcode/language.go
package errcode

import (
	"sort"
	"strconv"
	"strings"
)

// Language 表示一个语言选项及其权重
type Language struct {
	Tag    string  // 语言标签
	Weight float64 // 权重(q值)
}

// ParseAcceptLanguage 解析 Accept-Language 头
// 示例输入: "fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7"
func ParseAcceptLanguage(header string) []Language {
	if header == "" {
		return nil
	}

	parts := strings.Split(header, ",")
	langs := make([]Language, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// 分割语言标签和权重
		langParts := strings.Split(part, ";")
		lang := strings.TrimSpace(langParts[0])

		// 默认权重为1.0
		weight := 1.0

		// 如果有指定权重，则解析权重
		if len(langParts) > 1 {
			qPart := strings.TrimSpace(langParts[1])
			if strings.HasPrefix(qPart, "q=") {
				w := strings.TrimPrefix(qPart, "q=")
				if parsedWeight, err := strconv.ParseFloat(w, 64); err == nil {
					weight = parsedWeight
				}
			}
		}

		// 添加语言标签
		langs = append(langs, Language{Tag: lang, Weight: weight})
	}

	// 按权重降序排序
	sort.Slice(langs, func(i, j int) bool {
		return langs[i].Weight > langs[j].Weight
	})

	return langs
}

// NormalizeLanguage 规范化语言标签
// 示例: "zh-CN" -> "zh_CN"
func NormalizeLanguage(lang string) string {
	// 移除权重部分
	if idx := strings.Index(lang, ";"); idx != -1 {
		lang = lang[:idx]
	}

	// 将 "-" 替换为 "_"
	return strings.ReplaceAll(lang, "-", "_")
}
