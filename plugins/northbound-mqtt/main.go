// Command northbound-plugin-mqtt is the official MQTT sink for the ThingsPanel
// northbound service.
//
// This connector deliberately does NOT use the Go SDK: it implements the
// northbound-connector/v1 HTTP contract directly. That is the cheapest way to
// keep the contract honest — if the wire format were only usable through one
// helper library, it would not be a contract.
package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const contractVersion = "northbound-connector/v1"

// Outcome values from the contract.
const (
	statusDelivered = "delivered"
	statusRetryable = "retryable_failure"
	statusPermanent = "permanent_failure"
	statusUnknown   = "unknown"
)

func main() {
	var (
		port  = flag.Int("port", 18082, "listen port")
		host  = flag.String("host", "127.0.0.1", "listen address; set 0.0.0.0 inside a container")
		token = flag.String("token", os.Getenv("NB_PLUGIN_TOKEN"), "platform bearer token (required)")
	)
	flag.Parse()
	logger := log.New(os.Stdout, "sink-mqtt ", log.LstdFlags|log.LUTC)
	if strings.TrimSpace(*token) == "" {
		logger.Fatal("refusing to start: -token or NB_PLUGIN_TOKEN is required")
	}
	p := newMQTTSink(logger)
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("Authorization")
			if len(got) > 7 && strings.EqualFold(got[:7], "bearer ") {
				got = got[7:]
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(*token)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid platform credential"})
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("/v1/describe", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, p.describe())
	}))
	mux.HandleFunc("/v1/health", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	mux.HandleFunc("/v1/validate", auth(p.handleValidate))
	mux.HandleFunc("/v1/open", auth(p.handleOpen))
	mux.HandleFunc("/v1/write", auth(p.handleWrite))
	mux.HandleFunc("/v1/close", auth(p.handleClose))

	srv := &http.Server{
		// Loopback by default; a container must pass -host 0.0.0.0 because a
		// published port forwards to eth0, not to the container's loopback.
		Addr:              fmt.Sprintf("%s:%d", *host, *port),
		Handler:           mux,
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
	p.closeAll()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	logger.Print("stopped")
}

// ---------------------------------------------------------------------------
// Contract types (declared locally on purpose: no shared implementation)
// ---------------------------------------------------------------------------

type descriptor struct {
	ContractVersion string          `json:"contractVersion"`
	ID              string          `json:"id"`
	Version         string          `json:"version"`
	DisplayName     string          `json:"displayName"`
	Description     string          `json:"description,omitempty"`
	Capabilities    capabilities    `json:"capabilities"`
	ConfigSchema    json.RawMessage `json:"configSchema"`
	SecretFields    []string        `json:"secretFields"`
	Limits          limits          `json:"limits"`
}

type capabilities struct {
	EventTypes     []string `json:"eventTypes"`
	Batch          bool     `json:"batch"`
	AckLevel       string   `json:"ackLevel"`
	Idempotency    string   `json:"idempotency"`
	Verify         bool     `json:"verify"`
	MaxBatchEvents int      `json:"maxBatchEvents"`
}

type limits struct {
	MaxBatchEvents  int `json:"maxBatchEvents"`
	MaxPayloadBytes int `json:"maxPayloadBytes"`
	MaxInFlight     int `json:"maxInFlight"`
}

type writeItem struct {
	DeliveryID     string          `json:"deliveryId"`
	EventID        string          `json:"eventId"`
	IdempotencyKey string          `json:"idempotencyKey"`
	EventType      string          `json:"eventType"`
	Subject        string          `json:"subject"`
	OccurredAt     time.Time       `json:"occurredAt"`
	DeviceID       string          `json:"deviceId"`
	Data           json.RawMessage `json:"data"`
	Serialization  json.RawMessage `json:"serialization,omitempty"`
}

type writeRequest struct {
	ContractVersion string      `json:"contractVersion"`
	ConnectionID    string      `json:"connectionId"`
	InstanceID      string      `json:"instanceId"`
	PipelineID      string      `json:"pipelineId"`
	Attempt         int         `json:"attempt"`
	Items           []writeItem `json:"items"`
}

type writeResult struct {
	DeliveryID   string          `json:"deliveryId"`
	Status       string          `json:"status"`
	AckLevel     string          `json:"ackLevel,omitempty"`
	Receipt      json.RawMessage `json:"receipt,omitempty"`
	ErrorCode    string          `json:"errorCode,omitempty"`
	ErrorDetail  string          `json:"errorDetail,omitempty"`
	SafeToRetry  *bool           `json:"safeToRetry,omitempty"`
	RetryAfterMS int64           `json:"retryAfterMs,omitempty"`
}

type writeResponse struct {
	ContractVersion string        `json:"contractVersion"`
	Results         []writeResult `json:"results"`
}

// ---------------------------------------------------------------------------
// Sink
// ---------------------------------------------------------------------------

type mqttConfig struct {
	BrokerURL      string `json:"brokerUrl"`
	TopicTemplate  string `json:"topicTemplate"`
	QoS            int    `json:"qos"`
	Retain         bool   `json:"retain"`
	ClientIDPrefix string `json:"clientIdPrefix"`
	KeepAliveSec   int    `json:"keepAliveSec"`
	ConnectTimeout int    `json:"connectTimeoutMs"`
	PublishTimeout int    `json:"publishTimeoutMs"`
	// UseTLS is implied by an ssl:// or tls:// URL.
}

type instance struct {
	cfg    mqttConfig
	client mqtt.Client
}

type mqttSink struct {
	logger    *log.Logger
	mu        sync.Mutex
	instances map[string]*instance
}

func newMQTTSink(logger *log.Logger) *mqttSink {
	return &mqttSink{logger: logger, instances: map[string]*instance{}}
}

var configSchema = json.RawMessage(`{
  "type": "object",
  "required": ["brokerUrl", "topicTemplate"],
  "properties": {
    "brokerUrl": {"type": "string", "title": "Broker 地址", "description": "例如 tcp://127.0.0.1:1883 或 ssl://mq.example.com:8883"},
    "topicTemplate": {"type": "string", "default": "northbound/{deviceId}/{eventType}", "title": "主题模板", "description": "可用变量：{deviceId} {eventType} {subject}"},
    "qos": {"type": "integer", "enum": [0, 1, 2], "default": 1, "title": "QoS", "description": "QoS 0 没有 Broker 确认，平台会显示为未核验"},
    "retain": {"type": "boolean", "default": false, "title": "保留消息"},
    "clientIdPrefix": {"type": "string", "default": "tp-nb", "title": "客户端 ID 前缀"},
    "keepAliveSec": {"type": "integer", "default": 30, "title": "心跳(秒)"},
    "connectTimeoutMs": {"type": "integer", "default": 5000, "title": "连接超时(毫秒)"},
    "publishTimeoutMs": {"type": "integer", "default": 8000, "title": "发布超时(毫秒)"}
  }
}`)

func (s *mqttSink) describe() descriptor {
	return descriptor{
		ContractVersion: contractVersion,
		ID:              "io.thingspanel.sink.mqtt",
		Version:         "1.0.0",
		DisplayName:     "MQTT 数据出口",
		Description:     "把选中的数据发布到客户指定的 MQTT Broker 主题，按 Broker 确认判定结果。",
		Capabilities: capabilities{
			EventTypes:     []string{"telemetry", "attribute", "event"},
			Batch:          false,
			AckLevel:       "destination_acked",
			Idempotency:    "none",
			Verify:         false,
			MaxBatchEvents: 1,
		},
		ConfigSchema: configSchema,
		SecretFields: []string{"username", "password"},
		Limits:       limits{MaxBatchEvents: 1, MaxPayloadBytes: 256 << 10, MaxInFlight: 4},
	}
}

func parseConfig(raw json.RawMessage) (mqttConfig, error) {
	var c mqttConfig
	if len(raw) == 0 {
		return c, errors.New("配置为空")
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("配置格式不正确: %w", err)
	}
	c.BrokerURL = strings.TrimSpace(c.BrokerURL)
	c.TopicTemplate = strings.TrimSpace(c.TopicTemplate)
	if c.BrokerURL == "" {
		return c, errors.New("必须填写 Broker 地址")
	}
	scheme := strings.ToLower(strings.SplitN(c.BrokerURL, "://", 2)[0])
	switch scheme {
	case "tcp", "ssl", "tls", "ws", "wss":
	default:
		return c, errors.New("Broker 地址必须以 tcp:// ssl:// ws:// wss:// 开头")
	}
	if c.TopicTemplate == "" {
		return c, errors.New("必须填写主题模板")
	}
	if strings.ContainsAny(c.TopicTemplate, "+#") {
		return c, errors.New("主题模板不能包含通配符 + 或 #")
	}
	if c.QoS < 0 || c.QoS > 2 {
		return c, errors.New("QoS 只能是 0、1 或 2")
	}
	if c.KeepAliveSec <= 0 {
		c.KeepAliveSec = 30
	}
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = 5000
	}
	if c.PublishTimeout <= 0 {
		c.PublishTimeout = 8000
	}
	if c.ClientIDPrefix == "" {
		c.ClientIDPrefix = "tp-nb"
	}
	return c, nil
}

