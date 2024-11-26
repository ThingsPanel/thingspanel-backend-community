package api

import (
	"crypto/md5"
	"errors"
	"fmt"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type UpLoadApi struct{}

// 定义文件上传配置
const (
	BaseUploadDir = "./files/"
	OtaPath       = "./api/v1/ota/download/files/"
	MaxFileSize   = 200 << 20 // 200 MB = 200 * 1024 * 1024 bytes
)

// UpFile
// @Tags     文件上传
// @Router   /api/v1/file/up [post]
// UpFile 处理文件上传
func (a *UpLoadApi) UpFile(c *gin.Context) {
	// 1. 验证请求参数
	file, fileType, err := validateRequest(c)
	if err != nil {
		ErrorHandler(c, http.StatusBadRequest, err)
		return
	}

	filename := filepath.Base(file.Filename)

	// 2. 生成安全的文件名和路径
	uploadDir, fileName, err := generateFilePath(fileType, filename)
	if err != nil {
		ErrorHandler(c, http.StatusUnprocessableEntity, err)
		return
	}

	// 3. 保存文件
	filePath, err := saveFile(c, file, uploadDir, fileName, fileType)
	if err != nil {
		ErrorHandler(c, http.StatusUnprocessableEntity, err)
		return
	}

	// 4. 返回结果
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

	filename := filepath.Base(file.Filename)
	// 检查文件大小
	if file.Size > MaxFileSize {
		return nil, "", fmt.Errorf("文件大小不能超过 200MB，当前大小 %.2fMB", float64(file.Size)/(1<<20))
	}

	fileType, exists := c.GetPostForm("type")
	if !exists || fileType == "" {
		return nil, "", errors.New("无效的文件类型")
	}

	// 检查文件路径和类型
	if err := utils.CheckPath(fileType); err != nil {
		return nil, "", err
	}

	if !utils.ValidateFileType(filename, fileType) {
		return nil, "", errors.New("文件类型验证失败")
	}

	return file, fileType, nil
}

// generateFilePath 生成安全的文件路径
func generateFilePath(fileType, originalFilename string) (string, string, error) {
	// 生成上传目录
	dateDir := time.Now().Format("2006-01-02/")
	uploadDir := filepath.Join(BaseUploadDir, fileType, dateDir)

	// 确保目录存在
	if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
		return "", "", fmt.Errorf("创建目录失败: %v", err)
	}

	// 生成安全的文件名
	ext := strings.ToLower(filepath.Ext(originalFilename))
	randomStr := fmt.Sprintf("%d", rand.Int31n(9999)+1000)
	timeStr := time.Now().Format("2006_01_02_15_04_05_")
	hashName := md5.Sum([]byte(timeStr + randomStr))
	fileName := fmt.Sprintf("%x%s", hashName, ext)

	// 验证文件名
	if err := utils.CheckFilename(fileName); err != nil {
		return "", "", err
	}

	return uploadDir, fileName, nil
}

// saveFile 保存文件并返回路径
func saveFile(c *gin.Context, file *multipart.FileHeader, uploadDir, fileName, fileType string) (string, error) {
	fullPath := filepath.Join(uploadDir, fileName)

	// 保存文件
	if err := c.SaveUploadedFile(file, fullPath); err != nil {
		return "", fmt.Errorf("保存文件失败: %v", err)
	}

	// 特殊处理升级包路径
	if fileType == "upgradePackage" {
		return filepath.Join(OtaPath, fileType, time.Now().Format("2006-01-02/"), fileName), nil
	}

	return fullPath, nil
}
