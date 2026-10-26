package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Exit codes are part of the contract: a script decides what happened from
// $? without reading a message. They are deliberately coarse — the fine
// detail is in the JSON error body.
const (
	exitOK             = 0
	exitError          = 1 // the server said no, and we cannot classify it better
	exitUsage          = 2 // the command line itself was wrong
	exitNotFound       = 3
	exitInvalid        = 4 // validation / bad input
	exitDenied         = 5 // unauthenticated or not allowed
	exitConflict       = 6 // version conflict, preview stale, already exists
	exitUnknownOutcome = 7 // the request may or may not have happened
	exitUnavailable    = 8 // the service is down or timed out
)

// fieldError is one input the server rejected. It exists so callers never
// have to parse the human message to find out which field is wrong.
type fieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// apiError is the machine-readable half of a failure.
type apiError struct {
	Code       string       `json:"code"`               // grpc-style: invalid_argument, failed_precondition…
	AppCode    string       `json:"app_code,omitempty"` // our own: invalid_definition, preview_stale…
	Message    string       `json:"message"`            // human text, never parsed
	Fields     []fieldError `json:"fields,omitempty"`   // which inputs were wrong
	HTTPStatus int          `json:"http_status,omitempty"`
	ExitCode   int          `json:"exit_code"`
	// Retryable says the same command may succeed later unchanged. It is false
	// for validation errors, because retrying a rejected body only wastes time.
	Retryable bool `json:"retryable,omitempty"`
	// Hint is the next command to run. For an unknown outcome it is the lookup
	// that answers "did it happen?", which is the only honest advice.
	Hint string `json:"hint,omitempty"`
}

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

// response is what the transport returned, success or not.
type response struct {
	status    int
	body      []byte
	requestID string
}

// call performs one HTTP request.
//
// It returns (resp, nil) when the server answered with 2xx, (resp, apiErr) when
// it answered with an error, and (nil, apiErr) when the outcome is unknown
// because the transport failed. The third case is the important one: the caller
// must not claim the action did not happen.
func (c *client) call(method, path string, query url.Values, body any,
	idem, requestID string) (*response, *apiError) {

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, &apiError{Code: "usage", Message: "请求体无法序列化: " + err.Error(),
				ExitCode: exitUsage}
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return nil, &apiError{Code: "usage", Message: err.Error(), ExitCode: exitUsage}
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
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, unknownOutcome(method, path, idem, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))

	out := &response{status: res.StatusCode, body: raw, requestID: res.Header.Get("X-Request-ID")}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return out, nil
	}
	return out, decodeError(res.StatusCode, raw, idem)
}

// unknownOutcome is the transport failure case: we do not know whether the
// server saw the request.
func unknownOutcome(method, path, idem string, err error) *apiError {
	e := &apiError{
		Code:      "outcome_unknown",
		Message:   "请求未得到响应，无法确定是否已生效: " + err.Error(),
		ExitCode:  exitUnknownOutcome,
		Retryable: true,
	}
	if method == http.MethodGet {
		// A read that failed did nothing. Saying "unknown" for a GET would train
		// people to ignore the one case that matters.
		e.Code = "unavailable"
		e.Message = "服务不可达: " + err.Error()
		e.ExitCode = exitUnavailable
	}
	if idem != "" {
		e.Hint = fmt.Sprintf("用同一幂等键重试（--idempotency-key %s），服务端会返回同一次结果", idem)
	}
	return e
}

// decodeError turns an Encore error body into our structure. When the body is
// not the shape we expect (a proxy error page, say) we still produce something
// machine-readable rather than dumping HTML at the caller.
func decodeError(status int, raw []byte, idem string) *apiError {
	var wire struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Details json.RawMessage `json:"details"`
	}
	out := &apiError{HTTPStatus: status}
	if err := json.Unmarshal(raw, &wire); err != nil || wire.Code == "" {
		out.Code = httpCodeName(status)
		out.Message = strings.TrimSpace(string(raw))
		if len(out.Message) > 400 {
			out.Message = out.Message[:400]
		}
		out.ExitCode = exitForCode(out.Code, status)
		return out
	}
	out.Code = wire.Code
	out.Message = wire.Message
	if len(wire.Details) > 0 {
		var details struct {
			Code   string          `json:"code"`
			Fields []fieldError    `json:"fields"`
			Extra  json.RawMessage `json:"-"`
		}
		if json.Unmarshal(wire.Details, &details) == nil {
			out.AppCode = details.Code
			out.Fields = details.Fields
		}
	}
	out.ExitCode = exitForCode(out.Code, status)
	out.Retryable = out.ExitCode == exitUnavailable || out.ExitCode == exitConflict
	if out.ExitCode == exitUnknownOutcome && idem != "" {
		out.Hint = "用同一幂等键重试: --idempotency-key " + idem
	}
	return out
}

func httpCodeName(status int) string {
	switch {
	case status == 400 || status == 422:
		return "invalid_argument"
	case status == 401:
		return "unauthenticated"
	case status == 403:
		return "permission_denied"
	case status == 404:
		return "not_found"
	case status == 409:
		return "aborted"
	case status == 412:
		return "failed_precondition"
	case status == 429:
		return "resource_exhausted"
	case status >= 500:
		return "unavailable"
	}
	return "unknown"
}

func exitForCode(code string, status int) int {
	switch code {
	case "not_found":
		return exitNotFound
	case "invalid_argument", "out_of_range":
		return exitInvalid
	case "unauthenticated", "permission_denied":
		return exitDenied
	case "failed_precondition", "already_exists", "aborted", "resource_exhausted":
		return exitConflict
	case "unavailable", "deadline_exceeded":
		return exitUnavailable
	case "internal":
		return exitError
	}
	return exitForCode(httpCodeName(status), status)
}

// newRequestID is a correlation id we send and echo. Encore may generate its
// own; having ours in the output means a support ticket can name one string.
func newRequestID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("tpctl-%d-%s", time.Now().UnixNano(), hex.EncodeToString(b))
}

// intentKey derives an idempotency key from the command and its body.
//
// Same intent, same key — that is the whole point. A retry after a timeout
// therefore cannot open the light twice, without the user having to remember
// anything. `--idempotency-key` still overrides it when a script wants to pin
// the key across differently-shaped invocations.
func intentKey(command string, body any) string {
	sum := sha256.New()
	sum.Write([]byte(command))
	if body != nil {
		// encoding/json sorts map keys, so the same intent produces the same
		// bytes even when the flags were given in a different order.
		raw, err := json.Marshal(body)
		if err == nil {
			sum.Write(raw)
		}
	}
	return "tpctl-" + sanitizeKey(command) + "-" + hex.EncodeToString(sum.Sum(nil))[:16]
}

func sanitizeKey(s string) string {
	return strings.NewReplacer(".", "-", "_", "-", " ", "-").Replace(s)
}

var errNoResponse = errors.New("no response")
