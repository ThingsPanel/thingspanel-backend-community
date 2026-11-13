// Package systemagent is the platform's client for the host-side manager's system API (tp-agent, system role).
//
// The Agent listens on a Unix socket in a directory that only this process's container mounts (read-only), next
// to its bearer token. The client sends fixed actions only: nothing in a request chooses a command, image or URL.
// It has no Encore dependency so it can be tested with a plain HTTP server on a Unix socket.
package systemagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	// ErrUnavailable: the system API is not configured or not reachable (no socket, no token, Agent down).
	ErrUnavailable = errors.New("system management is not available")
	// ErrBusy: another system operation is already running.
	ErrBusy = errors.New("another system operation is running")
)

// Error is a failure the Agent reported. Code is one of its fixed codes, never free text from the stack.
type Error struct {
	Message string
	Code    string
}

func (e *Error) Error() string { return e.Message }

// Client talks to one Agent system socket.
type Client struct {
	Socket    string
	TokenFile string
	// Timeout bounds one request (default 20 s); long operations are asynchronous on the Agent side.
	Timeout time.Duration
}

// FromEnv reads TP_SYSTEM_SOCKET and TP_SYSTEM_TOKEN_FILE. Both unset gives a client that always reports
// ErrUnavailable, so a deployment without the system role keeps working.
func FromEnv() *Client {
	return &Client{Socket: os.Getenv("TP_SYSTEM_SOCKET"), TokenFile: os.Getenv("TP_SYSTEM_TOKEN_FILE")}
}

// Component is one stack component in the health result.
type Component struct {
	Name   string `json:"name"`
	Group  string `json:"group"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// Health is the Agent's health verdict.
type Health struct {
	OK         bool        `json:"ok"`
	Components []Component `json:"components"`
	CheckedAt  time.Time   `json:"checked_at"`
}

// Operation is the current or last system operation.
type Operation struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Scope     string    `json:"scope,omitempty"`
	State     string    `json:"state"` // idle | running | done | failed | aborted
	Step      string    `json:"step"`
	Percent   int       `json:"percent"`
	Component string    `json:"component,omitempty"`
	Code      string    `json:"code,omitempty"`
	Notes     []string  `json:"notes,omitempty"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Upgrade is the persisted upgrade state (phases are fixed identifiers).
type Upgrade struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Phase    string `json:"phase"`
	Snapshot string `json:"snapshot,omitempty"`
	Code     string `json:"code,omitempty"`
}

// ScopeSync is the last Broker scope synchronisation.
type ScopeSync struct {
	Outcome string `json:"outcome"`
	Code    string `json:"code,omitempty"`
}

// Check is the configuration check result.
type Check struct {
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"`
	Service string `json:"service,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Updates is the answer to "check for updates".
type Updates struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest"`
	Newer     bool      `json:"newer"`
	ValidFrom time.Time `json:"valid_from"`
	ValidTo   time.Time `json:"valid_until"`
}

// Backup is one backup package.
type Backup struct {
	Name            string    `json:"name"`
	Size            int64     `json:"size"`
	CreatedAt       time.Time `json:"created_at"`
	PlatformVersion string    `json:"platform_version"`
	Trigger         string    `json:"trigger"`
	History         bool      `json:"history"`
	Valid           bool      `json:"valid"`
}

// Response is every field the Agent may answer with.
type Response struct {
	OK           bool       `json:"ok"`
	Error        string     `json:"error"`
	Code         string     `json:"code"`
	AgentVersion string     `json:"agent_version"`
	Project      string     `json:"project"`
	Result       *Health    `json:"result"`
	Operation    *Operation `json:"operation"`
	Upgrade      *Upgrade   `json:"upgrade"`
	ScopeSync    *ScopeSync `json:"scope_sync"`
	Check        *Check     `json:"check"`
	Updates      *Updates   `json:"updates"`
	Backups      []Backup   `json:"backups"`
}

func (c *Client) http() (*http.Client, string, error) {
	if c == nil || c.Socket == "" || c.TokenFile == "" {
		return nil, "", ErrUnavailable
	}
	raw, err := os.ReadFile(c.TokenFile)
	token := strings.TrimSpace(string(raw))
	if err != nil || len(token) < 32 {
		return nil, "", ErrUnavailable
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", c.Socket)
		}}}, token, nil
}

// Call posts a fixed action (for example "status", "backup/list") with an optional JSON body.
func (c *Client) Call(ctx context.Context, action string, body map[string]any) (*Response, error) {
	client, token, err := c.http()
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(body)
	if body == nil {
		payload = []byte("{}")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent/v1/system/"+action, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, ErrUnavailable
	}
	var out Response
	if json.Unmarshal(raw, &out) != nil {
		return nil, ErrUnavailable
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnavailable // a token the Agent does not accept is as good as no access
	}
	if !out.OK {
		if strings.Contains(out.Error, "another system operation is running") {
			return nil, ErrBusy
		}
		msg := out.Error
		if msg == "" {
			msg = "system manager error"
		}
		return nil, &Error{Message: msg, Code: out.Code}
	}
	return &out, nil
}
