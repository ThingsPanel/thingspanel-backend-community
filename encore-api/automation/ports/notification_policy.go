package ports

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// NotificationPolicyResolver translates a community selection at event time.
// The caller persists this result; delivery retries must never resolve it again.
type NotificationPolicyResolver interface {
	ResolvePolicy(ctx context.Context, tenantID, aliasID string) (NotificationPolicy, error)
}

type NotificationPolicy struct {
	Configured    bool   `json:"configured"`
	PolicyID      string `json:"policy_id"`
	GroupRevision int64  `json:"group_revision"`
}

func (h *HTTPNotificationOwner) ResolvePolicy(ctx context.Context, tenantID, aliasID string) (NotificationPolicy, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var out NotificationPolicy
	if h.cfg.CommunityBaseURL == "" || h.cfg.SourceReaderToken == "" || h.cfg.DeploymentID == "" {
		return out, ErrNotConfigured
	}
	u := h.cfg.CommunityBaseURL + "/api/v1/notification/source-policies/" + url.PathEscape(tenantID)
	if aliasID != "" {
		u += "?alias=" + url.QueryEscape(aliasID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+h.cfg.SourceReaderToken)
	req.Header.Set("X-Notification-Deployment", h.cfg.DeploymentID)
	resp, err := h.client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("notification policy resolution returned %d", resp.StatusCode)
	}
	var wire struct {
		Configured    *bool  `json:"configured"`
		PolicyID      string `json:"policy_id"`
		GroupRevision int64  `json:"group_revision"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 16<<10))
	if err := decoder.Decode(&wire); err != nil {
		return out, err
	}
	if wire.Configured == nil || (aliasID != "" && !*wire.Configured) || (*wire.Configured && (wire.PolicyID == "" || wire.GroupRevision <= 0)) {
		return out, fmt.Errorf("invalid notification policy snapshot")
	}
	return NotificationPolicy{Configured: *wire.Configured, PolicyID: wire.PolicyID, GroupRevision: wire.GroupRevision}, nil
}
