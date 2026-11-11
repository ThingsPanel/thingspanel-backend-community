package notification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const managedSourceLifetime = 15 * time.Minute

type managedSourceRequest struct {
	TenantID string            `json:"tenantId"`
	Purpose  CredentialPurpose `json:"purpose"`
}
type managedSourceLease struct {
	Credential   string            `json:"credential" encore:"sensitive"`
	ExpiresAt    time.Time         `json:"expiresAt"`
	TenantID     string            `json:"tenantId"`
	DeploymentID string            `json:"deploymentId"`
	Purpose      CredentialPurpose `json:"purpose"`
}

// The machine identity is installation-scoped, not a user's login and not a
// tenant allowlist. Current platform grants authorize each requested tenant.
type managedSourceHandler struct {
	store      *Store
	token      string
	deployment string
	directory  interface {
		Exists(context.Context, string) (bool, error)
	}
	ready func() error
}

func serviceFingerprint(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (h *managedSourceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	fail := func(status int, reason string) { writeNotificationFailure(w, r, coreFailure(status, reason)) }
	if h == nil || h.ready == nil || h.ready() != nil || h.store == nil || h.directory == nil {
		fail(503, "notification_unavailable")
		return
	}
	if r.Method != "POST" || len(r.Header.Values("Authorization")) != 1 || len(r.Header.Values("X-Notification-Deployment")) != 1 || len(h.token) < 32 || h.deployment == "" || r.Header.Get("X-Notification-Deployment") != h.deployment || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.token)) != 1 {
		fail(401, "service_authentication_required")
		return
	}
	var req managedSourceRequest
	body, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
	if readErr != nil || len(body) > 4096 {
		fail(400, "invalid_source_scope")
		return
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(&struct{}{}) != io.EOF || !validDirectoryPart(req.TenantID) || (req.Purpose != CredentialPurposeSource && req.Purpose != CredentialPurposeProjection) {
		fail(400, "invalid_source_scope")
		return
	}
	exists, err := h.directory.Exists(r.Context(), req.TenantID)
	if err != nil {
		fail(503, "tenant_directory_unavailable")
		return
	}
	if !exists {
		fail(403, "source_not_authorized")
		return
	}
	lease, err := h.store.issueManagedSource(r.Context(), h.deployment, req.TenantID, req.Purpose, serviceFingerprint(h.token))
	if errors.Is(err, ErrIdentityForbidden) {
		fail(403, "source_not_authorized")
		return
	}
	if err != nil {
		fail(503, "credential_issuance_unavailable")
		return
	}
	writeManagementSuccess(w, r, http.StatusOK, lease)
}

func (s *Store) issueManagedSource(ctx context.Context, deployment, tenant string, purpose CredentialPurpose, fingerprint string) (managedSourceLease, error) {
	var empty managedSourceLease
	if !validDirectoryPart(tenant) || deployment == "" || len(fingerprint) != 64 || (purpose != CredentialPurposeSource && purpose != CredentialPurposeProjection) {
		return empty, ErrCredentialInvalid
	}
	db, err := s.database()
	if err != nil {
		return empty, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	// Lock the grant used for issuance so revocation cannot race issuance. Every
	// subsequent use also rechecks the current grants; cached leases confer no
	// entitlement after platform authorization is removed.
	var grant string
	var grantVersion, registrationVersion int64
	err = tx.QueryRowContext(ctx, `SELECT g.registration_id,g.version,p.version FROM notification_plugin_grants g JOIN notification_plugin_registrations p ON p.id=g.registration_id WHERE g.tenant_id=$1 AND g.enabled AND p.enabled ORDER BY g.registration_id LIMIT 1 FOR SHARE OF g,p`, tenant).Scan(&grant, &grantVersion, &registrationVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, ErrIdentityForbidden
	}
	if err != nil {
		return empty, err
	}
	record, token, err := newDeploymentCredential(purpose, deployment, []string{tenant}, s.now(), managedSourceLifetime)
	if err != nil {
		return empty, err
	}
	allowed, _ := json.Marshal(record.AllowedTenantIDs)
	_, err = tx.ExecContext(ctx, `INSERT INTO notification_credentials(id,purpose,token_hash,deployment_id,allowed_tenant_ids,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, record.ID, record.Purpose, record.TokenHash, deployment, string(allowed), record.ExpiresAt)
	if err != nil {
		return empty, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO notification_managed_credentials(credential_id,service_fingerprint,registration_id,grant_version,registration_version) VALUES($1,$2,$3,$4,$5)`, record.ID, fingerprint, grant, grantVersion, registrationVersion)
	if err != nil {
		return empty, err
	}
	// Only automatically issued, expired credentials are pruned. Receipt and
	// historical operator records are not affected.
	_, err = tx.ExecContext(ctx, `DELETE FROM notification_credentials c USING notification_managed_credentials m WHERE m.credential_id=c.id AND c.expires_at < $1 AND NOT EXISTS(SELECT 1 FROM notification_credentials child WHERE child.rotated_from=c.id)`, s.now().Add(-24*time.Hour))
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return managedSourceLease{token, record.ExpiresAt, tenant, deployment, purpose}, nil
}

func (s *Store) AuthorizeManagedSource(ctx context.Context, record NotificationCredentialRecord) error {
	if len(record.AllowedTenantIDs) != 1 || s.managedSourceFingerprint == "" || record.DeploymentID != s.managedSourceDeployment {
		return ErrCredentialInvalid
	}
	db, err := s.database()
	if err != nil {
		return err
	}
	var allowed bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_managed_credentials m WHERE m.credential_id=$1 AND m.service_fingerprint=$2 AND EXISTS(SELECT 1 FROM notification_plugin_grants g JOIN notification_plugin_registrations p ON p.id=g.registration_id WHERE g.tenant_id=$3 AND g.enabled AND p.enabled AND g.registration_id=m.registration_id AND g.version=m.grant_version AND p.version=m.registration_version))`, record.ID, s.managedSourceFingerprint, record.AllowedTenantIDs[0]).Scan(&allowed)
	if err != nil || !allowed {
		return ErrCredentialInvalid
	}
	return nil
}

func (s *ManagementService) authorizeManagedSource(ctx context.Context, record NotificationCredentialRecord) error {
	if s.tenantDirectory == nil || len(record.AllowedTenantIDs) != 1 {
		return ErrCredentialInvalid
	}
	exists, err := s.tenantDirectory.Exists(ctx, record.AllowedTenantIDs[0])
	if err != nil || !exists {
		return ErrCredentialInvalid
	}
	managed, ok := s.credentials.(interface {
		AuthorizeManagedSource(context.Context, NotificationCredentialRecord) error
	})
	if !ok {
		return ErrCredentialInvalid
	}
	return managed.AuthorizeManagedSource(ctx, record)
}

// validateManagedServiceConfig rejects whitespace rather than normalizing a
// secret differently in different services.
func validateManagedServiceConfig(token, deployment string) bool {
	return len(token) >= 32 && len(token) <= 8192 && !strings.ContainsAny(token, " \r\n\t") && deployment != "" && strings.TrimSpace(deployment) == deployment
}
