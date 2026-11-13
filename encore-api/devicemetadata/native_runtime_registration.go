package devicemetadata

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// registerNativeMQTTRuntime registers a runtime projection, not a second user
// device. The owner confirms the same UUID and persists its deployment ownership.
// Keep the native device locked through confirmation so deletion cannot race it.
func registerNativeMQTTRuntime(ctx context.Context, item runtimeOutboxItem, change credentialChangeDesired, voucher string) error {
	tx, err := beginTenantScopedSQLTx(ctx, item.TenantID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name, number, kind, administrativeState string
	var direct, configuration []byte
	var namespace, external, currentProvider sql.NullString
	err = tx.QueryRow(ctx, `SELECT d.name,d.device_number,d.device_kind,d.administrative_state,b.direct_access::text,t.configuration::text,b.runtime_namespace,b.runtime_external_id,b.runtime_provider_id::text
 FROM devices d JOIN device_access_bindings b ON b.tenant_id=d.tenant_id AND b.device_id=d.id
 LEFT JOIN device_access_templates t ON t.tenant_id=b.tenant_id AND t.id=b.template_id
 WHERE d.tenant_id=$1 AND d.id=$2 AND d.deleted_at IS NULL FOR UPDATE OF d,b`, item.TenantID, item.DeviceID).Scan(&name, &number, &kind, &administrativeState, &direct, &configuration, &namespace, &external, &currentProvider)
	if err != nil {
		return err
	}
	if administrativeState != "enabled" && change.Change != "revocation" {
		return fmt.Errorf("disabled native device cannot register runtime access")
	}
	if kind != "standalone" || accessProtocol(configuration, direct) != "mqtt" {
		return fmt.Errorf("runtime registration supports standalone MQTT only")
	}
	var access struct {
		Access struct {
			Mode string `json:"mode"`
		} `json:"access"`
	}
	raw := configuration
	if len(raw) == 0 {
		raw = direct
	}
	if json.Unmarshal(raw, &access) != nil || access.Access.Mode != "direct" {
		return fmt.Errorf("runtime registration requires direct MQTT")
	}
	endpoint := strings.TrimRight(os.Getenv("TP_COMMUNITY_BASE_URL"), "/")
	parsed, e := url.Parse(endpoint)
	token, deployment := os.Getenv("TP_AUTOMATION_SERVICE_TOKEN"), os.Getenv("TP_SOURCE_DEPLOYMENT_ID")
	if e != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(token) < 32 || deployment == "" {
		return fmt.Errorf("runtime owner identity unavailable")
	}
	provider := uuid.NewSHA1(uuid.NameSpaceOID, []byte("thingspanel/community/runtime/"+deployment)).String()
	if namespace.Valid || external.Valid || currentProvider.Valid {
		if namespace.String != "community" || external.String != item.DeviceID || currentProvider.String != provider {
			return fmt.Errorf("runtime identity already assigned")
		}
	}
	payload, _ := json.Marshal(map[string]string{"eventId": item.EventID, "change": change.Change, "replacementCredentialId": change.ReplacementCredentialID, "deviceId": item.DeviceID, "credentialId": change.CredentialID, "deviceNumber": number, "name": name, "voucher": voucher})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/internal/automation/runtime-devices/register", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Automation-Deployment", deployment)
	req.Header.Set("X-Automation-Tenant", item.TenantID)
	client := http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("runtime owner unavailable")
	}
	defer resp.Body.Close()
	var result struct {
		Code int `json:"code"`
		Data struct {
			DeviceID string `json:"deviceId"`
		} `json:"data"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result) != nil || result.Code != 200 || result.Data.DeviceID != item.DeviceID {
		return fmt.Errorf("runtime owner did not confirm registration")
	}
	if _, err = tx.Exec(ctx, `UPDATE device_access_bindings SET runtime_provider_id=$3,runtime_namespace='community',runtime_external_id=$2
 WHERE tenant_id=$1 AND device_id=$2 AND runtime_namespace IS NULL AND runtime_external_id IS NULL`, item.TenantID, item.DeviceID, provider); err != nil {
		return err
	}
	return tx.Commit()
}

// Only identities confirmed by this deployment's runtime owner may be retried
// through registration. Existing imported runtime identities keep their path.
func isOwnedNativeRuntime(ctx context.Context, tenantID, deviceID string) bool {
	deployment := os.Getenv("TP_SOURCE_DEPLOYMENT_ID")
	if deployment == "" {
		return false
	}
	provider := uuid.NewSHA1(uuid.NameSpaceOID, []byte("thingspanel/community/runtime/"+deployment)).String()
	var owned bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2 AND runtime_provider_id=$3 AND runtime_namespace='community' AND runtime_external_id=$2)`, tenantID, deviceID, provider).Scan(&owned)
	return err == nil && owned
}