func (s *mqttSink) handleValidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ContractVersion string            `json:"contractVersion"`
		Config          json.RawMessage   `json:"config"`
		Secrets         map[string]string `json:"secrets"`
	}
	if !decode(w, r, &req) {
		return
	}
	cfg, err := parseConfig(req.Config)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error(),
			"errors": []map[string]any{{"name": "config", "ok": false, "message": err.Error()}}})
		return
	}
	checks := []map[string]any{{"name": "config", "ok": true, "message": "配置格式正确"}}
	// A real connect proves the credential, not just the syntax. It is closed
	// immediately so validation never leaves a live subscription behind.
	opts := buildOptions(cfg, req.Secrets, "validate")
	client := mqtt.NewClient(opts)
	tok := client.Connect()
	if !tok.WaitTimeout(time.Duration(cfg.ConnectTimeout)*time.Millisecond) || tok.Error() != nil {
		msg := "无法连接到 Broker"
		if tok.Error() != nil {
			msg = tok.Error().Error()
		}
		client.Disconnect(0)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": msg,
			"errors": append(checks, map[string]any{"name": "broker", "ok": false, "message": msg})})
		return
	}
	client.Disconnect(100)
	checks = append(checks, map[string]any{"name": "broker", "ok": true, "message": "连接成功"})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "配置可用", "errors": checks})
}

