package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	model "project/internal/model"
	query "project/internal/query"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

var allowedFileExts = []string{
	"jpg", "jpeg", "png", "pdf", "doc", "docx", "xlsx", "xls", "zip", "rar", "tar", "gz", "7z",
}

func OperationLogs() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isModifyMethod(c.Request.Method) {
			c.Next()
			return
		}

		logrus.Info("开始处理请求:", c.Request.URL.Path, "方法:", c.Request.Method)
		requestMessage, _ := processRequestBody(c)
		logrus.Info("请求体:", requestMessage)

		writer := newResponseBodyWriter(c)
		c.Writer = writer

		start := time.Now().UTC()
		c.Next()
		cost := time.Since(start).Milliseconds()

		logrus.Info("请求处理完成，状态码:", c.Writer.Status(), "耗时(ms):", cost)
		logrus.Info("响应体大小:", writer.body.Len())
		logrus.Info("响应的信息:", writer.body.String())

		saveOperationLog(c, start, cost, requestMessage, writer.body.String())
	}
}

func isModifyMethod(method string) bool {
	return method == http.MethodPost ||
		method == http.MethodPut ||
		method == http.MethodDelete
}

func processRequestBody(c *gin.Context) (string, string) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logrus.Error("读取请求体失败:", err)
		return "", ""
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	requestMessage := string(body)
	if strings.Contains(c.Request.URL.Path, "file/up") {
		requestMessage = handleFileUpload(c)
	}

	return requestMessage, requestMessage
}

func handleFileUpload(c *gin.Context) string {
	file, err := c.FormFile("file")
	if err != nil {
		return ""
	}

	fileType := c.PostForm("type")
	if fileType == "" {
		fileType = "unknown"
	}

	// 1. 获取安全的基本文件名,去除路径
	baseFileName := filepath.Base(file.Filename)

	// 2. 净化文件名
	filename := utils.SanitizeFilename(baseFileName)

	// 3. 二次验证文件名的安全性
	if !filepath.IsLocal(filename) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的文件名"})
		return ""
	}

	// 4. 验证文件类型
	if !utils.ValidateFileExtension(filename, allowedFileExts) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不允许的文件类型"})
		return ""
	}

	return fmt.Sprintf("%s:%s", fileType, filename)
}

func saveOperationLog(c *gin.Context, start time.Time, cost int64, requestMsg, responseMsg string) {
	// 检查 claims 是否存在
	claims, exists := c.Get("claims")
	if !exists {
		logrus.Info("未找到用户信息，跳过操作日志记录")
		return
	}

	// 类型断言
	userClaims, ok := claims.(*utils.UserClaims)
	if !ok {
		logrus.Info("用户信息类型不正确，跳过操作日志记录")
		return
	}

	// 检查 tenantID 是否为空
	if userClaims.TenantID == "" {
		logrus.Info("租户ID为空，跳过操作日志记录")
		return
	}

	path := c.Request.URL.Path

	log := &model.OperationLog{
		ID:              uuid.New(),
		IP:              c.ClientIP(),
		Path:            &path,
		UserID:          userClaims.ID,
		Name:            &c.Request.Method,
		CreatedAt:       start,
		Latency:         &cost,
		RequestMessage:  &requestMsg,
		ResponseMessage: &responseMsg,
		TenantID:        userClaims.TenantID,
	}

	query.OperationLog.Create(log)
}

type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func newResponseBodyWriter(c *gin.Context) responseBodyWriter {
	return responseBodyWriter{
		ResponseWriter: c.Writer,
		body:           &bytes.Buffer{},
	}
}

func (r responseBodyWriter) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
