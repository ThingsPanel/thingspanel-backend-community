package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	maxInputBytes    = 256 << 10
	maxToolInput     = 128 << 10
	maxResponseBytes = 64 << 10
)

var allowedOperations = map[string]struct{}{
	"devices.describe": {}, "telemetry.query": {}, "commands.preview": {}, "commands.getResult": {},
}

var allowedErrorCategories = map[string]struct{}{
	"invalid_request": {}, "invalid_argument": {}, "not_authorized": {}, "budget_exceeded": {},
	"unsupported_capability": {}, "not_found": {}, "stale_state": {}, "temporarily_unavailable": {},
}

type toolRequest struct {
	Token     string          `json:"token"`
	Operation string          `json:"operation"`
	Input     json.RawMessage `json:"input"`
}

type resultEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	_ = stderr // Intentionally never emit raw input, token, URL, response, or transport errors.
	fs := flag.NewFlagSet("tp-broker-tools-client", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	origin := fs.String("origin", "", "fixed HTTPS API origin")
	caFile := fs.String("ca-file", "", "append trusted CA certificate")
	developmentHTTP := fs.Bool("development-http", false, "allow HTTP only for loopback IP literals")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *origin == "" {
		return emit(stdout, resultEnvelope{OK: false, Error: "invalid_request"})
	}
	target, err := parseOrigin(*origin, *developmentHTTP)
	if err != nil {
		return emit(stdout, resultEnvelope{OK: false, Error: "invalid_request"})
	}
	requestBytes, err := io.ReadAll(io.LimitReader(stdin, maxInputBytes+1))
	if err != nil || len(requestBytes) == 0 || len(requestBytes) > maxInputBytes || validateUniqueJSON(requestBytes) != nil {
		return emit(stdout, resultEnvelope{OK: false, Error: "invalid_request"})
	}
	var input toolRequest
	decoder := json.NewDecoder(bytes.NewReader(requestBytes))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.Token == "" || input.Operation == "" || input.Input == nil {
		return emit(stdout, resultEnvelope{OK: false, Error: "invalid_request"})
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return emit(stdout, resultEnvelope{OK: false, Error: "invalid_request"})
	}
	if _, ok := allowedOperations[input.Operation]; !ok || !validOpaqueToken(input.Token) || len(input.Input) == 0 || len(input.Input) > maxToolInput || validateUniqueJSON(input.Input) != nil || !jsonObject(input.Input) || !validToolInputShape(input.Operation, input.Input) || bytes.Contains(input.Input, []byte(input.Token)) {
		return emit(stdout, resultEnvelope{OK: false, Error: "invalid_request"})
	}
	transport, err := newTransport(*caFile)
	if err != nil {
		return emit(stdout, resultEnvelope{OK: false, Error: "invalid_request"})
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer transport.CloseIdleConnections()
	data, category, err := doRequest(ctx, client, target, input)
	if err != nil {
		return emit(stdout, resultEnvelope{OK: false, Error: "temporarily_unavailable"})
	}
	if category != "" {
		return emit(stdout, resultEnvelope{OK: false, Error: category})
	}
	return emit(stdout, resultEnvelope{OK: true, Data: data})
}

func emit(w io.Writer, result resultEnvelope) int {
	encoded, err := json.Marshal(result)
	if err != nil {
		encoded = []byte(`{"ok":false,"error":"temporarily_unavailable"}`)
	}
	encoded = append(encoded, '\n')
	if _, err := w.Write(encoded); err != nil {
		return 1
	}
	if result.OK {
		return 0
	}
	return 1
}

func parseOrigin(raw string, developmentHTTP bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.Host == "" || u.Hostname() == "" {
		return "", errors.New("invalid origin")
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("invalid origin port")
	}
	switch u.Scheme {
	case "https":
		if developmentHTTP {
			return "", errors.New("development HTTP flag only applies to HTTP")
		}
	case "http":
		ip := net.ParseIP(u.Hostname())
		if !developmentHTTP || ip == nil || !ip.IsLoopback() {
			return "", errors.New("HTTP requires explicit loopback development mode")
		}
	default:
		return "", errors.New("HTTPS origin required")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func validOpaqueToken(token string) bool {
	if len(token) != 48 || !strings.HasPrefix(token, "tpai_") {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "tpai_"))
	return err == nil && len(decoded) == 32
}

func validToolInputShape(operation string, raw []byte) bool {
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil {
		return false
	}
	var allowed map[string]struct{}
	switch operation {
	case "devices.describe":
		allowed = fieldSet("device_id")
	case "telemetry.query":
		allowed = fieldSet("device_id", "window")
	case "commands.preview":
		allowed = fieldSet("device_id", "command")
	case "commands.getResult":
		allowed = fieldSet("device_id", "message_id")
	default:
		return false
	}
	if !onlyFields(input, allowed) {
		return false
	}
	for nested, fields := range map[string]map[string]struct{}{
		"window":  fieldSet("start", "end", "keys", "limit"),
		"command": fieldSet("device_id", "value", "identify", "idempotency_key", "expires_at"),
	} {
		value, exists := input[nested]
		if !exists {
			continue
		}
		if !jsonObject(value) {
			return false
		}
		var child map[string]json.RawMessage
		if json.Unmarshal(value, &child) != nil || !onlyFields(child, fields) {
			return false
		}
	}
	return true
}

func fieldSet(fields ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	return set
}

func onlyFields(value map[string]json.RawMessage, allowed map[string]struct{}) bool {
	for field := range value {
		if _, ok := allowed[field]; !ok {
			return false
		}
	}
	return true
}

func newTransport(caFile string) (*http.Transport, error) {
	var roots *x509.CertPool
	if caFile != "" {
		var err error
		roots, err = x509.SystemCertPool()
		if err != nil || roots == nil {
			return nil, errors.New("system root store unavailable")
		}
		pem, err := os.ReadFile(caFile)
		if err != nil || !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("invalid CA file")
		}
	}
	dialer := &net.Dialer{Timeout: time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                 nil, // Never consult HTTP_PROXY/HTTPS_PROXY or user proxy settings.
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		TLSHandshakeTimeout:   2 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
		ExpectContinueTimeout: 250 * time.Millisecond,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
	}, nil
}

func doRequest(ctx context.Context, client *http.Client, origin string, input toolRequest) (json.RawMessage, string, error) {
	endpoint := origin + "/api/v1/broker-ai/tools/" + input.Operation
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(input.Input))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+input.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes || validateUniqueJSON(body) != nil || !jsonObject(body) || bytes.Contains(body, []byte(input.Token)) {
		return nil, "", errors.New("invalid upstream response")
	}
	if resp.StatusCode == http.StatusOK {
		var envelope struct {
			Operation string          `json:"operation"`
			TenantID  string          `json:"tenant_id"`
			SubjectID string          `json:"subject_id"`
			DeviceID  string          `json:"device_id"`
			Data      json.RawMessage `json:"data"`
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		expectedDevice, ok := requestDeviceID(input.Input)
		if dec.Decode(&envelope) != nil || envelope.Operation != input.Operation || !ok || envelope.DeviceID != expectedDevice || !boundedIdentity(envelope.TenantID) || !boundedIdentity(envelope.SubjectID) || len(envelope.Data) == 0 || validateUniqueJSON(envelope.Data) != nil || !jsonObject(envelope.Data) {
			return nil, "", errors.New("invalid success response")
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return nil, "", errors.New("trailing success response")
		}
		return append(json.RawMessage(nil), body...), "", nil
	}
	var envelope struct {
		Error string `json:"error"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(&envelope) != nil || envelope.Error == "" {
		return nil, "", errors.New("invalid upstream error")
	}
	if _, ok := allowedErrorCategories[envelope.Error]; !ok {
		return nil, "", errors.New("unknown upstream error category")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return nil, "", errors.New("trailing upstream error")
	}
	return nil, envelope.Error, nil
}

func boundedIdentity(value string) bool {
	return value != "" && len(value) <= 36 && strings.TrimSpace(value) == value
}

func requestDeviceID(input json.RawMessage) (string, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(input, &fields) != nil {
		return "", false
	}
	var deviceID string
	err := json.Unmarshal(fields["device_id"], &deviceID)
	return deviceID, err == nil && deviceID != ""
}

func jsonObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}

func validateUniqueJSON(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := scanJSONValue(dec); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func scanJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("duplicate JSON field")
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
	case '[':
		for dec.More() {
			if err := scanJSONValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	return nil
}
