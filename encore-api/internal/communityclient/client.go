package communityclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Client talks to the Community backend that owns the device runtime: live and historical
// readings, commands, simulation and the device records themselves.
//
// It is configured only from the environment and has no defaults, so a deployment that does
// not configure it fails clearly instead of talking to a public demo server with a shared
// password:
//
//	TP_COMMUNITY_BASE_URL  Community base URL, e.g. http://community:9999 (required; shared with the automation service)
//	TP_COMMUNITY_API_KEY   a Community API key for this service (preferred)
//	TP_COMMUNITY_EMAIL / TP_COMMUNITY_PASSWORD  a dedicated service account, used only when no API key is set
type Client struct {
	baseURL  string
	apiKey   string
	email    string
	password string
	http     *http.Client
}

// ErrNotConfigured is returned by every call when the Community connection is not configured.
// It is not a refusal from Community, so callers treat it as "runtime unavailable".
var ErrNotConfigured = errors.New("device runtime is not configured: set TP_COMMUNITY_BASE_URL and TP_COMMUNITY_API_KEY (or TP_COMMUNITY_EMAIL and TP_COMMUNITY_PASSWORD)")

func NewFromEnv() *Client {
	return &Client{
		baseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("TP_COMMUNITY_BASE_URL")), "/"),
		apiKey:   strings.TrimSpace(os.Getenv("TP_COMMUNITY_API_KEY")),
		email:    strings.TrimSpace(os.Getenv("TP_COMMUNITY_EMAIL")),
		password: os.Getenv("TP_COMMUNITY_PASSWORD"),
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Configured reports whether a base URL and some credential are present.
func (c *Client) Configured() bool {
	return c.baseURL != "" && (c.apiKey != "" || (c.email != "" && c.password != ""))
}

type loginResp struct {
	Code int `json:"code"`
	Data struct {
		Token string `json:"token"`
	} `json:"data"`
}

// Login returns a session token. If TP_COMMUNITY_API_KEY is set it returns a sentinel
// value so callers can use the API key path instead.
func (c *Client) Login(ctx context.Context) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	if c.apiKey != "" {
		return "", nil // signal: use API key, no JWT needed
	}
	body := map[string]string{"email": c.email, "password": c.password}
	var out loginResp
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/login", "", body, &out); err != nil {
		return "", err
	}
	if out.Data.Token == "" {
		return "", fmt.Errorf("community login returned empty token")
	}
	return out.Data.Token, nil
}

// ErrRejected marks an answer from the Community runtime that definitely refused the
// request (a 4xx status or an envelope code other than 200). Any other failure
// (transport error, timeout, 5xx) leaves it unknown whether the runtime acted.
var ErrRejected = errors.New("community runtime rejected the request")

func (c *Client) doJSON(ctx context.Context, method, path, token string, reqBody any, out any) error {
	raw, err := c.doRaw(ctx, method, path, token, reqBody)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) doRaw(ctx context.Context, method, path, token string, reqBody any) (json.RawMessage, error) {
	var bodyReader *bytes.Reader
	if reqBody == nil {
		bodyReader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	} else if token != "" {
		req.Header.Set("x-token", token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusRequestTimeout {
			return nil, fmt.Errorf("%w: legacy status=%d path=%s body=%s", ErrRejected, resp.StatusCode, path, string(body))
		}
		return nil, fmt.Errorf("legacy status=%d path=%s body=%s", resp.StatusCode, path, string(body))
	}
	return body, nil
}

func i64toa(v int64) string {
	return strconv.FormatInt(v, 10)
}
