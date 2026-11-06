package ports

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SourceCredentialProvider supplies short-lived, tenant- and purpose-scoped leases.
type SourceCredentialProvider interface {
	Credential(context.Context, string, string) (string, error)
	Invalidate(string, string, string)
}

type sourceCredentialLease struct {
	Credential   string    `json:"credential"`
	ExpiresAt    time.Time `json:"expiresAt"`
	TenantID     string    `json:"tenantId"`
	DeploymentID string    `json:"deploymentId"`
	Purpose      string    `json:"purpose"`
}
type sourceCredentialClient struct {
	base, deployment, identity string
	client                     *http.Client
	mu                         sync.Mutex
	leases                     map[string]sourceCredentialLease
	pending                    map[string]chan struct{}
	now                        func() time.Time
}

func newSourceCredentialClient(base, deployment, identity string, client *http.Client) (*sourceCredentialClient, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !validLeaseScope(deployment) || len(identity) < 32 || len(identity) > 8192 || client == nil || strings.ContainsAny(identity, " \r\n\t") {
		return nil, errors.New("notification service identity unavailable")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return nil, errors.New("notification identity transport requires TLS")
	}
	safeClient := *client
	if safeClient.Timeout <= 0 {
		safeClient.Timeout = 10 * time.Second
	}
	if safeClient.Transport == nil {
		safeClient.Transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	if transport, ok := safeClient.Transport.(*http.Transport); ok {
		transport = transport.Clone()
		transport.Proxy = nil
		safeClient.Transport = transport
	}
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &sourceCredentialClient{base: strings.TrimRight(base, "/"), deployment: deployment, identity: identity, client: &safeClient, leases: map[string]sourceCredentialLease{}, pending: map[string]chan struct{}{}, now: time.Now}, nil
}

func (c *sourceCredentialClient) Credential(ctx context.Context, tenant, purpose string) (string, error) {
	if c == nil || !validLeaseScope(tenant) || (purpose != "source" && purpose != "projection") {
		return "", errors.New("notification credential scope unavailable")
	}
	key := tenant + "\x00" + purpose
	for {
		c.mu.Lock()
		if lease, ok := c.leases[key]; ok && lease.ExpiresAt.After(c.now().Add(time.Minute)) {
			c.mu.Unlock()
			return lease.Credential, nil
		}
		if pending, ok := c.pending[key]; ok {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		c.pending[key] = pending
		c.mu.Unlock()
		lease, err := c.fetch(ctx, tenant, purpose)
		c.mu.Lock()
		if err == nil {
			if len(c.leases) >= 2048 {
				for old := range c.leases {
					delete(c.leases, old)
					break
				}
			}
			c.leases[key] = lease
		}
		delete(c.pending, key)
		close(pending)
		c.mu.Unlock()
		if err != nil {
			return "", err
		}
		return lease.Credential, nil
	}
}

func (c *sourceCredentialClient) Invalidate(tenant, purpose, token string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := tenant + "\x00" + purpose
	if c.leases[key].Credential == token {
		delete(c.leases, key)
	}
}

func (c *sourceCredentialClient) fetch(ctx context.Context, tenant, purpose string) (sourceCredentialLease, error) {
	var empty sourceCredentialLease
	body, _ := json.Marshal(map[string]string{"tenantId": tenant, "purpose": purpose})
	request, err := http.NewRequestWithContext(ctx, "POST", c.base+"/internal/notification/source-credentials", bytes.NewReader(body))
	if err != nil {
		return empty, err
	}
	request.Header.Set("Authorization", "Bearer "+c.identity)
	request.Header.Set("X-Notification-Deployment", c.deployment)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return empty, errors.New("notification credential service unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return empty, errors.New("notification credential authorization rejected")
	}
	var envelope struct {
		Code int                   `json:"code"`
		Data sourceCredentialLease `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16385))
	if decoder.Decode(&envelope) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return empty, errors.New("notification credential response invalid")
	}
	lease := envelope.Data
	if envelope.Code != 200 || lease.TenantID != tenant || lease.DeploymentID != c.deployment || lease.Purpose != purpose || len(lease.Credential) < 32 || strings.ContainsAny(lease.Credential, " \t\n\r") || !lease.ExpiresAt.After(c.now().Add(time.Minute)) || lease.ExpiresAt.After(c.now().Add(20*time.Minute)) {
		return empty, errors.New("notification credential scope mismatch")
	}
	return lease, nil
}

func validLeaseScope(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:-", c)) {
			return false
		}
	}
	return true
}
