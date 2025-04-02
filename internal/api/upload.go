package api

import (
	"crypto/md5"
	"errors"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"project/pkg/common"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type UpLoadApi struct{}

// 定义文件上传配置
const (
	BaseUploadDir = "./files/"
	OtaPath       = "./api/v1/ota/download/files/"
	MaxFileSize   = 500 << 20 // 200MB
)

// UpFile 处理文件上传
// @Tags     文件上传
// @Router   /api/v1/file/up [post]
func (*UpLoadApi) UpFile(c *gin.Context) {
	// 检查文件是否为空
	file, err := c.FormFile("file")
	if err != nil || file == nil {
		c.Error(errcode.New(errcode.CodeFileEmpty))
		return
	}

	// 验证文件类型
	fileType := c.PostForm("type")
	if fileType == "" {
		c.Error(errcode.New(errcode.CodeFileEmpty))
		return
	}

	// 验证文件大小
	if file.Size > MaxFileSize {
		c.Error(errcode.WithVars(errcode.CodeFileTooLarge, map[string]interface{}{
			"max_size":     "500MB",
			"current_size": fmt.Sprintf("%.2fMB", float64(file.Size)/(1<<20)),
		}))
		return
	}

	// 文件名净化
	filename := sanitizeFilename(file.Filename)

	// 文件类型检查
	if err := validateFileType(filename, fileType); err != nil {
		c.Error(errcode.WithVars(errcode.CodeFileTypeMismatch, map[string]interface{}{
			"expected_type": fileType,
			"actual_type":   filepath.Ext(filename),
		}))
		return
	}

	// 生成文件路径
	uploadDir, fileName, err := generateFilePath(fileType, file.Filename)
	if err != nil {
		c.Error(errcode.WithVars(errcode.CodeFilePathGenError, map[string]interface{}{
			"error":     err.Error(),
			"file_type": fileType,
			"filename":  file.Filename,
		}))
		return
	}

	// 保存文件
	filePath, err := saveFile(c, file, uploadDir, fileName, fileType)
	if err != nil {
		c.Error(errcode.WithVars(errcode.CodeFileSaveError, map[string]interface{}{
			"error":      err.Error(),
			"upload_dir": uploadDir,
			"filename":   fileName,
		}))
		return
	}

	c.Set("data", map[string]interface{}{
		"path": filePath,
	})
}

// generateFilePath 生成安全的文件路径,路径：./files/{type}/{2023-08-10}/
func generateFilePath(fileType, filename string) (string, string, error) {
	// 1. 验证 fileType 是否包含非法字符
	if strings.ContainsAny(fileType, "./\\") {
		return "", "", errcode.New(errcode.CodeFilePathGenError)
	}

	// 2. 生成日期目录
	dateDir := time.Now().Format("2006-01-02")

	// 3. 使用 filepath.Clean 清理并验证路径
	uploadDir := filepath.Clean(filepath.Join(BaseUploadDir, fileType, dateDir))
	absUploadDir, err := filepath.Abs(uploadDir)
	if err != nil {
		return "", "", errcode.WithVars(errcode.CodeFilePathGenError, map[string]interface{}{
			"error": "invalid path",
		})
	}

	absBaseDir, err := filepath.Abs(BaseUploadDir)
	if err != nil {
		return "", "", errcode.WithVars(errcode.CodeFilePathGenError, map[string]interface{}{
			"error": "invalid base path",
		})
	}

	// 确保生成的路径在基础目录下
	if !strings.HasPrefix(absUploadDir, absBaseDir) {
		return "", "", errcode.WithVars(errcode.CodeFilePathGenError, map[string]interface{}{
			"error": "path traversal detected",
		})
	}

	// 4. 创建目录
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return "", "", errcode.WithVars(errcode.CodeFilePathGenError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	// 5. 生成唯一文件名
	randomStr, err := common.GenerateRandomString(16)
	if err != nil {
		return "", "", errcode.WithVars(errcode.CodeFilePathGenError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	timeStr := time.Now().Format("20060102150405")
	hashStr := fmt.Sprintf("%x", md5.Sum([]byte(timeStr+randomStr)))
	fileName := hashStr + strings.ToLower(filepath.Ext(filename))

	return uploadDir, fileName, nil
}

// saveFile 保存文件并返回路径
func saveFile(c *gin.Context, file *multipart.FileHeader, uploadDir, fileName, fileType string) (string, error) {
	fullPath := filepath.Join(uploadDir, fileName)

	if err := c.SaveUploadedFile(file, fullPath); err != nil {
		return "", fmt.Errorf("保存文件失败: %v", err)
	}

	// 特殊处理升级包路径
	if fileType == "upgradePackage" {
		return "./" + filepath.Join(OtaPath, fileType, time.Now().Format("2006-01-02"), fileName), nil
	}

	return "./" + fullPath, nil
}

// sanitizeFilename 净化文件名
func sanitizeFilename(filename string) string {
	ext := filepath.Ext(filename)
	nameOnly := strings.TrimSuffix(filepath.Base(filename), ext)

	reg := regexp.MustCompile(`[^a-zA-Z0-9-_]+`)
	sanitized := reg.ReplaceAllString(nameOnly, "_")

	if sanitized == "" || sanitized == "_" {
		sanitized = fmt.Sprintf("file_%d", time.Now().Unix())
	}

	return sanitized + strings.ToLower(ext)
}

// validateFileType 验证文件类型
func validateFileType(filename, fileType string) error {
	if err := utils.CheckPath(fileType); err != nil {
		return fmt.Errorf("无效的文件路径: %v", err)
	}

	if !utils.ValidateFileType(filename, fileType) {
		return errors.New("文件类型不允许")
	}

	return nil
}
