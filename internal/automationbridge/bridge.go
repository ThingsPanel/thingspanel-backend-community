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
	"os"
	"strings"
	"time"
)

const TenantHeader = "X-Automation-Tenant"
const DeploymentHeader = "X-Automation-Deployment"

func NativeEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_MODE")), "native")
}

func Deployment() string { return strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_DEPLOYMENT_ID")) }

func Authenticate(r *http.Request) (string, error) {
	token := strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_TOKEN"))
	if len(token) < 32 || Deployment() == "" {
		return "", errors.New("automation service credentials are not configured")
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 || r.Header.Get(DeploymentHeader) != Deployment() {
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
	url := strings.TrimRight(strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_BASE_URL")), "/")
	token := strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_TOKEN"))
	if url == "" || len(token) < 32 || Deployment() == "" {
		return nil, errors.New("native automation requires service URL, service token and deployment ID")
	}
	return &Client{url: url, token: token, deployment: Deployment(), http: &http.Client{Timeout: 5 * time.Second}}, nil
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
