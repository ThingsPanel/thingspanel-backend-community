// Package uplinkcontract reads immutable published contracts for the trusted
// data owner. Mutable Community device/config bindings are never cached here.
package uplinkcontract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"project/internal/brokersemantic"
)

var ErrUnavailable = errors.New("published uplink contract unavailable")

type Pins struct {
	TenantID   string `json:"-"`
	ModelID    string `json:"model_id"`
	SnapshotID string `json:"snapshot_id"`
	Revision   uint64 `json:"revision"`
}

func (p Pins) valid() bool {
	return p.Revision > 0 && validPin(p.TenantID, 256) && validPin(p.ModelID, 128) && validPin(p.SnapshotID, 128)
}
func validPin(s string, max int) bool {
	return s != "" && len(s) <= max && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\r\n")
}

type Config struct{ BaseURL, Token, Deployment string }
type Client struct {
	base, token, deployment string
	http                    *http.Client
	cache                   *contractCache
}

func NewClient(config Config) (*Client, error) {
	u, err := url.Parse(config.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(strings.TrimSpace(config.Token)) < 32 || !validPin(config.Deployment, 256) {
		return nil, ErrUnavailable
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, ErrUnavailable
		}
	}
	u.Path = ""
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 2 * time.Second, MaxIdleConns: 4, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second}
	c := &Client{base: u.String(), token: strings.TrimSpace(config.Token), deployment: config.Deployment, http: &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	c.cache = newContractCache(c.load, 64, 8<<20, 4)
	return c, nil
}

// Contract performs no database work and must be called before acquiring
// device/config write fences. Cache identity is private to this owner client.
func (c *Client) Contract(ctx context.Context, pins Pins) (*brokersemantic.UplinkContract, error) {
	if c == nil || c.cache == nil || ctx == nil || !pins.valid() {
		return nil, ErrUnavailable
	}
	return c.cache.get(ctx, pins)
}
func (c *Client) CloseIdleConnections() {
	if c != nil && c.http != nil {
		c.http.CloseIdleConnections()
	}
}

func (c *Client) load(ctx context.Context, pins Pins) (*brokersemantic.UplinkContract, error) {
	body, _ := json.Marshal(pins)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/internal/thingmodel/uplink-contract", bytes.NewReader(body))
	if err != nil {
		return nil, ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("X-Automation-Deployment", c.deployment)
	request.Header.Set("X-Automation-Tenant", pins.TenantID)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, ErrUnavailable
	}
	var result struct {
		SchemaVersion  int             `json:"schema_version"`
		TenantID       string          `json:"tenant_id"`
		ModelID        string          `json:"model_id"`
		SnapshotID     string          `json:"snapshot_id"`
		Revision       uint64          `json:"revision"`
		ContractSHA256 string          `json:"contract_sha256"`
		Model          json.RawMessage `json:"model"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&result) != nil || d.Decode(new(any)) != io.EOF || result.SchemaVersion != 1 || result.TenantID != pins.TenantID || result.ModelID != pins.ModelID || result.SnapshotID != pins.SnapshotID || result.Revision != pins.Revision {
		return nil, ErrUnavailable
	}
	checksum := sha256.Sum256(result.Model)
	if result.ContractSHA256 != hex.EncodeToString(checksum[:]) {
		return nil, ErrUnavailable
	}
	contract, err := brokersemantic.CompileUplinkContract(result.Model)
	if err != nil {
		return nil, ErrUnavailable
	}
	return contract, nil
}
