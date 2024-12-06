package api

import (
	"crypto/md5"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"project/pkg/common"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type UpLoadApi struct{}

// 定义文件上传配置
const (
	BaseUploadDir = "./files/"
	OtaPath       = "./api/v1/ota/download/files/"
	MaxFileSize   = 200 << 20 // 200MB
)

// UpFile 处理文件上传
// @Tags     文件上传
// @Router   /api/v1/file/up [post]
func (*UpLoadApi) UpFile(c *gin.Context) {
	// 验证请求参数
	file, fileType, err := validateRequest(c)
	if err != nil {
		ErrorHandler(c, http.StatusBadRequest, err)
		return
	}

	// 生成文件路径
	uploadDir, fileName, err := generateFilePath(fileType, file.Filename)
	if err != nil {
		ErrorHandler(c, http.StatusUnprocessableEntity, err)
		return
	}

	// 保存文件
	filePath, err := saveFile(c, file, uploadDir, fileName, fileType)
	if err != nil {
		ErrorHandler(c, http.StatusUnprocessableEntity, err)
		return
	}

	SuccessHandler(c, "上传成功", map[string]interface{}{
		"path": filePath,
	})
}

// validateRequest 验证上传请求
func validateRequest(c *gin.Context) (*multipart.FileHeader, string, error) {
	file, err := c.FormFile("file")
	if err != nil || file == nil {
		return nil, "", errors.New("文件获取失败")
	}

	// 验证文件大小
	if file.Size > MaxFileSize {
		return nil, "", fmt.Errorf("文件大小不能超过 200MB，当前大小 %.2fMB", float64(file.Size)/(1<<20))
	}

	// 验证文件类型
	fileType := c.PostForm("type")
	if fileType == "" {
		return nil, "", errors.New("无效的文件类型")
	}

	// 文件安全检查
	filename := sanitizeFilename(file.Filename)
	if err := validateFileType(filename, fileType); err != nil {
		return nil, "", err
	}

	file.Filename = filename
	return file, fileType, nil
}

// generateFilePath 生成安全的文件路径
func generateFilePath(fileType, filename string) (string, string, error) {
	dateDir := time.Now().Format("2006-01-02")
	uploadDir := filepath.Join(BaseUploadDir, fileType, dateDir)

	if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
		return "", "", fmt.Errorf("创建目录失败: %v", err)
	}

	// 生成唯一文件名
	ext := strings.ToLower(filepath.Ext(filename))
	randomStr, err := common.GenerateRandomString(16)
	if err != nil {
		return "", "", err
	}

	timeStr := time.Now().Format("20060102150405")
	hashStr := fmt.Sprintf("%x", md5.Sum([]byte(timeStr+randomStr)))
	fileName := hashStr + ext

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
