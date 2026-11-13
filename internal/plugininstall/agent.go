package plugininstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

var (
	ErrAgentOffline  = errors.New("agent is not reachable")
	ErrAgentConflict = errors.New("agent refused: state conflict") // HTTP 409: stale/revoked/not granted ...
	ErrAgentRejected = errors.New("agent rejected the request")
)

// FieldSpec describes one site-specific value of a plugin (catalog env_user). It never carries a value.
type FieldSpec struct {
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
	Description string `json:"description"`
	Validate    string `json:"validate,omitempty"`
	Pattern     string `json:"pattern,omitempty"`
	Default     string `json:"default,omitempty"`
}

type HealthSpec struct {
	Port int    `json:"port"`
	Path string `json:"path"`
}

type ServicePluginSpec struct {
	Name        string            `json:"name"`
	ServiceType int32             `json:"service_type"`
	Description string            `json:"description"`
	Config      map[string]string `json:"config"`
}

// CatalogEntry is the public projection of a signed catalog entry, served by the Agent after it verified the catalog.
type CatalogEntry struct {
	ID                string               `json:"id"`
	DisplayName       string               `json:"display_name"`
	Version           string               `json:"version"`
	Description       string               `json:"description"`
	ServiceIdentifier string               `json:"service_identifier"`
	NetworkAlias      string               `json:"network_alias"`
	HTTPPort          int                  `json:"http_port"`
	Health            *HealthSpec          `json:"health,omitempty"`
	ServicePlugin     *ServicePluginSpec   `json:"service_plugin,omitempty"`
	EnvUser           map[string]FieldSpec `json:"env_user"`
	// ImagePresent is the Agent's answer to "is the pinned image on this host"; nil when it did not say.
	ImagePresent *bool `json:"image_present,omitempty"`
}

// PrepareRequest carries the only inputs the Agent accepts to build a container: the catalog id, non-secret site
// values and the one-shot identity token. Secrets are already in the Agent's own store (PutSecret).
type PrepareRequest struct {
	Instance          string
	Generation        uint64
	RequestKey        string
	Plugin            string
	Values            map[string]string
	IdentityToken     string
	ServiceIdentifier string
}

// Agent is the complete allow-list of operations the backend may ask of the host Agent. There is no exec, no free
// profile, no image and no mount parameter.
type Agent interface {
	Catalog(ctx context.Context) ([]CatalogEntry, error)
	PutSecret(ctx context.Context, instance, key, value string) error
	SecretKeys(ctx context.Context, instance string) ([]string, error)
	ClearConfig(ctx context.Context, instance string) error
	AcceptGeneration(ctx context.Context, instance string, generation uint64) error
	Prepare(ctx context.Context, req PrepareRequest) error
	Grant(ctx context.Context, instance string, generation uint64, requestKey, grantID string, ttl time.Duration) error
	Activate(ctx context.Context, instance string, generation uint64, requestKey string) error
	Renew(ctx context.Context, instance string, generation uint64, requestKey, grantID string, ttl time.Duration) error
	Revoke(ctx context.Context, instance string, generation uint64, reason string) error
	Retire(ctx context.Context, instance string, generation uint64) error
	Status(ctx context.Context) error // reachability + auth
}

type wireRequest struct {
	Instance          string            `json:"instance,omitempty"`
	Generation        uint64            `json:"generation,omitempty"`
	RequestKey        string            `json:"request_key,omitempty"`
	Profile           string            `json:"profile,omitempty"`
	GrantID           string            `json:"grant_id,omitempty"`
	TTLSeconds        int64             `json:"ttl_seconds,omitempty"`
	Reason            string            `json:"reason,omitempty"`
	Key               string            `json:"key,omitempty"`
	Value             string            `json:"value,omitempty"`
	Values            map[string]string `json:"values,omitempty"`
	IdentityToken     string            `json:"identity_token,omitempty"`
	ServiceIdentifier string            `json:"service_identifier,omitempty"`
}

type wireResponse struct {
	OK      bool           `json:"ok"`
	Error   string         `json:"error,omitempty"`
	Catalog []CatalogEntry `json:"catalog,omitempty"`
	Keys    []string       `json:"keys,omitempty"`
}

