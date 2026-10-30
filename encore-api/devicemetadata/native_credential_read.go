package devicemetadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.dev"
	"encore.dev/beta/errs"
)

// NativeCredentialSummary intentionally exposes metadata only. The backing
// query never reads verifier, ciphertext, nonce, or certificate material.
type NativeCredentialSummary struct {
	ID               string     `json:"id"`
	DeviceID         string     `json:"deviceId"`
	Kind             string     `json:"kind"`
	Status           string     `json:"status"`
	PublicIdentifier *string    `json:"publicIdentifier"`
	Fingerprint      string     `json:"fingerprint"`
	CreatedAt        time.Time  `json:"createdAt"`
	RevokedAt        *time.Time `json:"revokedAt"`
}

// NativeCredentialDetail is a summary plus the plaintext secret. Secret is null
// when the credential has no stored secret (x509) or has been revoked.
type NativeCredentialDetail struct {
	NativeCredentialSummary
	Secret *string `json:"secret"`
}

type NativeCredentialDetailPage struct {
	Items    []NativeCredentialDetail `json:"items"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"pageSize"`
	Total    int64                    `json:"total"`
}

type NativeCredentialDetailPageResponse struct {
	Data NativeCredentialDetailPage `json:"data"`
	Meta RequestMeta                `json:"meta"`
	ETag string                     `json:"-" header:"ETag"`
}

type NativeCredentialSummaryPage struct {
	Items    []NativeCredentialSummary `json:"items"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"pageSize"`
	Total    int64                     `json:"total"`
}

type NativeCredentialSummaryPageResponse struct {
	Data NativeCredentialSummaryPage `json:"data"`
	Meta RequestMeta                 `json:"meta"`
	ETag string                      `json:"-" header:"ETag"`
}

func readNativeDeviceCredentialSummaries(ctx context.Context, identity nativeDeviceReadIdentity, deviceID string, page, pageSize int, requestID string) (*NativeCredentialSummaryPageResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	if page < 1 || page > 1_000_000 || pageSize < 1 || pageSize > 100 {
		return nil, invalidGroupQuery("invalid credential pagination")
	}
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "list device credentials failed")
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL)`, identity.tenantID, deviceID).Scan(&exists); err != nil {
		return nil, wrapDBErr(err, "read device failed")
	}
	if !exists {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	const scope = ` FROM device_credentials WHERE tenant_id=$1 AND device_id=$2 AND purpose='device_access'`
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*)`+scope, identity.tenantID, deviceID).Scan(&total); err != nil {
		return nil, wrapDBErr(err, "count device credentials failed")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,device_id,kind,status,public_identifier,fingerprint,created_at,revoked_at`+scope+` ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, identity.tenantID, deviceID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, wrapDBErr(err, "list device credentials failed")
	}
	items := make([]NativeCredentialSummary, 0)
	for rows.Next() {
		var item NativeCredentialSummary
		var publicIdentifier sql.NullString
		var fingerprint []byte
		var createdAt sql.NullTime
		var revokedAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.DeviceID, &item.Kind, &item.Status, &publicIdentifier, &fingerprint, &createdAt, &revokedAt); err != nil {
			rows.Close()
			return nil, wrapDBErr(err, "list device credentials failed")
		}
		if len(fingerprint) != sha256.Size || !createdAt.Valid {
			rows.Close()
			return nil, &errs.Error{Code: errs.Internal, Message: "credential summary is invalid"}
		}
		item.PublicIdentifier = nullableStringPointer(publicIdentifier)
		item.Fingerprint = hex.EncodeToString(fingerprint)
		item.CreatedAt = createdAt.Time.UTC()
		if revokedAt.Valid {
			value := revokedAt.Time.UTC()
			item.RevokedAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, wrapDBErr(err, "list device credentials failed")
	}
	if err := rows.Close(); err != nil {
		return nil, wrapDBErr(err, "list device credentials failed")
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "list device credentials failed")
	}
	response := &NativeCredentialSummaryPageResponse{Data: NativeCredentialSummaryPage{Items: items, Page: page, PageSize: pageSize, Total: total}, Meta: RequestMeta{RequestID: requestID}}
	encoded, _ := json.Marshal(response.Data)
	digest := sha256.Sum256(encoded)
	response.ETag = `"sha256-` + hex.EncodeToString(digest[:]) + `"`
	return response, nil
}

