package utils

import "strings"

func FormatLangCode(acceptLanguage string) string {
	// 如果为空则返回默认值 en_US
	if acceptLanguage == "" {
		return "en_US"
	}

	// 分割 accept-language，取第一个
	langs := strings.Split(acceptLanguage, ",")
	primaryLang := strings.TrimSpace(langs[0])

	// 处理可能的权重值 如 zh-CN;q=0.9
	primaryLang = strings.Split(primaryLang, ";")[0]

	// 替换 - 为 _
	primaryLang = strings.Replace(primaryLang, "-", "_", 1)

	// 处理特殊情况
	switch primaryLang {
	case "zh":
		return "zh_CN"
	case "en":
		return "en_US"
	}

	// 如果已经是正确格式则直接返回
	if len(primaryLang) == 5 && primaryLang[2] == '_' {
		return primaryLang
	}

	// 其他情况返回默认值
	return "zh_CN"
}
