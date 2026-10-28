package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"encore.app/l3-core/notification/contract"
	"github.com/google/uuid"
)

func (s *Store) AcceptSubmission(ctx context.Context, tenantID, idempotencyKey, intentDigest string, request contract.NotificationSubmit) (contract.AcceptedData, error) {
	if !validIdempotencyInput(tenantID, "submission", idempotencyKey, intentDigest) || len(request.Deliveries) == 0 || request.Source.Type == "" || request.Source.ID == "" {
		return contract.AcceptedData{}, storeErr(StoreErrorInvalid)
	}
	expires, err := time.Parse(time.RFC3339, request.ExpiresAt)
	if err != nil || !expires.After(s.now()) {
		return contract.AcceptedData{}, storeErr(StoreErrorInvalid)
	}
	return s.acceptDeliveries(ctx, tenantID, "submission", idempotencyKey, intentDigest, request.Source, "", "", "", "", 0, "", request.Deliveries, expires)
}

// AcceptSourcePlan durably accepts one source event tuple. The tuple includes
// actionID so separate actions from one event stay independently deliverable.
func (s *Store) AcceptSourcePlan(ctx context.Context, tenantID, idempotencyKey, intentDigest string, plan SourceAcceptancePlan) (contract.AcceptedData, error) {
	event := plan.Event
	if event.TenantID != tenantID || event.SourceDeploymentID == "" || event.SourceEventID == "" || event.ActionID == "" || event.SchemaVersion == "" || !validIdempotencyInput(tenantID, "source", idempotencyKey, intentDigest) {
		return contract.AcceptedData{}, storeErr(StoreErrorInvalid)
	}
	expires, err := time.Parse(time.RFC3339, event.ExpiresAt)
	if err != nil {
		return contract.AcceptedData{}, storeErr(StoreErrorInvalid)
	}
	if !expires.After(s.now()) {
		plan.BlockedReason = "blocked_expired"
		plan.Deliveries = nil
	}
	if plan.BlockedReason == "" && (event.NotificationGroupID == "" || event.GroupRevision < 1) {
		return contract.AcceptedData{}, storeErr(StoreErrorInvalid)
	}
	if plan.BlockedReason != "" && len(plan.Deliveries) != 0 {
		return contract.AcceptedData{}, storeErr(StoreErrorInvalid)
	}
	if plan.BlockedReason == "" && len(plan.Deliveries) == 0 {
		return contract.AcceptedData{}, storeErr(StoreErrorInvalid)
	}
	// V1 ingress is the legacy alarm adapter. "source" is an internal
	// idempotency operation, not a public sourceType in the frozen contract.
	source := contract.SourceRef{Type: "alarm", ID: event.SourceEventID}
	return s.acceptDeliveries(ctx, tenantID, "source", idempotencyKey, intentDigest, source, event.SourceDeploymentID, event.SourceEventID, event.ActionID, event.NotificationGroupID, event.GroupRevision, plan.BlockedReason, plan.Deliveries, expires)
}

