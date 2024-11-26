package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	model "project/internal/model"
	query "project/internal/query"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

func sanitizeFilename(filename string) string {
	// 只保留文件名,移除路径
	filename = filepath.Base(filename)

	// 只允许字母数字和基本符号
	reg := regexp.MustCompile(`[^a-zA-Z0-9.-]`)
	filename = reg.ReplaceAllString(filename, "_")

	// 防止空文件名
	if filename == "" {
		return "unnamed_file"
	}
	return filename
}

func OperationLogs() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodDelete {
			// 读取body
			body, err := io.ReadAll(c.Request.Body)
			if err != nil {
				logrus.Error(err)
			} else {
				c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
			}

			path := c.Request.URL.Path
			userClaims := c.MustGet("claims").(*utils.UserClaims)
			requestMessage := string(body)

			// 安全处理文件上传
			if strings.Contains(path, "file/up") {
				if file, err := c.FormFile("file"); err == nil {
					fileType, _ := c.GetPostForm("type")
					if fileType == "" {
						fileType = "unknown"
					}
					// 安全处理文件名
					filename := sanitizeFilename(file.Filename)
					requestMessage = fmt.Sprintf("%s:%s", fileType, filename)
				}
			}

			writer := responseBodyWriter{
				ResponseWriter: c.Writer,
				body:           &bytes.Buffer{},
			}
			c.Writer = writer

			start := time.Now().UTC()
			c.Next()
			cost := time.Since(start).Milliseconds()

			// 获取客户端IP
			clientIP := c.ClientIP()

			responseMessage := writer.body.String()
			operationlog := &model.OperationLog{
				ID:              uuid.New(),
				IP:              clientIP,
				Path:            &path,
				UserID:          userClaims.ID,
				Name:            &c.Request.Method,
				CreatedAt:       start,
				Latency:         &cost,
				RequestMessage:  &requestMessage,
				ResponseMessage: &responseMessage,
				TenantID:        userClaims.TenantID,
			}
			query.OperationLog.Create(operationlog)
		}
	}
}

type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (r responseBodyWriter) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
