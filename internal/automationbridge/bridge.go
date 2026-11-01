// Package automationbridge defines the owner-to-owner automation transport.
package automationbridge

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const TenantHeader = "X-Automation-Tenant"
const DeploymentHeader = "X-Automation-Deployment"

type Config struct{ Mode, BaseURL, Token, Deployment string }

var configMu sync.RWMutex
var frozenConfig *Config

// ReadConfig freezes the first valid configuration for every entry point.
// An invalid attempt does not freeze state and may be retried after correction.
func ReadConfig() (Config, error) {
	configMu.Lock()
	defer configMu.Unlock()
	if frozenConfig != nil {
		return *frozenConfig, nil
	}
	c, err := parseConfig()
	if err != nil {
		return Config{}, err
	}
	frozenConfig = &c
	return c, nil
}

func parseConfig() (Config, error) {
	c := Config{Mode: strings.ToLower(strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_MODE"))), BaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_BASE_URL")), "/"), Token: strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_TOKEN")), Deployment: strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_DEPLOYMENT_ID"))}
	if c.Mode == "" {
		c.Mode = "legacy"
	}
	if c.Mode != "legacy" && c.Mode != "native" {
		return Config{}, errors.New("invalid automation mode")
	}
	if c.Mode == "native" {
		if err := validateNative(c); err != nil {
			return Config{}, err
		}
	}
	return c, nil
}

func validateNative(c Config) error {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid native automation service URL")
	}
	if len(c.Token) < 32 || c.Deployment == "" || !headerSafe(c.Token) || !headerSafe(c.Deployment) {
		return errors.New("native automation requires service token and deployment ID")
	}
	return nil
}

func headerSafe(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// Initialize freezes one valid configuration before application initialization.
func Initialize() error {
	_, err := ReadConfig()
	return err
}

func NativeEnabled() bool {
	c, err := ReadConfig()
	return err == nil && c.Mode == "native"
}

func Deployment() string {
	c, err := ReadConfig()
	if err != nil {
		return ""
	}
	return c.Deployment
}

func Authenticate(r *http.Request) (string, error) {
	c, err := ReadConfig()
	if err != nil {
		return "", err
	}
	token, deployment := c.Token, c.Deployment
	if len(token) < 32 || deployment == "" {
		return "", errors.New("automation service credentials are not configured")
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 || r.Header.Get(DeploymentHeader) != deployment {
		return "", errors.New("invalid automation service identity")
	}
	tenant := strings.TrimSpace(r.Header.Get(TenantHeader))
	if tenant == "" || len(tenant) > 256 {
		return "", errors.New("automation tenant is required")
	}
	return tenant, nil
}

type Uplink struct {
	TenantID   string `json:"-"`
	Class      string `json:"class"`
	DeviceID   string `json:"device_id"`
	MessageID  string `json:"message_id"`
	ObservedAt string `json:"observed_at"`
	Values     string `json:"values"`
	Source     string `json:"source"`
}

type Client struct {
	url, token, deployment string
	http                   *http.Client
}

func NewClient() (*Client, error) {
	c, err := ReadConfig()
	if err != nil {
		return nil, err
	}
	if err := validateNative(c); err != nil {
		return nil, err
	}
	return &Client{url: c.BaseURL, token: c.Token, deployment: c.Deployment, http: &http.Client{Timeout: 5 * time.Second}}, nil
}

func (c *Client) Deliver(ctx context.Context, event Uplink) error {
	if event.TenantID == "" {
		return errors.New("missing trusted tenant")
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/internal/automation/uplink", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set(TenantHeader, event.TenantID)
	req.Header.Set(DeploymentHeader, c.deployment)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("native ingress HTTP %d", resp.StatusCode)
	}
	var receipt struct {
		Class      string `json:"class"`
		Accepted   int    `json:"accepted"`
		Duplicates int    `json:"duplicates"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&receipt) != nil || receipt.Class != event.Class || receipt.Accepted < 0 || receipt.Duplicates < 0 {
		return errors.New("native ingress returned an invalid acceptance receipt")
	}
	return nil
}
