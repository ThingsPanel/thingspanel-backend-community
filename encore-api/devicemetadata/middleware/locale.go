package middleware

import (
	"context"
	"strconv"
	"strings"
)

// LocaleContextKey is used to store locale in context
type localeContextKey string

const localeKey localeContextKey = "locale"

const defaultLocale = "zh-CN"

// ParseAcceptLanguage parses RFC 7231 Accept-Language header
// Format: "en-US,en;q=0.9,zh-CN;q=0.8"
// Returns the first (most preferred) language code or default
func ParseAcceptLanguage(header string) string {
	if header == "" {
		return defaultLocale
	}

	// Split by comma to get language ranges
	ranges := strings.Split(header, ",")
	var topLang struct {
		lang   string
		weight float64
	}
	topLang.weight = -1

	for _, langRange := range ranges {
		langRange = strings.TrimSpace(langRange)
		if langRange == "" {
			continue
		}

		// Split language from quality factor
		parts := strings.Split(langRange, ";")
		lang := strings.TrimSpace(parts[0])

		// Validate basic language code format
		if !isValidLanguageCode(lang) {
			continue
		}

		// Default quality is 1.0
		quality := 1.0
		if len(parts) > 1 {
			// Parse quality factor: q=0.9
			for _, param := range parts[1:] {
				param = strings.TrimSpace(param)
				if strings.HasPrefix(param, "q=") {
					qStr := strings.TrimPrefix(param, "q=")
					if q, err := strconv.ParseFloat(qStr, 64); err == nil {
						quality = q
					}
				}
			}
		}

		// Track the highest weighted language
		if quality > topLang.weight {
			topLang.weight = quality
			topLang.lang = lang
		}
	}

	// Validate the language code format (should be like en, en-US, zh-CN, etc.)
	if topLang.lang == "" {
		return defaultLocale
	}

	// Normalize the language code
	lang := NormalizeLanguageCode(topLang.lang)
	if lang == "" {
		return defaultLocale
	}

	return lang
}

// isValidLanguageCode checks if a string looks like a valid language code
func isValidLanguageCode(code string) bool {
	if code == "" {
		return false
	}

	// Language codes should match pattern: [a-zA-Z]{2,3}(-[a-zA-Z]{2})?
	parts := strings.Split(code, "-")
	if len(parts) > 2 {
		return false // Too many parts
	}

	// First part should be 2-3 letters
	if len(parts[0]) < 2 || len(parts[0]) > 3 {
		return false
	}
	for _, c := range parts[0] {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}

	// Second part (region) should be 2 letters
	if len(parts) > 1 {
		if len(parts[1]) != 2 {
			return false
		}
		for _, c := range parts[1] {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
				return false
			}
		}
	}

	return true
}

// NormalizeLanguageCode normalizes language codes to standard format
// Examples: "en" -> "en", "en-US" -> "en-US", "EN-us" -> "en-US"
func NormalizeLanguageCode(code string) string {
	if code == "" {
		return ""
	}

	// Remove extra spaces
	code = strings.TrimSpace(code)

	// Split by hyphen
	parts := strings.Split(code, "-")
	if len(parts) == 0 {
		return ""
	}

	// Normalize: language code to lowercase, region to uppercase
	result := strings.ToLower(parts[0])

	if len(parts) > 1 {
		result += "-" + strings.ToUpper(parts[1])
	}

	return result
}

// WithLocale injects locale into context
func WithLocale(ctx context.Context, locale string) context.Context {
	if locale == "" {
		locale = defaultLocale
	}
	return context.WithValue(ctx, localeKey, locale)
}

// LocaleFromCtx extracts locale from context with fallback to default
func LocaleFromCtx(ctx context.Context) string {
	locale, ok := ctx.Value(localeKey).(string)
	if !ok || locale == "" {
		return defaultLocale
	}
	return locale
}

// GetDefaultLocale returns the default locale
func GetDefaultLocale() string {
	return defaultLocale
}