func (s *mqttSink) handleOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ContractVersion string            `json:"contractVersion"`
		ConnectionID    string            `json:"connectionId"`
		Config          json.RawMessage   `json:"config"`
		Secrets         map[string]string `json:"secrets"`
	}
	if !decode(w, r, &req) {
		return
	}
	cfg, err := parseConfig(req.Config)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	s.mu.Lock()
	old := s.instances[req.ConnectionID]
	s.mu.Unlock()
	if old != nil {
		old.client.Disconnect(100)
	}
	client := mqtt.NewClient(buildOptions(cfg, req.Secrets, req.ConnectionID))
	tok := client.Connect()
	if !tok.WaitTimeout(time.Duration(cfg.ConnectTimeout)*time.Millisecond) || tok.Error() != nil {
		msg := "无法连接到 Broker"
		if tok.Error() != nil {
			msg = tok.Error().Error()
		}
		client.Disconnect(0)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": msg})
		return
	}
	s.mu.Lock()
	s.instances[req.ConnectionID] = &instance{cfg: cfg, client: client}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "instanceId": req.ConnectionID})
}

func buildOptions(cfg mqttConfig, secrets map[string]string, suffix string) *mqtt.ClientOptions {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.BrokerURL)
	opts.SetClientID(fmt.Sprintf("%s-%s-%d", cfg.ClientIDPrefix, sanitizeID(suffix), time.Now().UnixNano()%100000))
	opts.SetKeepAlive(time.Duration(cfg.KeepAliveSec) * time.Second)
	opts.SetConnectTimeout(time.Duration(cfg.ConnectTimeout) * time.Millisecond)
	opts.SetAutoReconnect(false)
	opts.SetCleanSession(true)
	if u := secrets["username"]; u != "" {
		opts.SetUsername(u)
		opts.SetPassword(secrets["password"])
	}
	if strings.HasPrefix(cfg.BrokerURL, "ssl://") || strings.HasPrefix(cfg.BrokerURL, "tls://") {
		opts.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	return opts
}

