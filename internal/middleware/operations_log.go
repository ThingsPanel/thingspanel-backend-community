package middleware

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	model "project/internal/model"
	query "project/internal/query"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

func OperationLogs() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodDelete {
			//读取body
			body, err := ioutil.ReadAll(c.Request.Body)
			if err != nil {
				logrus.Error(err)
			} else {
				c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(body))
			}

			path := c.Request.URL.Path
			userClaims := c.MustGet("claims").(*utils.UserClaims)
			requestMessage := string(body)

			if strings.Contains(path, "file/up") {
				file, _ := c.FormFile("file")
				fileType, _ := c.GetPostForm("type")
				requestMessage = fileType + ":" + file.Filename
			}

			writer := responseBodyWriter{
				ResponseWriter: c.Writer,
				body:           &bytes.Buffer{},
			}
			c.Writer = writer
			start := time.Now().UTC()

			c.Next()
			cost := int64(time.Since(start) / time.Millisecond)

			// 分别打印各种可能的IP来源
			logrus.Debug("RemoteAddr: %s\n", c.Request.RemoteAddr)
			logrus.Debug("X-Forwarded-For: %s\n", c.Request.Header.Get("X-Forwarded-For"))
			logrus.Debug("X-Real-IP: %s\n", c.Request.Header.Get("X-Real-IP"))
			// 获取ip
			var clientIP string

			// 从X-Forwarded-For获取
			forwardedIP := c.Request.Header.Get("X-Forwarded-For")
			if forwardedIP != "" {
				ips := strings.Split(forwardedIP, ",")
				clientIP = strings.TrimSpace(ips[0])
			}

			// 如果没有，从X-Real-IP获取
			if clientIP == "" {
				clientIP = c.Request.Header.Get("X-Real-IP")
			}

			// 都没有才使用RemoteAddr
			if clientIP == "" {
				clientIP = c.Request.RemoteAddr
				if i := strings.LastIndex(clientIP, ":"); i > -1 {
					clientIP = clientIP[:i]
				}
			}

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
