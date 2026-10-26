package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const commandPlanPathPrefix = "/api/v1/command/datas/plans/"

// BrokerCommandPlanBodyLimits must run before middleware that reads or logs
// request bodies. The route handlers keep their own decoder limit as a second
// boundary; this middleware protects earlier global body readers.
func BrokerCommandPlanBodyLimits() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost && strings.HasPrefix(c.Request.URL.Path, commandPlanPathPrefix) {
			if c.Request.ContentLength > maxCommandPlanBody {
				commandPlanError(c, http.StatusRequestEntityTooLarge, "invalid_request")
				return
			}
			limited := http.MaxBytesReader(c.Writer, c.Request.Body, maxCommandPlanBody)
			body, err := io.ReadAll(limited)
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				commandPlanError(c, http.StatusRequestEntityTooLarge, "invalid_request")
				return
			}
			if err != nil {
				commandPlanError(c, http.StatusBadRequest, "invalid_request")
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
		}
		c.Next()
	}
}
