package northbound

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Plugin wire contract, northbound-connector/v1. The plugin is an independent
// process; it never receives a database connection, the platform secret key, or
// another tenant's data.
const (
	pathDescribe = "/v1/describe"
	pathValidate = "/v1/validate"
	pathOpen     = "/v1/open"
	pathWrite    = "/v1/write"
	pathClose    = "/v1/close"
	pathHealth   = "/v1/health"
)

// WriteItem is one delivery presented to a plugin. `IdempotencyKey` is stable
// across retries so a target that supports idempotency can deduplicate.
type WriteItem struct {
	DeliveryID     string          `json:"deliveryId"`
	EventID        string          `json:"eventId"`
	IdempotencyKey string          `json:"idempotencyKey"`
	EventType      string          `json:"eventType"`
	Subject        string          `json:"subject"`
	OccurredAt     time.Time       `json:"occurredAt"`
	DeviceID       string          `json:"deviceId"`
	Data           json.RawMessage `json:"data"`
	Serialization  json.RawMessage `json:"serialization,omitempty"`
	// Headers carries the fields the target needs to place the record, e.g. an
	// HTTP idempotency header name. It never carries a credential.
	Headers map[string]string `json:"headers,omitempty"`
}

type WriteRequest struct {
	ContractVersion string      `json:"contractVersion"`
	ConnectionID    string      `json:"connectionId"`
	InstanceID      string      `json:"instanceId"`
	PipelineID      string      `json:"pipelineId"`
	Attempt         int         `json:"attempt"`
	Items           []WriteItem `json:"items"`
}

type WriteResult struct {
	DeliveryID   string          `json:"deliveryId"`
	Status       string          `json:"status"`
	AckLevel     string          `json:"ackLevel,omitempty"`
	Receipt      json.RawMessage `json:"receipt,omitempty"`
	ErrorCode    string          `json:"errorCode,omitempty"`
	ErrorDetail  string          `json:"errorDetail,omitempty"`
	SafeToRetry  *bool           `json:"safeToRetry,omitempty"`
	RetryAfterMS int64           `json:"retryAfterMs,omitempty"`
}

type WriteResponse struct {
	ContractVersion string        `json:"contractVersion"`
	Results         []WriteResult `json:"results"`
}

type OpenRequest struct {
	ContractVersion string            `json:"contractVersion"`
	ConnectionID    string            `json:"connectionId"`
	Config          json.RawMessage   `json:"config"`
	Secrets         map[string]string `json:"secrets"`
}

type OpenResponse struct {
	InstanceID string `json:"instanceId"`
	OK         bool   `json:"ok"`
	Message    string `json:"message,omitempty"`
}

type ValidateRequest struct {
	ContractVersion string            `json:"contractVersion"`
	Config          json.RawMessage   `json:"config"`
	Secrets         map[string]string `json:"secrets"`
}

type ValidateResponse struct {
	OK      bool    `json:"ok"`
	Message string  `json:"message,omitempty"`
	Errors  []Check `json:"errors,omitempty"`
}

// pluginErrorKind lets the caller distinguish "retry later" from "fix the
// configuration" without parsing strings.
type pluginErrorKind int

const (
	pluginUnreachable pluginErrorKind = iota
	pluginTimeout
	pluginRejected
	pluginProtocol
)

type pluginError struct {
	kind pluginErrorKind
	msg  string
}

func (e *pluginError) Error() string { return e.msg }

func pluginCall(ctx context.Context, inst connectorConfig, method, path string, body any, out any) error {
	if strings.TrimSpace(inst.Endpoint) == "" {
		return &pluginError{kind: pluginUnreachable, msg: "plugin endpoint is not configured for " + inst.ID}
	}
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return &pluginError{kind: pluginProtocol, msg: "encode plugin request: " + err.Error()}
		}
		payload = bytes.NewReader(raw)
	}
	timeout := 20 * time.Second
	if path == pathDescribe || path == pathHealth {
		timeout = 5 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, method, strings.TrimRight(inst.Endpoint, "/")+path, payload)
	if err != nil {
		return &pluginError{kind: pluginProtocol, msg: "build plugin request: " + err.Error()}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Northbound-Contract", ContractVersion)
	if inst.Token != "" {
		// Plugin-internal authentication: the plugin refuses requests without
		// this bearer token, so an exposed plugin port is not an open relay.
		req.Header.Set("Authorization", "Bearer "+inst.Token)
	}
	resp, err := pluginHTTPClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || callCtx.Err() == context.DeadlineExceeded {
			return &pluginError{kind: pluginTimeout, msg: "plugin " + inst.ID + " timed out"}
		}
		return &pluginError{kind: pluginUnreachable, msg: "plugin " + inst.ID + " unreachable"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return &pluginError{kind: pluginProtocol, msg: "read plugin response: " + err.Error()}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &pluginError{kind: pluginRejected, msg: "plugin " + inst.ID + " rejected the platform credential"}
	}
	if resp.StatusCode >= 400 {
		return &pluginError{kind: pluginRejected, msg: fmt.Sprintf("plugin %s returned %d: %s", inst.ID, resp.StatusCode, redact(string(raw)))}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return &pluginError{kind: pluginProtocol, msg: "decode plugin response: " + err.Error()}
		}
	}
	return nil
}

var pluginHTTPClient = &http.Client{
	// The per-call context owns the deadline; the client timeout is a backstop.
	Timeout: 45 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     60 * time.Second,
	},
}

func describePlugin(ctx context.Context, inst connectorConfig) (*Descriptor, error) {
	var out Descriptor
	if err := pluginCall(ctx, inst, http.MethodGet, pathDescribe, nil, &out); err != nil {
		return nil, err
	}
	if out.ContractVersion != ContractVersion {
		return nil, &pluginError{kind: pluginProtocol, msg: "plugin " + inst.ID + " speaks " + out.ContractVersion}
	}
	if out.ID != inst.ID {
		return nil, &pluginError{kind: pluginProtocol, msg: "plugin identity mismatch: " + out.ID + " != " + inst.ID}
	}
	return &out, nil
}

func validatePlugin(ctx context.Context, inst connectorConfig, config json.RawMessage, secrets map[string]string) (*ValidateResponse, error) {
	var out ValidateResponse
	err := pluginCall(ctx, inst, http.MethodPost, pathValidate, ValidateRequest{
		ContractVersion: ContractVersion, Config: config, Secrets: secrets,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func openPlugin(ctx context.Context, inst connectorConfig, connectionID string, config json.RawMessage, secrets map[string]string) (*OpenResponse, error) {
	var out OpenResponse
	err := pluginCall(ctx, inst, http.MethodPost, pathOpen, OpenRequest{
		ContractVersion: ContractVersion, ConnectionID: connectionID, Config: config, Secrets: secrets,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func closePlugin(ctx context.Context, inst connectorConfig, connectionID, instanceID string) error {
	return pluginCall(ctx, inst, http.MethodPost, pathClose, map[string]string{
		"contractVersion": ContractVersion, "connectionId": connectionID, "instanceId": instanceID,
	}, nil)
}

func healthPlugin(ctx context.Context, inst connectorConfig) error {
	return pluginCall(ctx, inst, http.MethodGet, pathHealth, nil, nil)
}
