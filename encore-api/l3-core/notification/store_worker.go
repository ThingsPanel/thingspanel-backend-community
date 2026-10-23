package notification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"encore.app/l3-core/notification/contract"
	"github.com/google/uuid"
)

type claimCandidate struct {
	tenantID, deliveryID, instanceID string
	configVersion                    int64
	expiresAt                        time.Time
}

func (s *Store) ClaimDeliveries(ctx context.Context, limit int, leaseFor time.Duration) ([]DeliveryClaim, error) {
	if limit < 1 || limit > 100 || leaseFor <= 0 || leaseFor > 5*time.Minute {
		return nil, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	defer tx.Rollback()
	now := s.now()
	rows, err := tx.QueryContext(ctx, `SELECT d.tenant_id,d.id,d.instance_id,d.config_version,d.expires_at FROM notification_deliveries d JOIN notification_instances i ON i.tenant_id=d.tenant_id AND i.id=d.instance_id JOIN notification_instance_configs c ON c.tenant_id=d.tenant_id AND c.instance_id=d.instance_id AND c.config_version=d.config_version JOIN notification_plugin_grants g ON g.tenant_id=d.tenant_id AND g.registration_id=c.plugin_registration_id AND g.enabled=TRUE JOIN notification_plugin_registrations p ON p.id=c.plugin_registration_id AND p.enabled=TRUE WHERE d.dispatch_status='queued' AND d.next_attempt_at<=$1 AND d.expires_at>$1 AND i.enabled=TRUE AND c.enabled=TRUE AND NOT EXISTS (SELECT 1 FROM notification_deliveries active WHERE active.tenant_id=d.tenant_id AND active.instance_id=d.instance_id AND active.dispatch_status='sending') ORDER BY d.next_attempt_at,d.created_at,d.id FOR UPDATE OF d,i SKIP LOCKED LIMIT $2`, now, limit)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	var candidates []claimCandidate
	seen := map[string]bool{}
	for rows.Next() {
		var c claimCandidate
		if err := rows.Scan(&c.tenantID, &c.deliveryID, &c.instanceID, &c.configVersion, &c.expiresAt); err != nil {
			rows.Close()
			return nil, classifyStoreError(err)
		}
		key := c.tenantID + "\x00" + c.instanceID
		if seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, classifyStoreError(err)
	}
	rows.Close()
	claims := make([]DeliveryClaim, 0, len(candidates))
	for _, candidate := range candidates {
		claim, err := s.claimOneTx(ctx, tx, candidate, now, leaseFor)
		if err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	if err := tx.Commit(); err != nil {
		return nil, classifyStoreError(err)
	}
	return claims, nil
}

func (s *Store) claimOneTx(ctx context.Context, tx *sql.Tx, c claimCandidate, now time.Time, leaseFor time.Duration) (DeliveryClaim, error) {
	var recipientRef, contentRef string
	var recipientCipher, contentCipher []byte
	var recipientAAD, contentAAD int
	var recipientKind string
	var configJSON []byte
	var registrationID string
	var channel contract.Channel
	err := tx.QueryRowContext(ctx, `SELECT d.recipient_key_ref,d.recipient_ciphertext,d.recipient_aad_version,d.content_key_ref,d.content_ciphertext,d.content_aad_version,d.recipient_kind,c.config,c.plugin_registration_id,c.channel FROM notification_deliveries d JOIN notification_instance_configs c ON c.tenant_id=d.tenant_id AND c.instance_id=d.instance_id AND c.config_version=d.config_version WHERE d.tenant_id=$1 AND d.id=$2`, c.tenantID, c.deliveryID).Scan(&recipientRef, &recipientCipher, &recipientAAD, &contentRef, &contentCipher, &contentAAD, &recipientKind, &configJSON, &registrationID, &channel)
	if err != nil {
		return DeliveryClaim{}, classifyStoreError(err)
	}
	recipientJSON, err := s.open(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: c.tenantID, ResourceID: c.deliveryID, Version: 1, Purpose: "delivery-recipient", Slot: "recipient"}, ProtectedSnapshot{KeyRef: recipientRef, Ciphertext: recipientCipher, AADVersion: recipientAAD})
	if err != nil {
		return DeliveryClaim{}, storeErr(StoreErrorUnavailable)
	}
	contentBytes, err := s.open(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: c.tenantID, ResourceID: c.deliveryID, Version: 1, Purpose: "delivery-content", Slot: "content"}, ProtectedSnapshot{KeyRef: contentRef, Ciphertext: contentCipher, AADVersion: contentAAD})
	if err != nil {
		return DeliveryClaim{}, storeErr(StoreErrorUnavailable)
	}
	var recipient contract.Recipient
	var content contract.Content
	if json.Unmarshal(recipientJSON, &recipient) != nil || json.Unmarshal(contentBytes, &content) != nil {
		return DeliveryClaim{}, storeErr(StoreErrorUnavailable)
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(configJSON, &config) != nil {
		return DeliveryClaim{}, storeErr(StoreErrorUnavailable)
	}
	secrets, err := s.readInstanceSecretsTx(ctx, tx, c.tenantID, c.instanceID, c.configVersion)
	if err != nil {
		return DeliveryClaim{}, err
	}
	for key, value := range secrets {
		encoded, err := json.Marshal(value)
		if err != nil {
			return DeliveryClaim{}, storeErr(StoreErrorUnavailable)
		}
		config[key] = encoded
	}
	mergedConfig, err := json.Marshal(config)
	if err != nil {
		return DeliveryClaim{}, storeErr(StoreErrorUnavailable)
	}
	var rawContent contract.PluginContentV1
	raw, err := json.Marshal(content)
	if err != nil || json.Unmarshal(raw, &rawContent) != nil {
		return DeliveryClaim{}, storeErr(StoreErrorUnavailable)
	}
	attemptID := uuid.NewString()
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return DeliveryClaim{}, storeErr(StoreErrorUnavailable)
	}
	token := hex.EncodeToString(tokenBytes)
	sum := sha256.Sum256([]byte(token))
	leaseHash := hex.EncodeToString(sum[:])
	leaseUntil := now.Add(leaseFor)
	_, err = tx.ExecContext(ctx, `INSERT INTO notification_delivery_attempts(tenant_id,delivery_id,attempt_id,lease_hash,status,started_at,lease_expires_at) VALUES($1,$2,$3,$4,'sending',$5,$6)`, c.tenantID, c.deliveryID, attemptID, leaseHash, now, leaseUntil)
	if err != nil {
		return DeliveryClaim{}, classifyStoreError(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE notification_deliveries SET dispatch_status='sending',attempt_count=attempt_count+1,current_attempt_id=$3,current_lease_hash=$4,lease_expires_at=$5,updated_at=$6 WHERE tenant_id=$1 AND id=$2 AND dispatch_status='queued'`, c.tenantID, c.deliveryID, attemptID, leaseHash, leaseUntil, now)
	if err != nil {
		return DeliveryClaim{}, classifyStoreError(err)
	}
	var request contract.PluginSendRequestV1
	request.APIVersion = "1.0"
	request.DeliveryID = c.deliveryID
	request.AttemptID = attemptID
	request.TenantID = c.tenantID
	request.InstanceID = c.instanceID
	request.ConfigVersion = c.configVersion
	request.Channel = channel
	request.Recipient = recipient
	request.Content = rawContent
	request.Config = mergedConfig
	request.ExpiresAt = c.expiresAt.UTC().Format(time.RFC3339Nano)
	return DeliveryClaim{TenantID: c.tenantID, DeliveryID: c.deliveryID, AttemptID: attemptID, LeaseToken: token, ExpiresAt: leaseUntil, Request: request}, nil
}

func (s *Store) ResolvePluginRuntime(ctx context.Context, tenantID, instanceID string, configVersion int64) (PluginRuntime, error) {
	db, err := s.database()
	if err != nil {
		return PluginRuntime{}, err
	}
	var r PluginRuntime
	var keyRef string
	var cipherText []byte
	var aadVersion int
	err = db.QueryRowContext(ctx, `SELECT p.id,p.registration_version,p.origin,p.auth_key_ref,p.auth_ciphertext,p.auth_aad_version FROM notification_instance_configs c JOIN notification_instances i ON i.tenant_id=c.tenant_id AND i.id=c.instance_id JOIN notification_plugin_grants g ON g.tenant_id=i.tenant_id AND g.registration_id=c.plugin_registration_id AND g.enabled=TRUE JOIN notification_plugin_registrations p ON p.id=c.plugin_registration_id WHERE c.tenant_id=$1 AND c.instance_id=$2 AND c.config_version=$3 AND i.enabled=TRUE AND c.enabled=TRUE AND p.enabled=TRUE`, tenantID, instanceID, configVersion).Scan(&r.RegistrationID, &r.RegistrationVersion, &r.Origin, &keyRef, &cipherText, &aadVersion)
	if err != nil {
		return PluginRuntime{}, classifyStoreError(err)
	}
	plain, err := s.open(ctx, SnapshotAAD{ScopeKind: "platform", ResourceID: r.RegistrationID, Version: r.RegistrationVersion, Purpose: "plugin-auth", Slot: "authSecret"}, ProtectedSnapshot{KeyRef: keyRef, Ciphertext: cipherText, AADVersion: aadVersion})
	if err != nil {
		return PluginRuntime{}, storeErr(StoreErrorUnavailable)
	}
	r.AuthSecret = string(plain)
	return r, nil
}

func (s *Store) RecoverExpiredClaims(ctx context.Context, now time.Time) (int, error) {
	db, err := s.database()
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, classifyStoreError(err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT tenant_id,id,current_attempt_id FROM notification_deliveries WHERE dispatch_status='sending' AND lease_expires_at<=$1 FOR UPDATE SKIP LOCKED`, now)
	if err != nil {
		return 0, classifyStoreError(err)
	}
	type expired struct{ tenant, delivery, attempt string }
	var list []expired
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.tenant, &e.delivery, &e.attempt); err != nil {
			rows.Close()
			return 0, classifyStoreError(err)
		}
		list = append(list, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, classifyStoreError(err)
	}
	rows.Close()
	for _, e := range list {
		if _, err := tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET status='unknown',finished_at=$4 WHERE tenant_id=$1 AND delivery_id=$2 AND attempt_id=$3 AND status='sending'`, e.tenant, e.delivery, e.attempt, now); err != nil {
			return 0, classifyStoreError(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET dispatch_status='unknown',delivery_status='unknown',current_attempt_id=NULL,current_lease_hash=NULL,lease_expires_at=NULL,updated_at=$3 WHERE tenant_id=$1 AND id=$2`, e.tenant, e.delivery, now); err != nil {
			return 0, classifyStoreError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, classifyStoreError(err)
	}
	return len(list), nil
}
