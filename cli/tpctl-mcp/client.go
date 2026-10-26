package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// apiError is the machine-readable half of a failure. It is the same shape the
// CLI uses, on purpose: an AI that learns one learns both.
type apiError struct {
	Code       string       `json:"code"`
	AppCode    string       `json:"app_code,omitempty"`
	Message    string       `json:"message"`
	Fields     []fieldError `json:"fields,omitempty"`
	HTTPStatus int          `json:"http_status,omitempty"`
	Retryable  bool         `json:"retryable,omitempty"`
	Hint       string       `json:"hint,omitempty"`
}

type fieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

type client struct {
	baseURL string
	token   string
	tenant  string
	http    *http.Client
}

func newClient(baseURL, token, tenant string, timeout time.Duration) *client {
	return &client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		tenant:  tenant,
		http:    &http.Client{Timeout: timeout},
	}
}

type response struct {
	status    int
	body      []byte
	requestID string
}

func (c *client) call(method, path string, query url.Values, body any, idem string) (*response, *apiError) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, &apiError{Code: "invalid_arguments", Message: "参数无法序列化: " + err.Error()}
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return nil, &apiError{Code: "invalid_arguments", Message: err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.tenant != "" {
		req.Header.Set("X-Tenant-ID", c.tenant)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}

	res, err := c.http.Do(req)
	if err != nil {
		if method == http.MethodGet {
			return nil, &apiError{Code: "unavailable", Message: "服务不可达: " + err.Error()}
		}
		// A write with no response may have happened. Telling the model "it
		// failed" is how a light gets turned on twice.
		return nil, &apiError{
			Code:      "outcome_unknown",
			Message:   "请求未得到响应，无法确定是否已生效: " + err.Error(),
			Retryable: true,
			Hint: "先用 list_runs / get_run 查询这次意图的结果；重试请沿用同一个 idempotency_key，" +
				"服务端会返回同一次结果",
		}
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	out := &response{status: res.StatusCode, body: raw, requestID: res.Header.Get("X-Request-ID")}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return out, nil
	}
	return out, decodeError(res.StatusCode, raw)
}

func decodeError(status int, raw []byte) *apiError {
	var wire struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Details json.RawMessage `json:"details"`
	}
	out := &apiError{HTTPStatus: status}
	if err := json.Unmarshal(raw, &wire); err != nil || wire.Code == "" {
		out.Code = "unavailable"
		out.Message = strings.TrimSpace(string(raw))
		if len(out.Message) > 300 {
			out.Message = out.Message[:300]
		}
		return out
	}
	out.Code = wire.Code
	out.Message = wire.Message
	if len(wire.Details) > 0 {
		var d struct {
			Code   string       `json:"code"`
			Fields []fieldError `json:"fields"`
		}
		if json.Unmarshal(wire.Details, &d) == nil {
			out.AppCode = d.Code
			out.Fields = d.Fields
		}
	}
	out.Retryable = out.Code == "unavailable" || out.Code == "aborted" || out.Code == "failed_precondition"
	return out
}

// intentKey is the idempotency key for one intent.
//
// It is derived, not random: a model that retries after a timeout with the same
// arguments produces the same key, so the retry is safe without the model
// having to remember anything. When the tool result carries the key, the model
// can also pass it back explicitly.
func intentKey(tool string, args map[string]any) string {
	raw, _ := json.Marshal(args)
	sum := sha256Hex(append([]byte(tool), raw...))
	return "mcp-" + strings.ReplaceAll(tool, "_", "-") + "-" + sum[:16]
}

// argReader is a small typed view over tool arguments.
//
// Everything is validated here rather than at the endpoint so the model gets a
// field-level answer before a request is made. It does not replace server-side
// validation — it just fails faster and more legibly.
type argReader struct {
	args map[string]any
	errs []fieldError
}

func (r *argReader) str(name string, required bool) string {
	v, ok := r.args[name]
	if !ok || v == nil {
		if required {
			r.errs = append(r.errs, fieldError{Field: name, Code: "required", Message: "缺少必填参数"})
		}
		return ""
	}
	s, ok := v.(string)
	if !ok {
		r.errs = append(r.errs, fieldError{Field: name, Code: "type", Message: "需要字符串"})
		return ""
	}
	if required && strings.TrimSpace(s) == "" {
		r.errs = append(r.errs, fieldError{Field: name, Code: "required", Message: "不能为空"})
	}
	return s
}

func (r *argReader) num(name string) int {
	v, ok := r.args[name]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			r.errs = append(r.errs, fieldError{Field: name, Code: "type", Message: "需要整数"})
			return 0
		}
		return i
	default:
		r.errs = append(r.errs, fieldError{Field: name, Code: "type", Message: "需要整数"})
		return 0
	}
}

func (r *argReader) boolean(name string) bool {
	v, ok := r.args[name]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		r.errs = append(r.errs, fieldError{Field: name, Code: "type", Message: "需要布尔值"})
		return false
	}
	return b
}

func (r *argReader) obj(name string, required bool) map[string]any {
	v, ok := r.args[name]
	if !ok || v == nil {
		if required {
			r.errs = append(r.errs, fieldError{Field: name, Code: "required", Message: "缺少必填参数"})
		}
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		r.errs = append(r.errs, fieldError{Field: name, Code: "type", Message: "需要对象"})
		return nil
	}
	return m
}

func (r *argReader) strMap(name string) map[string]string {
	v, ok := r.args[name]
	if !ok || v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		r.errs = append(r.errs, fieldError{Field: name, Code: "type", Message: "需要对象"})
		return nil
	}
	out := map[string]string{}
	for k, raw := range m {
		s, ok := raw.(string)
		if !ok {
			r.errs = append(r.errs, fieldError{Field: name + "." + k, Code: "type", Message: "需要字符串"})
			continue
		}
		out[k] = s
	}
	return out
}

func (r *argReader) strList(name string) []string {
	v, ok := r.args[name]
	if !ok || v == nil {
		return nil
	}
	switch list := v.(type) {
	case []any:
		out := make([]string, 0, len(list))
		for i, item := range list {
			s, ok := item.(string)
			if !ok {
				r.errs = append(r.errs, fieldError{
					Field: fmt.Sprintf("%s[%d]", name, i), Code: "type", Message: "需要字符串"})
				continue
			}
			out = append(out, s)
		}
		return out
	case string:
		var out []string
		for _, p := range strings.Split(list, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	r.errs = append(r.errs, fieldError{Field: name, Code: "type", Message: "需要字符串数组"})
	return nil
}

func (r *argReader) listOfObjects(name string) []any {
	v, ok := r.args[name]
	if !ok || v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		r.errs = append(r.errs, fieldError{Field: name, Code: "type", Message: "需要数组"})
		return nil
	}
	return list
}

func (r *argReader) failed() error {
	if len(r.errs) == 0 {
		return nil
	}
	return &apiError{
		Code:    "invalid_arguments",
		Message: "工具参数不合法",
		Fields:  r.errs,
	}
}

// envOr reads an environment variable with a fallback.
func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

var errNotConfigured = errors.New("TPCTL_BASE_URL is not configured")
