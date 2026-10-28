package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBrokerCommandPlanBodyLimitsRejectsKnownOversizeBeforeBodyLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(BrokerCommandPlanBodyLimits())
	loggerCalled, serviceCalled := false, false
	router.Use(func(c *gin.Context) {
		loggerCalled = true
		_, _ = io.ReadAll(c.Request.Body)
		c.Next()
	})
	router.POST(commandPlanPathPrefix+"preview", func(c *gin.Context) {
		serviceCalled = true
		c.Status(http.StatusOK)
	})

	body := bytes.Repeat([]byte("x"), maxCommandPlanBody+1)
	req := httptest.NewRequest(http.MethodPost, commandPlanPathPrefix+"preview", bytes.NewReader(body))
	if req.ContentLength <= maxCommandPlanBody {
		t.Fatalf("fixture content length=%d, want above limit", req.ContentLength)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), `"error":"invalid_request"`) {
		t.Fatalf("response=%d %s, want fixed 413 invalid_request", response.Code, response.Body.String())
	}
	if loggerCalled || serviceCalled {
		t.Fatalf("body logger/service ran for known oversized request: logger=%t service=%t", loggerCalled, serviceCalled)
	}
}

func TestBrokerCommandPlanBodyLimitsRejectsUnknownOversizeBeforeLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(BrokerCommandPlanBodyLimits())
	loggerCalled := false
	serviceCalled := false
	router.Use(func(c *gin.Context) {
		loggerCalled = true
		_, _ = io.ReadAll(c.Request.Body)
		c.Next()
	})
	router.POST(commandPlanPathPrefix+"preview", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		var decoded map[string]any
		if err != nil || len(body) == 0 || jsonUnmarshal(body, &decoded) != nil {
			commandPlanError(c, http.StatusBadRequest, "invalid_request")
			return
		}
		serviceCalled = true
		c.Status(http.StatusOK)
	})

	// An unknown-length request resembles chunked transfer.
	payload := `{"value":"` + strings.Repeat("x", maxCommandPlanBody*2) + `"}`
	req := &http.Request{
		Method:        http.MethodPost,
		URL:           mustParseRequestURL(t, commandPlanPathPrefix+"preview"),
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(payload)),
		ContentLength: -1,
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), `"error":"invalid_request"`) {
		t.Fatalf("response=%d %s, want fixed 413 invalid_request", response.Code, response.Body.String())
	}
	if loggerCalled || serviceCalled {
		t.Fatalf("logger/service ran for unknown-length oversized request: logger=%t service=%t", loggerCalled, serviceCalled)
	}
}

func TestBrokerCommandPlanBodyLimitsReplaysValidBodyToLoggerAndHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(BrokerCommandPlanBodyLimits())
	const wantBody = `{"value":"你好"}`
	var loggerBody, handlerBody []byte
	router.Use(func(c *gin.Context) {
		loggerBody, _ = io.ReadAll(c.Request.Body)
		c.Request.Body = io.NopCloser(bytes.NewReader(loggerBody))
		c.Next()
	})
	router.POST(commandPlanPathPrefix+"preview", func(c *gin.Context) {
		handlerBody, _ = io.ReadAll(c.Request.Body)
		c.Status(http.StatusOK)
	})
	req := &http.Request{
		Method:        http.MethodPost,
		URL:           mustParseRequestURL(t, commandPlanPathPrefix+"preview"),
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(wantBody)),
		ContentLength: -1,
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("response=%d body=%s", response.Code, response.Body.String())
	}
	if string(loggerBody) != wantBody || string(handlerBody) != wantBody {
		t.Fatalf("logger body=%q handler body=%q; want both %q", loggerBody, handlerBody, wantBody)
	}
}

func TestBrokerCommandPlanBodyLimitsLeavesOtherRequestsUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(BrokerCommandPlanBodyLimits())
	var readErr error
	var readBytes int
	router.POST("/api/v1/command/datas/plans-other/preview", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		readErr, readBytes = err, len(body)
		c.Status(http.StatusNoContent)
	})
	body := bytes.Repeat([]byte("x"), maxCommandPlanBody+1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/command/datas/plans-other/preview", bytes.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent || readErr != nil || readBytes != len(body) {
		t.Fatalf("outside request response=%d read=%d err=%v; want full passthrough", response.Code, readBytes, readErr)
	}
}

func jsonUnmarshal(body []byte, target any) error {
	return json.Unmarshal(body, target)
}

func mustParseRequestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
