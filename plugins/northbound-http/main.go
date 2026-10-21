// Command northbound-plugin-http is the official HTTP sink for the ThingsPanel
// northbound service.
//
// It is a normal HTTP server that implements northbound-connector/v1. It owns
// only the parts that are specific to talking to an HTTP endpoint: request
// shape, authentication headers, status interpretation. Queueing, retry policy,
// credentials at rest and tenant isolation stay in the platform.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/thingspanel/northbound-sdk-go"
)

func main() {
	var (
		port  = flag.Int("port", 18081, "listen port")
		host  = flag.String("host", "127.0.0.1", "listen address; set 0.0.0.0 inside a container")
		token = flag.String("token", os.Getenv("NB_PLUGIN_TOKEN"), "platform bearer token (required)")
	)
	flag.Parse()
	logger := log.New(os.Stdout, "sink-http ", log.LstdFlags|log.LUTC)
	if strings.TrimSpace(*token) == "" {
		// Fail closed: a connector without a token would let anyone who can
		// reach the port push data into the customer's system.
		logger.Fatal("refusing to start: -token or NB_PLUGIN_TOKEN is required")
	}
	sink := newHTTPSink(logger)
	srv := &http.Server{
		// The default is loopback so a bare-process install is not reachable
		// from the network by accident. A container must pass -host 0.0.0.0:
		// publishing a port forwards to the container's eth0, so a listener on
		// the container's own loopback is unreachable from outside.
		Addr:              fmt.Sprintf("%s:%d", *host, *port),
		Handler:           northbound.NewServer(sink, northbound.ServerOptions{Token: *token, Logger: logger}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		logger.Printf("listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("serve: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	logger.Print("stopped")
}

// httpConfig is the tenant-owned destination configuration. It never contains
// credentials: those arrive separately and are never written to disk here.
type httpConfig struct {
	URL               string            `json:"url"`
	Method            string            `json:"method"`
	Batch             *bool             `json:"batch"`
	TimeoutMS         int               `json:"timeoutMs"`
	IdempotencyHeader string            `json:"idempotencyHeader"`
	Headers           map[string]string `json:"headers"`
	ContentType       string            `json:"contentType"`
	// WrapKey, when set, wraps the batch as {"<key>": [...]}. Some targets
	// reject a bare array.
	WrapKey string `json:"wrapKey"`
}

type httpInstance struct {
	config  httpConfig
	secrets map[string]string
	client  *http.Client
}

type httpSink struct {
	logger    *log.Logger
	mu        sync.RWMutex
	instances map[string]*httpInstance
}

func newHTTPSink(logger *log.Logger) *httpSink {
	return &httpSink{logger: logger, instances: map[string]*httpInstance{}}
}

var configSchema = json.RawMessage(`{
  "type": "object",
  "required": ["url"],
  "properties": {
    "url": {"type": "string", "format": "uri", "title": "目标地址", "description": "接收数据的 HTTP 接口完整地址"},
    "method": {"type": "string", "enum": ["POST", "PUT"], "default": "POST", "title": "请求方法"},
    "batch": {"type": "boolean", "default": true, "title": "批量发送", "description": "关闭时每条数据单独请求"},
    "timeoutMs": {"type": "integer", "minimum": 500, "maximum": 60000, "default": 10000, "title": "超时(毫秒)"},
    "idempotencyHeader": {"type": "string", "title": "幂等头名称", "description": "目标支持幂等时填写，例如 Idempotency-Key"},
    "contentType": {"type": "string", "default": "application/json", "title": "Content-Type"},
    "wrapKey": {"type": "string", "title": "批量包裹字段", "description": "填写后批量数据放在该字段下"},
    "headers": {"type": "object", "additionalProperties": {"type": "string"}, "title": "附加请求头"}
  }
}`)

func (s *httpSink) Describe() northbound.Descriptor {
	return northbound.Descriptor{
		ContractVersion: northbound.ContractVersion,
		ID:              "io.thingspanel.sink.http",
		Version:         "1.0.0",
		DisplayName:     "HTTP 数据出口",
		Description:     "把选中的数据以 JSON 推送到客户指定的 HTTP 接口，按目标响应判定结果。",
		Capabilities: northbound.Capabilities{
			EventTypes:     []string{"telemetry", "attribute", "event"},
			Batch:          true,
			AckLevel:       northbound.AckDestinationAcked,
			Idempotency:    "conditional",
			Verify:         false,
			MaxBatchEvents: 200,
		},
		ConfigSchema: configSchema,
		SecretFields: []string{"token", "basicUser", "basicPassword"},
		Limits: northbound.Limits{
			MaxBatchEvents:  200,
			MaxPayloadBytes: 4 << 20,
			MaxInFlight:     4,
		},
	}
}

func parseConfig(raw json.RawMessage) (httpConfig, error) {
	var c httpConfig
	if len(raw) == 0 {
		return c, errors.New("配置为空")
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("配置格式不正确: %w", err)
	}
	c.URL = strings.TrimSpace(c.URL)
	if c.URL == "" {
		return c, errors.New("必须填写目标地址")
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, errors.New("目标地址必须是完整的 http/https 地址")
	}
	if c.Method == "" {
		c.Method = http.MethodPost
	}
	if c.Method != http.MethodPost && c.Method != http.MethodPut {
		return c, errors.New("请求方法只支持 POST 或 PUT")
	}
	if c.TimeoutMS <= 0 {
		c.TimeoutMS = 10000
	}
	if c.ContentType == "" {
		c.ContentType = "application/json"
	}
	return c, nil
}

func (s *httpSink) Validate(ctx context.Context, raw json.RawMessage, secrets map[string]string) northbound.ValidationResult {
	checks := []northbound.Check{}
	cfg, err := parseConfig(raw)
	if err != nil {
		return northbound.ValidationResult{OK: false, Message: err.Error(), Errors: []northbound.Check{{Name: "config", OK: false, Message: err.Error()}}}
	}
	checks = append(checks, northbound.Check{Name: "config", OK: true, Message: "目标地址格式正确"})
	if cfg.IdempotencyHeader != "" && len(cfg.IdempotencyHeader) > 64 {
		checks = append(checks, northbound.Check{Name: "idempotency", OK: false, Message: "幂等头名称过长"})
	}
	if u, err := url.Parse(cfg.URL); err == nil && u.Scheme == "http" {
		checks = append(checks, northbound.Check{Name: "tls", OK: true, Message: "使用明文 HTTP，请确认目标在内网"})
	}
	ok := true
	for _, c := range checks {
		if !c.OK {
			ok = false
		}
	}
	return northbound.ValidationResult{OK: ok, Errors: checks, Message: firstFailure(checks)}
}

func (s *httpSink) Open(ctx context.Context, req northbound.OpenRequest) (northbound.OpenResponse, error) {
	cfg, err := parseConfig(req.Config)
	if err != nil {
		return northbound.OpenResponse{}, err
	}
	transport := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     60 * time.Second,
	}
	inst := &httpInstance{
		config:  cfg,
		secrets: req.Secrets,
		client:  &http.Client{Timeout: time.Duration(cfg.TimeoutMS) * time.Millisecond, Transport: transport},
	}
	s.mu.Lock()
	s.instances[req.ConnectionID] = inst
	s.mu.Unlock()
	return northbound.OpenResponse{InstanceID: req.ConnectionID, OK: true}, nil
}

func (s *httpSink) instance(id string) (*httpInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inst, ok := s.instances[id]
	if !ok {
		return nil, errors.New("连接尚未打开")
	}
	return inst, nil
}

func (s *httpSink) Close(ctx context.Context, instanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst, ok := s.instances[instanceID]; ok {
		if tr, ok := inst.client.Transport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
		delete(s.instances, instanceID)
	}
	return nil
}

func (s *httpSink) Health(ctx context.Context) error { return nil }

func (s *httpSink) Write(ctx context.Context, req northbound.WriteRequest) ([]northbound.WriteResult, error) {
	inst, err := s.instance(req.ConnectionID)
	if err != nil {
		return nil, err
	}
	batch := inst.config.Batch == nil || *inst.config.Batch
	if batch {
		return s.writeBatch(ctx, inst, req.Items), nil
	}
	results := make([]northbound.WriteResult, 0, len(req.Items))
	for _, item := range req.Items {
		results = append(results, s.send(ctx, inst, []northbound.WriteItem{item})...)
	}
	return results, nil
}

func (s *httpSink) writeBatch(ctx context.Context, inst *httpInstance, items []northbound.WriteItem) []northbound.WriteResult {
	return s.send(ctx, inst, items)
}

// send performs one HTTP request for the given items and maps the response to
// per-item results. Every input item gets exactly one result.
func (s *httpSink) send(ctx context.Context, inst *httpInstance, items []northbound.WriteItem) []northbound.WriteResult {
	cfg := inst.config
	var body []byte
	var err error
	if cfg.WrapKey != "" {
		body, err = json.Marshal(map[string]any{cfg.WrapKey: items})
	} else if len(items) == 1 {
		body, err = json.Marshal(items[0])
	} else {
		body, err = json.Marshal(map[string]any{"items": items})
	}
	if err != nil {
		return failure(items, northbound.StatusPermanent, "encode_failed", err.Error(), false)
	}
	httpReq, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.URL, strings.NewReader(string(body)))
	if err != nil {
		return failure(items, northbound.StatusPermanent, "request_invalid", err.Error(), false)
	}
	httpReq.Header.Set("Content-Type", cfg.ContentType)
	for k, v := range cfg.Headers {
		httpReq.Header.Set(k, v)
	}
	if token := inst.secrets["token"]; token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	if user := inst.secrets["basicUser"]; user != "" {
		httpReq.SetBasicAuth(user, inst.secrets["basicPassword"])
	}
	if cfg.IdempotencyHeader != "" && len(items) > 0 {
		// One request carries one idempotency key: the first item's. A batch is
		// only used when the target treats the request as one unit.
		httpReq.Header.Set(cfg.IdempotencyHeader, items[0].IdempotencyKey)
	}

	resp, err := inst.client.Do(httpReq)
	if err != nil {
		if isTimeout(err) {
			// The request may have reached the target. Retrying blindly could
			// duplicate a write, so the outcome is reported as unknown.
			return failure(items, northbound.StatusUnknown, "target_result_unknown",
				"请求超时，目标是否已写入未知", false)
		}
		safe := cfg.IdempotencyHeader != ""
		return failure(items, northbound.StatusRetryable, "target_unreachable", err.Error(), safe)
	}
	defer resp.Body.Close()
	excerpt := make([]byte, 0)
	if resp.Body != nil {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		excerpt = buf[:n]
	}
	receipt, _ := json.Marshal(map[string]any{
		"status": resp.StatusCode,
		"body":   truncate(string(excerpt), 200),
	})
	status := resp.StatusCode
	switch {
	case status >= 200 && status < 300:
		out := make([]northbound.WriteResult, 0, len(items))
		for _, it := range items {
			out = append(out, northbound.WriteResult{
				DeliveryID: it.DeliveryID, Status: northbound.StatusDelivered,
				AckLevel: northbound.AckDestinationAcked, Receipt: receipt,
			})
		}
		return out
	case status == 408 || status == 425 || status == 429 || status >= 500:
		results := failure(items, northbound.StatusRetryable, fmt.Sprintf("http_%d", status), truncate(string(excerpt), 200), true)
		if ra := retryAfter(resp.Header.Get("Retry-After")); ra > 0 {
			for i := range results {
				results[i].RetryAfterMS = ra.Milliseconds()
			}
		}
		return results
	case status == 401 || status == 403:
		return failure(items, northbound.StatusPermanent, "authentication_failed", "目标拒绝了凭证", false)
	case status == 400 || status == 404 || status == 405 || status == 415 || status == 422:
		return failure(items, northbound.StatusPermanent, fmt.Sprintf("http_%d", status), truncate(string(excerpt), 200), false)
	default:
		return failure(items, northbound.StatusUnknown, fmt.Sprintf("http_%d", status), truncate(string(excerpt), 200), false)
	}
}

func failure(items []northbound.WriteItem, status, code, detail string, safe bool) []northbound.WriteResult {
	out := make([]northbound.WriteResult, 0, len(items))
	for _, it := range items {
		s := safe
		out = append(out, northbound.WriteResult{
			DeliveryID: it.DeliveryID, Status: status, ErrorCode: code,
			ErrorDetail: detail, SafeToRetry: &s,
		})
	}
	return out
}

func isTimeout(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func retryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if secs, err := time.ParseDuration(raw + "s"); err == nil && secs > 0 && secs < time.Hour {
		return secs
	}
	if t, err := http.ParseTime(raw); err == nil {
		if d := time.Until(t); d > 0 && d < time.Hour {
			return d
		}
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func firstFailure(checks []northbound.Check) string {
	for _, c := range checks {
		if !c.OK {
			return c.Message
		}
	}
	return "配置可用"
}
