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
// 示例输入: "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7"
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

		// 添加完整的语言标签
		langs = append(langs, Language{Tag: lang, Weight: weight})

		// 对于复杂的语言标签，添加主语言变体
		// 例如：对于zh-CN，添加zh但权重略低
		parts := strings.Split(lang, "-")
		if len(parts) > 1 {
			mainLang := parts[0]
			if mainLang != lang {
				langs = append(langs, Language{
					Tag:    mainLang,
					Weight: weight - 0.001, // 稍低的权重
				})
			}
		}
	}

	// 按权重降序排序
	sort.Slice(langs, func(i, j int) bool {
		if langs[i].Weight == langs[j].Weight {
			// 权重相同时，优先选择更具体的语言标签
			return len(langs[i].Tag) > len(langs[j].Tag)
		}
		return langs[i].Weight > langs[j].Weight
	})

	return langs
}

// NormalizeLanguage 规范化语言标签
// 示例：
// "zh-CN" -> "zh_CN"
// "zh-Hans" -> "zh_CN"
// "zh" -> "zh_CN"
func NormalizeLanguage(lang string) string {
	// 移除权重部分
	if idx := strings.Index(lang, ";"); idx != -1 {
		lang = lang[:idx]
	}
	lang = strings.TrimSpace(lang)

	// 特殊情况处理
	switch lang {
	case "zh", "zh-Hans", "zh-CHS":
		return "zh_CN"
	case "zh-Hant", "zh-CHT":
		return "zh_TW"
	case "en", "en-US", "en-GB":
		return "en_US" // 统一使用美式英语
	}

	// 处理标准的语言-地区格式
	if len(lang) >= 4 && lang[2] == '-' {
		// 转换 "zh-CN" 到 "zh_CN" 格式
		return lang[:2] + "_" + strings.ToUpper(lang[3:])
	}

	return lang
}

// Example usage:
/*
func main() {
	header := "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7"
	langs := ParseAcceptLanguage(header)
	for _, lang := range langs {
		normalized := NormalizeLanguage(lang.Tag)
		fmt.Printf("Tag: %s, Normalized: %s, Weight: %.2f\n",
			lang.Tag, normalized, lang.Weight)
	}
}

Output:
Tag: zh-CN, Normalized: zh_CN, Weight: 1.00
Tag: zh, Normalized: zh_CN, Weight: 0.90
Tag: en-US, Normalized: en_US, Weight: 0.80
Tag: en, Normalized: en_US, Weight: 0.70
*/