// readNativeDeviceCredentials returns the page of credentials with plaintext
// secrets. Product decision 2026-10-29: secrets are shown directly to callers
// who may read device credentials; ordinary authentication and tenant isolation
// still apply, and no separate reveal flow exists.
func readNativeDeviceCredentials(ctx context.Context, identity nativeDeviceReadIdentity, deviceID string, page, pageSize int, requestID string, protector *CredentialSecretProtector) (*NativeCredentialDetailPageResponse, error) {
	summaries, err := readNativeDeviceCredentialSummaries(ctx, identity, deviceID, page, pageSize, requestID)
	if err != nil {
		return nil, err
	}
	items := make([]NativeCredentialDetail, 0, len(summaries.Data.Items))
	secrets := map[string]string{}
	if len(summaries.Data.Items) > 0 {
		onPage := map[string]struct{}{}
		for _, item := range summaries.Data.Items {
			if item.Status != "revoked" {
				onPage[item.ID] = struct{}{}
			}
		}
		tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil, wrapDBErr(err, "read device credential secrets failed")
		}
		defer tx.Rollback()
		rows, err := tx.QueryContext(ctx, `SELECT id::text,ciphertext,key_id,cipher_suite,nonce FROM device_credentials
			WHERE tenant_id=$1 AND device_id=$2 AND purpose='device_access' AND protection_mode='encrypted' AND status<>'revoked'`,
			identity.tenantID, deviceID)
		if err != nil {
			return nil, wrapDBErr(err, "read device credential secrets failed")
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var protected ProtectedCredential
			if err := rows.Scan(&id, &protected.Ciphertext, &protected.KeyID, &protected.CipherSuite, &protected.Nonce); err != nil {
				return nil, wrapDBErr(err, "read device credential secrets failed")
			}
			if _, wanted := onPage[id]; !wanted {
				continue
			}
			if protector == nil {
				return nil, credentialProtectionUnavailable()
			}
			plaintext, err := protector.Open(CredentialAAD{TenantID: identity.tenantID, OwnerKind: CredentialOwnerDevice, OwnerID: deviceID,
				CredentialID: id, Purpose: CredentialPurposeDeviceAccess, Use: CredentialUseMaterial}, protected)
			if err != nil {
				return nil, credentialProtectionUnavailable()
			}
			secrets[id] = string(plaintext)
		}
		if err := rows.Err(); err != nil {
			return nil, wrapDBErr(err, "read device credential secrets failed")
		}
	}
	for _, item := range summaries.Data.Items {
		detail := NativeCredentialDetail{NativeCredentialSummary: item}
		if secret, ok := secrets[item.ID]; ok {
			detail.Secret = &secret
		}
		items = append(items, detail)
	}
	response := &NativeCredentialDetailPageResponse{
		Data: NativeCredentialDetailPage{Items: items, Page: page, PageSize: pageSize, Total: summaries.Data.Total},
		Meta: RequestMeta{RequestID: requestID},
	}
	encoded, _ := json.Marshal(response.Data)
	digest := sha256.Sum256(encoded)
	response.ETag = `"sha256-` + hex.EncodeToString(digest[:]) + `"`
	return response, nil
}

func parseNativeCredentialPagination(r *http.Request) (int, int, error) {
	values, err := parseNativeReadValues(r, []string{"page", "pageSize"})
	if err != nil {
		return 0, 0, err
	}
	for _, key := range []string{"page", "pageSize"} {
		if values.Has(key) && values.Get(key) == "" {
			return 0, 0, invalidGroupQuery(key + " must not be empty")
		}
	}
	return normalizeDeviceGroupPagination(values.Get("page"), values.Get("pageSize"))
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/credentials
func (s *Service) ListNativeDeviceCredentials(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err == nil && !authz.Can(authn.CurrentData(), "device", "credentials:read") {
		err = &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to read device credentials"}
	}
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	page, pageSize, err := parseNativeCredentialPagination(r)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	protector, err := NewCredentialSecretProtectorFromEnv()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, credentialProtectionUnavailable())
		return
	}
	result, err := readNativeDeviceCredentials(r.Context(), identity, deviceID, page, pageSize, requestID, protector)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, result.ETag, result)
}
