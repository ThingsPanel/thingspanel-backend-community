// Package authorization is the dictionary host's narrow adapter to the current
// Community identity/Casbin owner. It stores no users, policies, or grants.
package authorization

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encore.app/platformconfig/app"
)

const ProtocolVersion = 1
const Path = "/internal/platformconfig/authorize"

// Resources are exact legacy route identities, including templates for dynamic
// IDs. Deployment must verify their current policy registration before cutover.
// Missing current g2/p/g policy is a denial, never a blanket grant.
func Resource(operation app.Operation) string {
	switch operation {
	case app.CreateColumn:
		return "api/v1/dict/column"
	case app.CreateLanguage:
		return "api/v1/dict/language"
	case app.ReadEnum:
		return "api/v1/dict/enum"
	case app.ReadPage:
		return "api/v1/dict"
	case app.ReadLanguages, app.DeleteLanguage:
		return "api/v1/dict/language/:id"
	case app.DeleteColumn:
		return "api/v1/dict/column/:id"
	case app.ReadProtocols:
		return "api/v1/dict/protocol/service"
	default:
		return ""
	}
}

type Request struct {
	ProtocolVersion int           `json:"protocolVersion"`
	TenantID        string        `json:"tenantId"`
	SubjectID       string        `json:"subjectId"`
	Operation       app.Operation `json:"operation"`
}

type Response struct {
	ProtocolVersion int           `json:"protocolVersion"`
	Authorized      bool          `json:"authorized"`
	TenantID        string        `json:"tenantId"`
	SubjectID       string        `json:"subjectId"`
	Operation       app.Operation `json:"operation"`
	Resource        string        `json:"resource"`
}

type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

func New(baseURL, token string) (*Client, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.RawFragment != "" || base.Opaque != "" || (base.Path != "" && base.Path != "/") || len(token) < 32 || strings.TrimSpace(token) != token {
		return nil, app.ErrUnavailable
	}
	base.Path = Path
	base.RawPath = ""
	return &Client{endpoint: base.String(), token: token, http: &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Authorize(ctx context.Context, principal app.Principal, operation app.Operation) error {
	resource := Resource(operation)
	if ctx == nil || c == nil || c.http == nil || resource == "" || !app.CompleteIdentity(principal) {
		return app.ErrUnavailable
	}
	input := Request{ProtocolVersion: ProtocolVersion, TenantID: principal.TenantID, SubjectID: principal.SubjectID, Operation: operation}
	body, err := json.Marshal(input)
	if err != nil {
		return app.ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return app.ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TP-Service-Token", c.token)
	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: current dictionary permission could not be confirmed", app.ErrUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return app.ErrUnauthorized
	}
	if response.StatusCode != http.StatusOK {
		return app.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16*1024+1))
	if err != nil || len(raw) > 16*1024 {
		return app.ErrUnavailable
	}
	var result Response
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return app.ErrUnavailable
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return app.ErrUnavailable
	}
	// Empty tenant is a legitimate platform identity, so it must still be an
	// explicit string echo. An omitted/null field is not evidence of agreement.
	var identityEcho struct {
		TenantID *string `json:"tenantId"`
	}
	if json.Unmarshal(raw, &identityEcho) != nil || identityEcho.TenantID == nil {
		return app.ErrUnavailable
	}
	if result.ProtocolVersion != ProtocolVersion || !result.Authorized || result.TenantID != input.TenantID || result.SubjectID != input.SubjectID || result.Operation != operation || result.Resource != resource {
		return app.ErrUnauthorized
	}
	return nil
}
