package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// 文件是否存在
func FileExist(path string) bool {
	_, err := os.Lstat(path)
	return !os.IsNotExist(err)
}

// 用户输入组合路径安全校验
func CheckPath(param string) error {
	if count := strings.Count(param, "."); count > 0 {
		return errors.New("路径中不能包含非法字符“.”")
	}
	if count := strings.Count(param, "/"); count > 0 {
		return errors.New("路径中不能包含非法字符“/”")
	}
	if count := strings.Count(param, "\\"); count > 0 {
		return errors.New("路径中不能包含非法字符“\\”")
	}
	return nil
}

// 用户输入文件名安全校验
func CheckFilename(param string) error {
	if count := strings.Count(param, "."); count > 1 {
		return errors.New("文件名中不能超过一个“.”")
	}
	if count := strings.Count(param, "/"); count > 0 {
		return errors.New("文件名中不能包含非法字符“/”")
	}
	if count := strings.Count(param, "\\"); count > 0 {
		return errors.New("文件名中不能包含非法字符“\\”")
	}
	return nil
}

// 文件md5计算
func FileSign(filePath string, sign string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	if sign == "MD5" {
		hash := md5.New()
		_, _ = io.Copy(hash, file)
		return hex.EncodeToString(hash.Sum(nil)), nil
	} else {
		hash := sha256.New()
		_, _ = io.Copy(hash, file)
		return hex.EncodeToString(hash.Sum(nil)), nil
	}

}

// 允许的文件扩展名映射
var (
	// 允许的文件扩展名映射
	allowExtMap = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".svg": true,
		".ico": true, ".gif": true, ".xlsx": true, ".xls": true, ".csv": true,
	}

	// 允许的升级包文件扩展名映射
	allowUpgradePackageMap = map[string]bool{
		".bin": true, ".tar": true, ".gz": true, ".zip": true,
		".gzip": true, ".apk": true, ".dav": true, ".pack": true,
	}

	// 允许的导入批量文件扩展名映射
	allowImportBatchMap = map[string]bool{
		".xlsx": true, ".xls": true, ".csv": true,
	}
)

// ValidateFileType 检查文件类型是否允许上传
// filename: 文件名
// fileType: 文件类型（"upgradePackage", "importBatch", "d_plugin" 或其他）
// 返回值：如果文件类型允许上传则返回 true，否则返回 false
func ValidateFileType(filename, fileType string) bool {
	ext := strings.ToLower(path.Ext(filename))

	switch fileType {
	case "upgradePackage":
		return allowUpgradePackageMap[ext]
	case "importBatch":
		return allowImportBatchMap[ext]
	case "d_plugin":
		// 不做限制
		return true
	default:
		return allowExtMap[ext]
	}
}

// ValidateFileExtension 验证文件扩展名是否在允许列表中
func ValidateFileExtension(filename string, allowedExts []string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return false
	}

	// 移除扩展名开头的点
	ext = strings.TrimPrefix(ext, ".")

	for _, allowedExt := range allowedExts {
		if strings.ToLower(allowedExt) == ext {
			return true
		}
	}

	return false
}

// sanitizeFilename 清理文件名中的不安全字符
func SanitizeFilename(filename string) string {
	// 1. 只获取基本文件名，去除任何路径
	filename = filepath.Base(filename)

	// 2. 分离文件名和扩展名
	ext := filepath.Ext(filename)
	nameWithoutExt := strings.TrimSuffix(filename, ext)

	// 3. 移除或替换危险字符
	// 创建一个只允许字母、数字、下划线、连字符和点的正则表达式
	reg := regexp.MustCompile(`[^\w\-\.]`)
	nameWithoutExt = reg.ReplaceAllString(nameWithoutExt, "_")

	// 4. 处理特殊文件名
	nameWithoutExt = handleSpecialFilenames(nameWithoutExt)

	// 5. 限制文件名长度（不包括扩展名）
	if len(nameWithoutExt) > 200 {
		nameWithoutExt = nameWithoutExt[:200]
	}

	// 6. 确保文件名不以点开头（防止隐藏文件）
	if strings.HasPrefix(nameWithoutExt, ".") {
		nameWithoutExt = "_" + nameWithoutExt
	}

	// 7. 过滤扩展名中的危险字符
	ext = reg.ReplaceAllString(ext, "_")

	// 8. 重组文件名
	sanitizedName := nameWithoutExt + strings.ToLower(ext)

	// 9. 确保生成的文件名不为空
	if sanitizedName == "" {
		return "unnamed_file"
	}

	return sanitizedName
}

// handleSpecialFilenames 处理特殊的文件名
func handleSpecialFilenames(filename string) string {
	// 转换为小写进行比较
	lowerName := strings.ToLower(filename)

	// 特殊文件名列表
	specialNames := map[string]bool{
		"con": true, "prn": true, "aux": true, "nul": true,
		"com1": true, "com2": true, "com3": true, "com4": true,
		"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
	}

	// 如果是特殊文件名，添加前缀
	if specialNames[lowerName] {
		return "_" + filename
	}

	return filename
}
