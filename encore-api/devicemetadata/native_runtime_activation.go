package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"encore.app/pkg/dbx"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Repair only a confirmed owned runtime projection; never infer activation from
// a configured credential or a non-empty runtime reference.
func confirmOwnedRuntimeActivation(ctx context.Context, tx dbx.Tx, tenant, device string) (time.Time, error) {
	var event, credential string
	var desired []byte
	err := tx.QueryRow(ctx, `SELECT o.id::text,c.id::text,o.desired FROM device_runtime_outbox o
 JOIN device_credentials c ON c.tenant_id=o.tenant_id AND c.device_id=o.device_id AND c.status='active'
 AND c.id::text=CASE WHEN o.desired->>'change'='rotation' THEN o.desired->>'replacementCredentialId' ELSE o.desired->>'credentialId' END
 WHERE o.tenant_id=$1 AND o.device_id=$2 AND o.kind='credential_change' AND o.state='applied'
 AND o.desired->>'change' IN ('activation','rotation') ORDER BY o.applied_at DESC,o.id DESC LIMIT 1`, tenant, device).Scan(&event, &credential, &desired)
	if err != nil {
		return time.Time{}, fmt.Errorf("applied runtime activation evidence unavailable: %w", err)
	}
	var change credentialChangeDesired
	if json.Unmarshal(desired, &change) != nil {
		return time.Time{}, fmt.Errorf("invalid applied credential event")
	}
	return requestRuntimeActivationConfirmation(ctx, tenant, device, event, change)
}
func requestRuntimeActivationConfirmation(ctx context.Context, tenant, device, event string, change credentialChangeDesired) (time.Time, error) {
	endpoint := strings.TrimRight(os.Getenv("TP_COMMUNITY_BASE_URL"), "/")
	parsed, err := url.Parse(endpoint)
	token, deployment := os.Getenv("TP_AUTOMATION_SERVICE_TOKEN"), os.Getenv("TP_SOURCE_DEPLOYMENT_ID")
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(token) < 32 || deployment == "" {
		return time.Time{}, fmt.Errorf("runtime owner identity unavailable")
	}
	payload, _ := json.Marshal(map[string]string{"deviceId": device, "eventId": event, "credentialId": change.CredentialID, "change": change.Change, "replacementCredentialId": change.ReplacementCredentialID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/internal/automation/runtime-devices/register", bytes.NewReader(payload))
	if err != nil {
		return time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Automation-Tenant", tenant)
	req.Header.Set("X-Automation-Deployment", deployment)
	req.Header.Set("X-Runtime-Confirm-Only", "true")
	client := http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, fmt.Errorf("runtime activation owner unavailable")
	}
	defer resp.Body.Close()
	var result struct {
		Code int `json:"code"`
		Data struct {
			DeviceID string     `json:"deviceId"`
			EventID  string     `json:"eventId"`
			State    string     `json:"activationState"`
			At       *time.Time `json:"activatedAt"`
		} `json:"data"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result) != nil || result.Code != 200 || result.Data.DeviceID != device || result.Data.EventID != event || result.Data.State != "active" || result.Data.At == nil || result.Data.At.IsZero() {
		return time.Time{}, fmt.Errorf("runtime activation receipt not confirmed")
	}
	return *result.Data.At, nil
}