// UnixAgent talks to the Agent over its private Unix socket with the bearer token read from a file on every call
// (so a rotated token is picked up). Request and response bodies are never logged.
type UnixAgent struct {
	http      *http.Client
	tokenFile func() (string, error)
}

func NewUnixAgent(socket string, tokenFile func() (string, error)) *UnixAgent {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
	}, DisableKeepAlives: true}
	return &UnixAgent{http: &http.Client{Transport: transport, Timeout: 90 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, tokenFile: tokenFile}
}

func (a *UnixAgent) call(ctx context.Context, path string, in wireRequest) (wireResponse, error) {
	token, err := a.tokenFile()
	if err != nil {
		return wireResponse{}, ErrAgentOffline
	}
	body, _ := json.Marshal(in)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent"+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.http.Do(req)
	if err != nil {
		return wireResponse{}, ErrAgentOffline
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out wireResponse
	_ = json.Unmarshal(raw, &out)
	switch {
	case res.StatusCode == http.StatusOK && out.OK:
		return out, nil
	case res.StatusCode == http.StatusConflict:
		return out, ErrAgentConflict
	case res.StatusCode >= 500:
		return out, fmt.Errorf("%w: status %d", ErrAgentOffline, res.StatusCode)
	}
	return out, fmt.Errorf("%w: status %d", ErrAgentRejected, res.StatusCode)
}

func (a *UnixAgent) Catalog(ctx context.Context) ([]CatalogEntry, error) {
	out, err := a.call(ctx, "/v1/catalog", wireRequest{})
	return out.Catalog, err
}
func (a *UnixAgent) PutSecret(ctx context.Context, instance, key, value string) error {
	_, err := a.call(ctx, "/v1/config-put", wireRequest{Instance: instance, Key: key, Value: value})
	return err
}
func (a *UnixAgent) SecretKeys(ctx context.Context, instance string) ([]string, error) {
	out, err := a.call(ctx, "/v1/config-keys", wireRequest{Instance: instance})
	return out.Keys, err
}
func (a *UnixAgent) ClearConfig(ctx context.Context, instance string) error {
	_, err := a.call(ctx, "/v1/config-clear", wireRequest{Instance: instance})
	return err
}
func (a *UnixAgent) AcceptGeneration(ctx context.Context, instance string, g uint64) error {
	_, err := a.call(ctx, "/v1/accept-generation", wireRequest{Instance: instance, Generation: g})
	return err
}
func (a *UnixAgent) Prepare(ctx context.Context, r PrepareRequest) error {
	_, err := a.call(ctx, "/v1/prepare", wireRequest{Instance: r.Instance, Generation: r.Generation, RequestKey: r.RequestKey,
		Profile: r.Plugin, Values: r.Values, IdentityToken: r.IdentityToken, ServiceIdentifier: r.ServiceIdentifier})
	return err
}
func (a *UnixAgent) Grant(ctx context.Context, instance string, g uint64, key, grantID string, ttl time.Duration) error {
	_, err := a.call(ctx, "/v1/grant", wireRequest{Instance: instance, Generation: g, RequestKey: key, GrantID: grantID, TTLSeconds: int64(ttl.Seconds())})
	return err
}
func (a *UnixAgent) Renew(ctx context.Context, instance string, g uint64, key, grantID string, ttl time.Duration) error {
	_, err := a.call(ctx, "/v1/renew", wireRequest{Instance: instance, Generation: g, RequestKey: key, GrantID: grantID, TTLSeconds: int64(ttl.Seconds())})
	return err
}
func (a *UnixAgent) Activate(ctx context.Context, instance string, g uint64, key string) error {
	_, err := a.call(ctx, "/v1/activate", wireRequest{Instance: instance, Generation: g, RequestKey: key})
	return err
}
func (a *UnixAgent) Revoke(ctx context.Context, instance string, g uint64, reason string) error {
	_, err := a.call(ctx, "/v1/revoke", wireRequest{Instance: instance, Generation: g, Reason: reason})
	return err
}
func (a *UnixAgent) Retire(ctx context.Context, instance string, g uint64) error {
	_, err := a.call(ctx, "/v1/retire", wireRequest{Instance: instance, Generation: g})
	return err
}
func (a *UnixAgent) Status(ctx context.Context) error {
	_, err := a.call(ctx, "/v1/sweep", wireRequest{})
	return err
}
