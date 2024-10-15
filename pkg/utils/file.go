package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
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
