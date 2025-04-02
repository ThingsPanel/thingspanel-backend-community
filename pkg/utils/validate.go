package utils

import (
	"regexp"
	"strings"
)

type InputType string

const (
	Email InputType = "email"
	Phone InputType = "phone"
)

type ValidateResult struct {
	IsValid bool      // 是否有效
	Type    InputType // 输入类型
	Message string    // 提示信息
}

func ValidateInput(input string) ValidateResult {
	// 去除首尾空格
	input = strings.TrimSpace(input)

	if input == "" {
		return ValidateResult{
			IsValid: false,
			Type:    "",
			Message: "input cannot be empty",
		}
	}

	// 邮箱正则
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

	// 手机号正则 (中国大陆手机号)
	phoneRegex := regexp.MustCompile(`^1[3-9]\d{9}$`)

	// 检查是否是有效的邮箱
	if emailRegex.MatchString(input) {
		return ValidateResult{
			IsValid: true,
			Type:    Email,
			Message: "not a valid email",
		}
	}

	// 检查是否是有效的手机号
	if phoneRegex.MatchString(input) {
		return ValidateResult{
			IsValid: true,
			Type:    Phone,
			Message: "not a valid phone number",
		}
	}

	// 如果不是有效格式，判断更像哪种类型并给出相应提示
	if strings.Contains(input, "@") {
		return ValidateResult{
			IsValid: false,
			Type:    Email,
			Message: "email format is incorrect",
		}
	}

	// 检查是否都是数字
	numberRegex := regexp.MustCompile(`^\d+$`)
	if numberRegex.MatchString(input) {
		return ValidateResult{
			IsValid: false,
			Type:    Phone,
			Message: "phone number format is incorrect",
		}
	}

	// 无法判断类型的情况
	return ValidateResult{
		IsValid: false,
		Type:    "",
		Message: "input format is incorrect",
	}
}
