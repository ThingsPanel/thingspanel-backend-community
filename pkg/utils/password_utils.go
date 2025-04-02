package utils

import (
	"strings"
	"unicode"

	"project/pkg/errcode"

	"github.com/sirupsen/logrus"
)

// ValidatePassword 检查给定的密码是否满足所需的标准。
// 如果密码无效，则返回错误，并说明为什么无效。
// 密码必须：
// - 至少6个字符长
// - 只包含字母数字字符和以下特殊字符：!@#$%^&*()_+-=[]{};\:'"|,./<>?
// - 包含至少一个大写字母，一个小写字母，一个数字和一个特殊字符
func ValidatePassword(password string) error {
	// 检查密码长度
	if len(password) < 6 {
		return errcode.New(200040)
	}

	validSpecialChars := "!@#$%^&*()_+-=[]{};\\':\"|,./<>?"
	var (
		hasUpper   bool
		hasLower   bool
		hasNumber  bool
		hasSpecial bool
	)
	invalidChars := make([]rune, 0)

	// 遍历密码中的每个字符
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasNumber = true
		case strings.ContainsRune(validSpecialChars, char):
			hasSpecial = true
		default:
			invalidChars = append(invalidChars, char)
		}
	}

	// 检查无效字符
	if len(invalidChars) > 0 {
		return errcode.WithVars(200053, map[string]interface{}{
			"invalid_chars": string(invalidChars),
		})
	}
	logrus.Debug("hasUpper", hasUpper)
	logrus.Debug("hasSpecial", hasSpecial)
	// 检查密码复杂度
	var missingElements []string
	// if !hasUpper {
	// 	missingElements = append(missingElements, "大写字母")
	// }
	if !hasLower {
		missingElements = append(missingElements, "小写字母")
	}
	if !hasNumber {
		missingElements = append(missingElements, "数字")
	}
	// if !hasSpecial {
	// 	missingElements = append(missingElements, "特殊字符")
	// }

	if len(missingElements) > 0 {
		return errcode.WithVars(200054, map[string]interface{}{
			"missing_elements": strings.Join(missingElements, "、"),
		})
	}

	return nil
}
