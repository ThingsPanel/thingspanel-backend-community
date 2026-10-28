package notification

import (
	"context"
	"database/sql"

	"encore.app/l3-core/notification/contract"
)

func (s *Store) FindManagementReplay(ctx context.Context, tenantID, operation, idempotencyKey, requestDigest string) (ManagementReplay, bool, error) {
	if !validIdempotencyInput(tenantID, operation, idempotencyKey, requestDigest) {
		return ManagementReplay{}, false, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return ManagementReplay{}, false, err
	}
	var replay ManagementReplay
	err = db.QueryRowContext(ctx, `
		SELECT resource_type, resource_id, resource_version
		FROM notification_idempotency
		WHERE tenant_id = $1 AND operation = $2 AND idempotency_key = $3 AND request_digest = $4
	`, tenantID, operation, idempotencyKey, requestDigest).Scan(&replay.ResourceType, &replay.ResourceID, &replay.Version)
	if err == sql.ErrNoRows {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM notification_idempotency WHERE tenant_id=$1 AND operation=$2 AND idempotency_key=$3
		)`, tenantID, operation, idempotencyKey).Scan(&exists); err != nil {
			return ManagementReplay{}, false, classifyStoreError(err)
		}
		if exists {
			return ManagementReplay{}, false, storeErr(StoreErrorConflict)
		}
		return ManagementReplay{}, false, nil
	}
	if err != nil {
		return ManagementReplay{}, false, classifyStoreError(err)
	}
	return replay, true, nil
}

func (s *Store) FindGroupReplay(ctx context.Context, tenantID, groupID, operation, idempotencyKey, requestDigest string) (contract.GroupView, bool, error) {
	replay, ok, err := s.FindManagementReplay(ctx, tenantID, operation, idempotencyKey, requestDigest)
	if err != nil || !ok {
		return contract.GroupView{}, ok, err
	}
	if replay.ResourceType != "group" || replay.ResourceID != groupID {
		return contract.GroupView{}, false, storeErr(StoreErrorConflict)
	}
	view, err := s.getGroupVersion(ctx, tenantID, groupID, replay.Version)
	return view, err == nil, err
}

func (s *Store) FindInstanceReplay(ctx context.Context, tenantID, instanceID, operation, idempotencyKey, requestDigest string) (contract.InstanceView, bool, error) {
	replay, ok, err := s.FindManagementReplay(ctx, tenantID, operation, idempotencyKey, requestDigest)
	if err != nil || !ok {
		return contract.InstanceView{}, ok, err
	}
	if replay.ResourceType != "instance" || replay.ResourceID != instanceID {
		return contract.InstanceView{}, false, storeErr(StoreErrorConflict)
	}
	view, err := s.getInstanceVersion(ctx, tenantID, instanceID, replay.Version)
	return view, err == nil, err
}

func (s *Store) FindPluginRegistrationReplay(ctx context.Context, tenantID, idempotencyKey, requestDigest string) (contract.PluginView, bool, error) {
	replay, ok, err := s.FindManagementReplay(ctx, tenantID, "plugin.register", idempotencyKey, requestDigest)
	if err != nil || !ok {
		return contract.PluginView{}, ok, err
	}
	if replay.ResourceType != "plugin" {
		return contract.PluginView{}, false, storeErr(StoreErrorConflict)
	}
	view, err := s.getPluginVersion(ctx, tenantID, replay.ResourceID, replay.Version)
	return view, err == nil, err
}

func (s *Store) FindSubmissionReplay(ctx context.Context, tenantID, idempotencyKey, intentDigest string) (contract.AcceptedData, bool, error) {
	return s.findAcceptedReplay(ctx, tenantID, "submission", idempotencyKey, intentDigest, "", "", "")
}

func (s *Store) FindSourceReplay(ctx context.Context, tenantID, idempotencyKey, intentDigest, deploymentID, sourceEventID, actionID string) (contract.AcceptedData, bool, error) {
	if deploymentID == "" || sourceEventID == "" || actionID == "" {
		return contract.AcceptedData{}, false, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.AcceptedData{}, false, err
	}
	var storedDigest, notificationID string
	err = db.QueryRowContext(ctx, `
		SELECT intent_digest, id FROM notification_requests
		WHERE tenant_id=$1 AND source_deployment_id=$2 AND source_event_id=$3 AND action_id=$4
	`, tenantID, deploymentID, sourceEventID, actionID).Scan(&storedDigest, &notificationID)
	if err != nil && err != sql.ErrNoRows {
		return contract.AcceptedData{}, false, classifyStoreError(err)
	}
	if err == nil {
		if storedDigest != intentDigest {
			return contract.AcceptedData{}, false, storeErr(StoreErrorConflict)
		}
		return s.acceptedForNotification(ctx, tenantID, notificationID)
	}
	return s.findAcceptedReplay(ctx, tenantID, "source", idempotencyKey, intentDigest, deploymentID, sourceEventID, actionID)
}

func (s *Store) acceptedForNotification(ctx context.Context, tenantID, notificationID string) (contract.AcceptedData, bool, error) {
	db, err := s.database()
	if err != nil {
		return contract.AcceptedData{}, false, err
	}
	var accepted contract.AcceptedData
	err = db.QueryRowContext(ctx, `SELECT id, intake_status, COALESCE(blocked_reason, '') FROM notification_requests WHERE tenant_id=$1 AND id=$2`, tenantID, notificationID).Scan(&accepted.NotificationID, &accepted.IntakeStatus, &accepted.BlockedReason)
	if err != nil {
		return contract.AcceptedData{}, false, classifyStoreError(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM notification_deliveries WHERE tenant_id=$1 AND request_id=$2 ORDER BY ordinal`, tenantID, notificationID)
	if err != nil {
		return contract.AcceptedData{}, false, classifyStoreError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return contract.AcceptedData{}, false, classifyStoreError(err)
		}
		accepted.DeliveryIDs = append(accepted.DeliveryIDs, id)
	}
	if err := rows.Err(); err != nil {
		return contract.AcceptedData{}, false, classifyStoreError(err)
	}
	accepted.Accepted = true
	return accepted, true, nil
}