func (s *Store) acceptDeliveries(ctx context.Context, tenantID, operation, idemKey, digest string, source contract.SourceRef, deploymentID, eventID, actionID, groupID string, groupRevision int64, blockedReason string, deliveries []contract.SubmitDelivery, expires time.Time) (contract.AcceptedData, error) {
	db, err := s.database()
	if err != nil {
		return contract.AcceptedData{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return contract.AcceptedData{}, classifyStoreError(err)
	}
	defer tx.Rollback()
	requestID := uuid.NewString()
	if rec, replay, err := beginIdempotencyTx(ctx, tx, tenantID, operation, idemKey, digest, "notification", requestID, 1); err != nil {
		return contract.AcceptedData{}, err
	} else if replay {
		return s.acceptedReplayTx(ctx, tx, tenantID, rec.ResourceID)
	}
	intakeStatus := contract.IntakeReady
	if blockedReason != "" {
		intakeStatus = contract.IntakeBlocked
	}
	var groupArg any
	var revisionArg any
	if groupID != "" && intakeStatus == contract.IntakeReady {
		if operation == "source" {
			var published bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
				SELECT 1 FROM notification_source_group_revisions p
				JOIN notification_group_revisions g ON g.tenant_id=p.tenant_id AND g.group_id=p.notification_group_id AND g.revision=p.group_revision
				WHERE p.source_deployment_id=$1 AND p.tenant_id=$2 AND p.notification_group_id=$3 AND p.group_revision=$4 AND g.enabled=TRUE
			)`, deploymentID, tenantID, groupID, groupRevision).Scan(&published); err != nil {
				return contract.AcceptedData{}, classifyStoreError(err)
			}
			if !published {
				return contract.AcceptedData{}, storeErr(StoreErrorNotFound)
			}
		}
		groupArg = groupID
		revisionArg = groupRevision
	}
	var sourceGroupArg any
	var sourceRevisionArg any
	if operation == "source" {
		sourceGroupArg = groupID
		if groupRevision > 0 {
			sourceRevisionArg = groupRevision
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO notification_requests(tenant_id,id,source_type,source_id,source_deployment_id,source_event_id,action_id,group_id,group_revision,source_group_id,source_group_revision,intent_digest,intake_status,blocked_reason,expires_at) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9,$10,$11,$12,$13,NULLIF($14,''),$15)`, tenantID, requestID, source.Type, source.ID, deploymentID, eventID, actionID, groupArg, revisionArg, sourceGroupArg, sourceRevisionArg, digest, string(intakeStatus), blockedReason, expires)
	if err != nil {
		return contract.AcceptedData{}, classifyStoreError(err)
	}
	accepted := contract.AcceptedData{Accepted: true, NotificationID: requestID, IntakeStatus: string(intakeStatus), BlockedReason: blockedReason, DeliveryIDs: []string{}}
	for ordinal, delivery := range deliveries {
		if delivery.InstanceID == "" || delivery.Recipient.Kind == "" || delivery.Recipient.Address == "" {
			return contract.AcceptedData{}, storeErr(StoreErrorInvalid)
		}
		var configVersion int64
		err := tx.QueryRowContext(ctx, `SELECT i.current_config_version FROM notification_instances i JOIN notification_instance_configs c ON c.tenant_id=i.tenant_id AND c.instance_id=i.id AND c.config_version=i.current_config_version JOIN notification_plugin_grants g ON g.tenant_id=i.tenant_id AND g.registration_id=c.plugin_registration_id AND g.enabled=TRUE JOIN notification_plugin_registrations p ON p.id=c.plugin_registration_id AND p.enabled=TRUE WHERE i.tenant_id=$1 AND i.id=$2 AND i.enabled=TRUE AND c.enabled=TRUE`, tenantID, delivery.InstanceID).Scan(&configVersion)
		if err != nil {
			return contract.AcceptedData{}, classifyStoreError(err)
		}
		deliveryID := uuid.NewString()
		recipientJSON, err := json.Marshal(delivery.Recipient)
		if err != nil {
			return contract.AcceptedData{}, storeErr(StoreErrorInvalid)
		}
		contentJSON, err := json.Marshal(delivery.Content)
		if err != nil {
			return contract.AcceptedData{}, storeErr(StoreErrorInvalid)
		}
		rp, err := s.seal(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: tenantID, ResourceID: deliveryID, Version: 1, Purpose: "delivery-recipient", Slot: "recipient"}, recipientJSON)
		if err != nil {
			return contract.AcceptedData{}, err
		}
		cp, err := s.seal(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: tenantID, ResourceID: deliveryID, Version: 1, Purpose: "delivery-content", Slot: "content"}, contentJSON)
		if err != nil {
			return contract.AcceptedData{}, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO notification_deliveries(tenant_id,id,request_id,ordinal,instance_id,config_version,recipient_kind,recipient_display,recipient_key_ref,recipient_ciphertext,recipient_aad_version,content_key_ref,content_ciphertext,content_aad_version,expires_at,dispatch_status,delivery_status,next_attempt_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'queued','pending',NOW())`, tenantID, deliveryID, requestID, ordinal, delivery.InstanceID, configVersion, delivery.Recipient.Kind, maskRecipient(delivery.Recipient.Address), rp.KeyRef, rp.Ciphertext, rp.AADVersion, cp.KeyRef, cp.Ciphertext, cp.AADVersion, expires)
		if err != nil {
			return contract.AcceptedData{}, classifyStoreError(err)
		}
		accepted.DeliveryIDs = append(accepted.DeliveryIDs, deliveryID)
	}
	if err := tx.Commit(); err != nil {
		return contract.AcceptedData{}, classifyStoreError(err)
	}
	return accepted, nil
}

func (s *Store) acceptedReplayTx(ctx context.Context, tx *sql.Tx, tenantID, requestID string) (contract.AcceptedData, error) {
	var accepted contract.AcceptedData
	err := tx.QueryRowContext(ctx, `SELECT id,intake_status,COALESCE(blocked_reason,'') FROM notification_requests WHERE tenant_id=$1 AND id=$2`, tenantID, requestID).Scan(&accepted.NotificationID, &accepted.IntakeStatus, &accepted.BlockedReason)
	if err != nil {
		return contract.AcceptedData{}, classifyStoreError(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM notification_deliveries WHERE tenant_id=$1 AND request_id=$2 ORDER BY ordinal`, tenantID, requestID)
	if err != nil {
		return contract.AcceptedData{}, classifyStoreError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return contract.AcceptedData{}, classifyStoreError(err)
		}
		accepted.DeliveryIDs = append(accepted.DeliveryIDs, id)
	}
	if err := rows.Err(); err != nil {
		return contract.AcceptedData{}, classifyStoreError(err)
	}
	accepted.Accepted = true
	if err := tx.Commit(); err != nil {
		return contract.AcceptedData{}, classifyStoreError(err)
	}
	return accepted, nil
}

func maskRecipient(address string) string {
	trimmed := strings.TrimSpace(address)
	if len(trimmed) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(trimmed)-4) + trimmed[len(trimmed)-4:]
}
