package middleware

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	model "project/internal/model"
	query "project/query"
	utils "project/utils"

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

			responseMessage := writer.body.String()
			operationlog := &model.OperationLog{
				ID:              uuid.New(),
				IP:              c.ClientIP(),
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