func (s *Store) findAcceptedReplay(ctx context.Context, tenantID, operation, idempotencyKey, intentDigest, deploymentID, sourceEventID, actionID string) (contract.AcceptedData, bool, error) {
	if !validIdempotencyInput(tenantID, operation, idempotencyKey, intentDigest) {
		return contract.AcceptedData{}, false, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.AcceptedData{}, false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return contract.AcceptedData{}, false, classifyStoreError(err)
	}
	defer tx.Rollback()
	var record *idempotencyRecord
	if operation == "submission" || operation == "source" {
		record, err = findIdempotencyTx(ctx, tx, tenantID, operation, idempotencyKey)
	} else {
		return contract.AcceptedData{}, false, storeErr(StoreErrorInvalid)
	}
	if err != nil {
		return contract.AcceptedData{}, false, err
	}
	if record == nil {
		return contract.AcceptedData{}, false, nil
	}
	if record.Digest != intentDigest {
		return contract.AcceptedData{}, false, storeErr(StoreErrorConflict)
	}
	if record.ResourceType != "notification" {
		return contract.AcceptedData{}, false, storeErr(StoreErrorUnavailable)
	}
	var accepted contract.AcceptedData
	var deployment, eventID, action string
	err = tx.QueryRowContext(ctx, `
		SELECT id, intake_status, COALESCE(blocked_reason, ''),
		       COALESCE(source_deployment_id, ''), COALESCE(source_event_id, ''), COALESCE(action_id, '')
		FROM notification_requests WHERE tenant_id=$1 AND id=$2
	`, tenantID, record.ResourceID).Scan(&accepted.NotificationID, &accepted.IntakeStatus, &accepted.BlockedReason, &deployment, &eventID, &action)
	if err != nil {
		return contract.AcceptedData{}, false, classifyStoreError(err)
	}
	if operation == "source" && (deployment != deploymentID || eventID != sourceEventID || action != actionID) {
		return contract.AcceptedData{}, false, storeErr(StoreErrorConflict)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM notification_deliveries WHERE tenant_id=$1 AND request_id=$2 ORDER BY ordinal`, tenantID, accepted.NotificationID)
	if err != nil {
		return contract.AcceptedData{}, false, classifyStoreError(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return contract.AcceptedData{}, false, classifyStoreError(err)
		}
		accepted.DeliveryIDs = append(accepted.DeliveryIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return contract.AcceptedData{}, false, classifyStoreError(err)
	}
	rows.Close()
	accepted.Accepted = true
	if err := tx.Commit(); err != nil {
		return contract.AcceptedData{}, false, classifyStoreError(err)
	}
	return accepted, true, nil
}
