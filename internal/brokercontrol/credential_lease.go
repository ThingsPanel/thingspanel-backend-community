package brokercontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"io"
	"net/http"
	bc "project/internal/brokercontract"
	"time"
)

type CredentialLease struct {
	ConnectionID   string `gorm:"primaryKey"`
	DeviceID       string
	TenantID       string
	BrokerCell     string
	BrokerInstance string
	VoucherSHA256  string `gorm:"column:voucher_sha256"`
	ExpiresAt      time.Time
	RevokedAckAt   *time.Time
}

func (CredentialLease) TableName() string { return "native_runtime_credential_leases" }

type credentialLeaseRepository interface {
	RecordCredentialLease(context.Context, bc.AuthRequest, bc.Grant) error
	CredentialLeasesReady(context.Context) error
	AcknowledgeCredentialLease(context.Context, bc.CredentialLeaseAck) error
}

// Lock order matches RegisterNativeRuntime. Recheck the credential AFTER taking
// the lock: authentication may have started before a concurrent rotation.
func (r *GormRepository) RecordCredentialLease(ctx context.Context, req bc.AuthRequest, grant bc.Grant) error {
	if grant.Principal.Kind != bc.PrincipalDevice {
		return nil
	}
	return r.quietDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 72311031))", grant.Principal.DeviceID).Error; err != nil {
			return err
		}
		var registered struct {
			TenantID      string
			VoucherSHA256 string `gorm:"column:voucher_sha256"`
			Active        bool
		}
		err := tx.Table("native_runtime_registrations").Where("device_id=?", grant.Principal.DeviceID).Take(&registered).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var password *string
		if req.Password != "" {
			password = &req.Password
		}
		raw, _ := json.Marshal(struct {
			Username string  `json:"username"`
			Password *string `json:"password,omitempty"`
		}{req.Username, password})
		// Compare JSON semantically; username/password vouchers may have different key order.
		var current string
		if err := tx.Table("devices").Select("voucher").Where("id=? AND tenant_id=? AND broker_safe_voucher_v1(voucher)=?::jsonb", grant.Principal.DeviceID, grant.Principal.TenantID, string(raw)).Scan(&current).Error; err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(current))
		if current == "" || !registered.Active || registered.TenantID != grant.Principal.TenantID || hex.EncodeToString(sum[:]) != registered.VoucherSHA256 {
			return ErrBindingConflict
		}
		if err := tx.Exec("DELETE FROM native_runtime_credential_leases WHERE device_id=? AND expires_at<=now()", grant.Principal.DeviceID).Error; err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO native_runtime_credential_leases(connection_id,device_id,tenant_id,broker_cell,broker_instance,voucher_sha256,expires_at)
 VALUES(?,?,?,?,?,?,?) ON CONFLICT(connection_id) DO UPDATE SET voucher_sha256=EXCLUDED.voucher_sha256,expires_at=EXCLUDED.expires_at,revoked_ack_at=NULL
 WHERE native_runtime_credential_leases.device_id=EXCLUDED.device_id AND native_runtime_credential_leases.tenant_id=EXCLUDED.tenant_id
 AND native_runtime_credential_leases.broker_cell=EXCLUDED.broker_cell AND native_runtime_credential_leases.broker_instance=EXCLUDED.broker_instance`,
			grant.ConnectionID, grant.Principal.DeviceID, grant.Principal.TenantID, req.BrokerCell, req.BrokerInstance, registered.VoucherSHA256, grant.ExpiresAt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBindingConflict
		}
		return nil
	})
}
func (r *GormRepository) AcknowledgeCredentialLease(ctx context.Context, ack bc.CredentialLeaseAck) error {
	// Exact expiry fences delayed ACKs from a previous refresh. An ACK can never
	// rewrite a new grant, rotate a credential, or affect another broker instance.
	return r.quietDB(ctx).Exec(`UPDATE native_runtime_credential_leases SET revoked_ack_at=now()
 WHERE connection_id=? AND broker_cell=? AND broker_instance=? AND expires_at=?`, ack.ConnectionID, ack.BrokerCell, ack.BrokerInstance, ack.ExpiresAt).Error
}
func (h *Handler) credentialLeaseAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var ack bc.CredentialLeaseAck
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&ack) != nil || ack.ConnectionID == "" || len(ack.ConnectionID) > 80 || ack.ExpiresAt.IsZero() || dec.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid acknowledgement", 400)
		return
	}
	if !h.owner.authorizedToken(bearerToken(r.Header.Get("Authorization")), ack.BrokerCell, ack.BrokerInstance) {
		http.Error(w, "unauthorized", 401)
		return
	}
	repo, ok := h.owner.repo.(credentialLeaseRepository)
	if !ok || repo.AcknowledgeCredentialLease(r.Context(), ack) != nil {
		http.Error(w, "acknowledgement unavailable", 503)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *GormRepository) CredentialLeasesReady(ctx context.Context) error {
	return r.quietDB(ctx).Exec("SELECT 1 FROM native_runtime_credential_leases LIMIT 0").Error
}
func (h *Handler) credentialLeaseReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var identity struct {
		BrokerCell     string `json:"broker_cell"`
		BrokerInstance string `json:"broker_instance"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&identity) != nil || dec.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid request", 400)
		return
	}
	if !h.owner.authorizedToken(bearerToken(r.Header.Get("Authorization")), identity.BrokerCell, identity.BrokerInstance) {
		http.Error(w, "unauthorized", 401)
		return
	}
	repo, ok := h.owner.repo.(credentialLeaseRepository)
	if !ok || repo.CredentialLeasesReady(r.Context()) != nil {
		http.Error(w, "lease contract unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "ready": true})
}
