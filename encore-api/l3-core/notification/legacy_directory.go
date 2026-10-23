package notification

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"encore.app/l3-core/notification/contract"
)

var ErrLegacyDirectoryUnavailable = errors.New("notification legacy directory is unavailable")
var errLegacyDirectoryScopeForbidden = errors.New("notification legacy directory scope is forbidden")

// LegacyDirectoryReader holds a read-only operator credential. It never uses
// a user's session, source-ingress credential, or projection credential.
type LegacyDirectoryReader struct {
	base       string
	token      string
	deployment string
	client     *http.Client
}

func NewLegacyDirectoryReader(base, token, deployment string, options LegacySessionOptions) (*LegacyDirectoryReader, error) {
	endpoint, err := NewLegacySessionChecker(base, options)
	if err != nil || !validDirectoryPart(deployment) || len(token) < 32 || len(token) > 8192 || strings.ContainsAny(token, " \t\r\n\x00") {
		return nil, ErrLegacyDirectoryUnavailable
	}
	return &LegacyDirectoryReader{base: strings.TrimSuffix(endpoint.endpoint, "/api/v1/notification/session-context"), token: token, deployment: deployment, client: endpoint.client}, nil
}

func validDirectoryPart(value string) bool {
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, "/\\?&#\r\n\x00 \t")
}

func (c *LegacyDirectoryReader) read(ctx context.Context, path, tenant string, out any) (bool, error) {
	if c == nil || c.client == nil || !validDirectoryPart(tenant) {
		return false, ErrLegacyDirectoryUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return false, ErrLegacyDirectoryUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Notification-Deployment", c.deployment)
	req.Header.Set("Accept", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return false, ErrLegacyDirectoryUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.StatusCode == http.StatusForbidden {
		return false, errLegacyDirectoryScopeForbidden
	}
	if res.StatusCode != http.StatusOK {
		return false, ErrLegacyDirectoryUnavailable
	}
	media, _, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return false, ErrLegacyDirectoryUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 || contract.DecodeStrictJSON(body, out) != nil {
		return false, ErrLegacyDirectoryUnavailable
	}
	return true, nil
}

func (c *LegacyDirectoryReader) Exists(ctx context.Context, tenant string) (bool, error) {
	if !validDirectoryPart(tenant) {
		return false, ErrLegacyDirectoryUnavailable
	}
	var result struct {
		TenantID string `json:"tenantId"`
		Exists   bool   `json:"exists"`
	}
	found, err := c.read(ctx, "/api/v1/notification/source-tenants/"+url.PathEscape(tenant), tenant, &result)
	if errors.Is(err, errLegacyDirectoryScopeForbidden) {
		return false, nil
	}
	if err != nil || !found {
		return found, err
	}
	if result.TenantID != tenant || !result.Exists {
		return false, ErrLegacyDirectoryUnavailable
	}
	return true, nil
}

func (c *LegacyDirectoryReader) ResolveMember(ctx context.Context, tenant, user, field string) ([]contract.Recipient, error) {
	if field == "appDevice" {
		return c.resolveAppDevices(ctx, tenant, user)
	}
	if !validDirectoryPart(user) || !validDirectoryPart(tenant) || (field != "email" && field != "phone") {
		return nil, ErrLegacyDirectoryUnavailable
	}
	var result struct {
		UserID       string `json:"userId"`
		TenantID     string `json:"tenantId"`
		ContactField string `json:"contactField"`
		Address      string `json:"address"`
	}
	query := url.Values{"tenantId": {tenant}, "contactField": {field}}
	found, err := c.read(ctx, "/api/v1/notification/source-members/"+url.PathEscape(user)+"?"+query.Encode(), tenant, &result)
	if err != nil || !found || result.UserID != user || result.TenantID != tenant || result.ContactField != field || result.Address == "" || len(result.Address) > 320 || strings.TrimSpace(result.Address) != result.Address || strings.ContainsAny(result.Address, "\r\n\x00") {
		return nil, ErrLegacyDirectoryUnavailable
	}
	return []contract.Recipient{{Kind: field, Address: result.Address}}, nil
}

var _ MemberResolver = (*LegacyDirectoryReader)(nil)

func (c *LegacyDirectoryReader) resolveAppDevices(ctx context.Context, tenant, user string) ([]contract.Recipient, error) {
	if !validDirectoryPart(user) || !validDirectoryPart(tenant) {
		return nil, ErrLegacyDirectoryUnavailable
	}
	var data struct {
		TenantID  string   `json:"tenantId"`
		UserID    string   `json:"userId"`
		DeviceIDs []string `json:"deviceIds"`
	}
	found, err := c.read(ctx, "/api/v1/notification/source-app-members/"+url.PathEscape(user)+"?tenantId="+url.QueryEscape(tenant), tenant, &data)
	if err != nil || !found || data.TenantID != tenant || data.UserID != user || len(data.DeviceIDs) > 500 {
		return nil, ErrLegacyDirectoryUnavailable
	}
	result := make([]contract.Recipient, 0, len(data.DeviceIDs))
	seen := map[string]bool{}
	for _, id := range data.DeviceIDs {
		if !validDirectoryPart(id) {
			return nil, ErrLegacyDirectoryUnavailable
		}
		if !seen[id] {
			result = append(result, contract.Recipient{Kind: "user_id", Address: id})
			seen[id] = true
		}
	}
	return result, nil
}
