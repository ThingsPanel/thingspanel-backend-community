package automationcompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CoreClient talks to the new automation service.
//
// It forwards the caller's own token instead of minting one. That is the whole
// tenant-safety argument of this adapter: the new service derives the tenant
// from the token exactly as it does for the console, so the adapter cannot
// widen a customer's reach even if it wanted to, and there is no second place
// where "which tenant is this" is decided.
type CoreClient struct {
	base string
	http *http.Client
}

// NewCoreClient builds a client. baseURL must be absolute and without a
// trailing slash.
func NewCoreClient(baseURL string, timeout time.Duration) *CoreClient {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &CoreClient{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: timeout},
	}
}

// CoreError is a refusal from the new service.
//
// Code is the service's own vocabulary ("legacy_ref_missing", "rule_disabled",
// "version_conflict", …). The adapter turns it into an old-style business code
// for the client, but keeps it in the payload as reason_code so an operator can
// still see what actually happened.
type CoreError struct {
	Status  int
	Code    string
	Message string
	Fields  []map[string]string
}

func (e *CoreError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("core %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("core %d: %s", e.Status, e.Message)
}

// IsNotFound reports a 404 from the new service.
func (e *CoreError) IsNotFound() bool { return e != nil && e.Status == http.StatusNotFound }

// IsConflict reports a 409 (version conflict, preview stale, …).
func (e *CoreError) IsConflict() bool { return e != nil && e.Status == http.StatusConflict }

// IsUnavailable reports that the new service could not be reached, or answered
// with a gateway-class status. A 502 is not a refusal: it means the thing behind
// the proxy did not answer, which is the same customer-visible situation as a
// dial failure and must not be reported as "the engine rejected your request".
func (e *CoreError) IsUnavailable() bool {
	if e == nil {
		return false
	}
	switch e.Status {
	case 0, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Wire types. Only the fields the adapter reads are declared: a field that is
// not needed here is a field that would have to be kept in sync for no reason.
// ---------------------------------------------------------------------------

type legacyRef struct {
	LegacyID   string   `json:"legacy_id"`
	RuleID     string   `json:"rule_id"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Enabled    bool     `json:"enabled"`
	Ambiguous  bool     `json:"ambiguous"`
	Candidates []string `json:"candidates"`
}

type rule struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	Enabled        bool            `json:"enabled"`
	Revision       int             `json:"revision"`
	Version        int             `json:"version"`
	DefinitionHash string          `json:"definition_hash"`
	LegacyID       string          `json:"legacy_id"`
	Definition     json.RawMessage `json:"definition"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type rulePage struct {
	Rules []rule `json:"rules"`
	Total int    `json:"total"`
}

type runStep struct {
	ID     string `json:"id"`
	Idx    int    `json:"idx"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type run struct {
	ID         string    `json:"id"`
	RuleID     string    `json:"rule_id"`
	Status     string    `json:"status"`
	ReasonCode string    `json:"reason_code"`
	Reason     string    `json:"reason"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Steps      []runStep `json:"steps"`
}

type runPage struct {
	Runs  []run `json:"runs"`
	Total int   `json:"total"`
}

type alarm struct {
	ID           string     `json:"id"`
	PolicyID     string     `json:"policy_id"`
	PolicyKey    string     `json:"policy_key"`
	SubjectID    string     `json:"subject_id"`
	Dimension    string     `json:"dimension"`
	State        string     `json:"state"`
	Level        string     `json:"level"`
	OpenedAt     time.Time  `json:"opened_at"`
	ClearedAt    *time.Time `json:"cleared_at"`
	SampleCount  int64      `json:"sample_count"`
	LastEvidence string     `json:"last_evidence"`
}

type alarmPage struct {
	Alarms []alarm `json:"alarms"`
}

// ---------------------------------------------------------------------------
// Calls
// ---------------------------------------------------------------------------

// ResolveLegacy maps an old id onto the rule that owns it now.
func (c *CoreClient) ResolveLegacy(ctx context.Context, token, legacyID string) (*legacyRef, error) {
	var out legacyRef
	if err := c.do(ctx, token, http.MethodGet, "/v1/legacy-refs/"+url.PathEscape(legacyID), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRule reads one rule.
func (c *CoreClient) GetRule(ctx context.Context, token, id string) (*rule, error) {
	var out rule
	if err := c.do(ctx, token, http.MethodGet, "/v1/rules/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRules reads a page of rules.
func (c *CoreClient) ListRules(ctx context.Context, token, kind string, limit, offset int) (*rulePage, error) {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(offset))
	var out rulePage
	if err := c.do(ctx, token, http.MethodGet, "/v1/rules", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RunRule runs a published rule revision now.
func (c *CoreClient) RunRule(ctx context.Context, token, id, deviceID, requestID string) (*run, error) {
	body := map[string]any{}
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	// The old clients have no idea what an idempotency key is, so the adapter
	// derives one. Without it, the App's "activate" button followed by a retry
	// after a timeout would open the valve twice.
	if requestID != "" {
		body["request_id"] = requestID
	}
	var out run
	if err := c.do(ctx, token, http.MethodPost, "/v1/rules/"+url.PathEscape(id)+"/run", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetEnabled flips a rule's switch with the version it was read at.
func (c *CoreClient) SetEnabled(ctx context.Context, token, id string, enabled bool, version int) (*rule, error) {
	var out rule
	body := map[string]any{"enabled": enabled, "version": version}
	if err := c.do(ctx, token, http.MethodPost, "/v1/rules/"+url.PathEscape(id)+"/enabled", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRuns reads a page of runs of one rule.
func (c *CoreClient) ListRuns(ctx context.Context, token, ruleID string, limit int) (*runPage, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	var out runPage
	if err := c.do(ctx, token, http.MethodGet, "/v1/rules/"+url.PathEscape(ruleID)+"/runs", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAlarms reads alarm episodes.
func (c *CoreClient) ListAlarms(ctx context.Context, token, subjectID, state string, limit int) (*alarmPage, error) {
	q := url.Values{}
	if subjectID != "" {
		q.Set("subject_id", subjectID)
	}
	if state != "" {
		q.Set("state", state)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out alarmPage
	if err := c.do(ctx, token, http.MethodGet, "/v1/alarms", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAlarm reads one episode, active or cleared, so an old detail link keeps
// working after the alarm recovered.
func (c *CoreClient) GetAlarm(ctx context.Context, token, id string) (*alarm, error) {
	var out alarm
	if err := c.do(ctx, token, http.MethodGet, "/v1/alarms/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

func (c *CoreClient) do(ctx context.Context, token, method, path string, query url.Values, body, out any) error {
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	if token != "" {
		// Both headers: the App only sends x-token, the console sends both, and
		// the new service accepts either. Forwarding what arrived would mean the
		// adapter behaves differently for two clients that should be identical.
		req.Header.Set("x-token", token)
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-TP-Compat", "automationcompat")

	res, err := c.http.Do(req)
	if err != nil {
		return &CoreError{Status: 0, Code: "upstream_unreachable", Message: err.Error()}
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return &CoreError{Status: res.StatusCode, Code: "upstream_read", Message: err.Error()}
	}
	if res.StatusCode >= 400 {
		return parseCoreError(res.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &CoreError{Status: res.StatusCode, Code: "upstream_decode", Message: err.Error()}
	}
	return nil
}

// parseCoreError reads Encore's error body.
//
// Encore returns {"code","message","details"}. The business code lives in
// details.code and the field in details.field; reading only the top-level code
// would reduce every refusal to "invalid_argument", which is not something an
// operator or a client can act on.
func parseCoreError(status int, raw []byte) *CoreError {
	e := &CoreError{Status: status}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			Code   string              `json:"code"`
			Field  string              `json:"field"`
			Fields []map[string]string `json:"fields"`
		} `json:"details"`
	}
	if err := json.Unmarshal(raw, &body); err == nil {
		e.Code = body.Details.Code
		if e.Code == "" {
			e.Code = body.Code
		}
		e.Message = body.Message
		e.Fields = body.Details.Fields
		if e.Message == "" && body.Details.Field != "" {
			e.Message = body.Details.Field
		}
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(raw))
		if len(e.Message) > 200 {
			e.Message = e.Message[:200]
		}
	}
	return e
}