func (s *mqttSink) handleWrite(w http.ResponseWriter, r *http.Request) {
	var req writeRequest
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	inst := s.instances[req.ConnectionID]
	s.mu.Unlock()
	if inst == nil {
		out := make([]writeResult, 0, len(req.Items))
		for _, it := range req.Items {
			out = append(out, writeResult{DeliveryID: it.DeliveryID, Status: statusPermanent,
				ErrorCode: "connection_not_open", ErrorDetail: "连接尚未打开"})
		}
		writeJSON(w, http.StatusOK, writeResponse{ContractVersion: contractVersion, Results: out})
		return
	}
	results := make([]writeResult, 0, len(req.Items))
	for _, it := range req.Items {
		results = append(results, publishItem(inst, it))
	}
	writeJSON(w, http.StatusOK, writeResponse{ContractVersion: contractVersion, Results: results})
}

func publishItem(inst *instance, it writeItem) writeResult {
	topic := renderTopic(inst.cfg.TopicTemplate, it)
	// paho only accepts []byte or string in Publish and type-switches on the
	// dynamic type, so a json.RawMessage (a named []byte type) is rejected with
	// "unknown payload type". Convert explicitly.
	payload := []byte(it.Data)
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	// The wire contract keeps the platform envelope; the MQTT topic carries
	// routing. Targets that need a different body shape should use an HTTP sink.
	tok := inst.client.Publish(topic, byte(inst.cfg.QoS), inst.cfg.Retain, payload)
	if !tok.WaitTimeout(time.Duration(inst.cfg.PublishTimeout) * time.Millisecond) {
		safe := false
		return writeResult{DeliveryID: it.DeliveryID, Status: statusUnknown,
			ErrorCode: "publish_timeout", ErrorDetail: "发布未在超时内完成，Broker 是否已接收未知",
			SafeToRetry: &safe}
	}
	if err := tok.Error(); err != nil {
		safe := true
		code := "publish_failed"
		if strings.Contains(strings.ToLower(err.Error()), "not connected") {
			code = "broker_disconnected"
		}
		return writeResult{DeliveryID: it.DeliveryID, Status: statusRetryable,
			ErrorCode: code, ErrorDetail: err.Error(), SafeToRetry: &safe}
	}
	ackLevel := "destination_acked"
	note := "Broker 已确认接收"
	if inst.cfg.QoS == 0 {
		ackLevel = "none"
		note = "QoS 0 没有 Broker 确认"
	}
	receipt, _ := json.Marshal(map[string]any{"topic": topic, "qos": inst.cfg.QoS, "note": note})
	return writeResult{DeliveryID: it.DeliveryID, Status: statusDelivered, AckLevel: ackLevel, Receipt: receipt}
}

func renderTopic(template string, it writeItem) string {
	subject := it.Subject
	if subject == "" {
		subject = "devices/" + it.DeviceID
	}
	out := strings.NewReplacer(
		"{deviceId}", sanitizeTopicSegment(it.DeviceID),
		"{eventType}", sanitizeTopicSegment(it.EventType),
		"{subject}", sanitizeTopicSegment(subject),
		"{eventId}", sanitizeTopicSegment(it.EventID),
	).Replace(template)
	return strings.TrimLeft(out, "/")
}

// sanitizeTopicSegment keeps a device id from breaking the topic structure.
func sanitizeTopicSegment(v string) string {
	if v == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '+', '#', '\x00':
			return '_'
		}
		return r
	}, v)
}

func sanitizeID(v string) string {
	if v == "" {
		return "x"
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, v)
}

func (s *mqttSink) handleClose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceID string `json:"instanceId"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	inst := s.instances[req.InstanceID]
	delete(s.instances, req.InstanceID)
	s.mu.Unlock()
	if inst != nil {
		inst.client.Disconnect(250)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *mqttSink) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, inst := range s.instances {
		inst.client.Disconnect(100)
		delete(s.instances, id)
	}
}

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil || len(raw) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unreadable body"})
		return false
	}
	if err := json.Unmarshal(raw, out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
