package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"encore.app/l3-core/notification/contract"
)

const maxPluginGrantPageSize = 100

func pluginGrantOperation(registrationID string) string {
	return "plugin.grant.set:" + registrationID
}

// FindPluginGrantReplay resolves the immutable grant state associated with an
// idempotency key. The target tenant is the idempotency scope; the operation
// binds the registration, so a key cannot be replayed for another grant.
func (s *Store) FindPluginGrantReplay(ctx context.Context, tenantID, registrationID, idempotencyKey, requestDigest string) (contract.PluginGrantView, bool, error) {
	if !validIdempotencyInput(tenantID, pluginGrantOperation(registrationID), idempotencyKey, requestDigest) || registrationID == "" {
		return contract.PluginGrantView{}, false, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.PluginGrantView{}, false, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return contract.PluginGrantView{}, false, classifyStoreError(err)
	}
	defer tx.Rollback()
	record, err := findIdempotencyTx(ctx, tx, tenantID, pluginGrantOperation(registrationID), idempotencyKey)
	if err != nil {
		return contract.PluginGrantView{}, false, err
	}
	if record == nil {
		return contract.PluginGrantView{}, false, nil
	}
	if record.Digest != requestDigest || record.ResourceType != "plugin_grant" || record.ResourceID != registrationID {
		return contract.PluginGrantView{}, false, storeErr(StoreErrorConflict)
	}
	view, err := getPluginGrantVersionTx(ctx, tx, tenantID, registrationID, record.ResourceVersion)
	if err != nil {
		return contract.PluginGrantView{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return contract.PluginGrantView{}, false, classifyStoreError(err)
	}
	return view, true, nil
}

// SetPluginGrant creates or changes exactly one tenant grant. Grant history is
// immutable so exact retries can return the original state after later edits.
func (s *Store) SetPluginGrant(ctx context.Context, tenantID, registrationID, idempotencyKey, requestDigest string, update contract.PluginGrantUpdate) (contract.PluginGrantView, bool, error) {
	if !validIdempotencyInput(tenantID, pluginGrantOperation(registrationID), idempotencyKey, requestDigest) || registrationID == "" || update.TenantID != tenantID || update.PluginRegistrationID != registrationID || update.ExpectedVersion < 0 {
		return contract.PluginGrantView{}, false, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.PluginGrantView{}, false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return contract.PluginGrantView{}, false, classifyStoreError(err)
	}
	defer tx.Rollback()

	if view, created, found, err := findPluginGrantReplayTx(ctx, tx, tenantID, registrationID, idempotencyKey, requestDigest); err != nil {
		return contract.PluginGrantView{}, false, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return contract.PluginGrantView{}, false, classifyStoreError(err)
		}
		return view, created, nil
	}

	// Locking the registration also serializes two expectedVersion=0 inserts
	// for the same grant, where there is no grant row to lock yet.
	var lockedRegistrationID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM notification_plugin_registrations WHERE id=$1 FOR UPDATE`, registrationID).Scan(&lockedRegistrationID)
	if err == sql.ErrNoRows {
		return contract.PluginGrantView{}, false, storeErr(StoreErrorNotFound)
	}
	if err != nil {
		return contract.PluginGrantView{}, false, classifyStoreError(err)
	}
	if view, created, found, err := findPluginGrantReplayTx(ctx, tx, tenantID, registrationID, idempotencyKey, requestDigest); err != nil {
		return contract.PluginGrantView{}, false, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return contract.PluginGrantView{}, false, classifyStoreError(err)
		}
		return view, created, nil
	}

	var currentVersion int64
	var currentEnabled bool
	var createdAt, updatedAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT version,enabled,created_at,updated_at
		FROM notification_plugin_grants
		WHERE tenant_id=$1 AND registration_id=$2
		FOR UPDATE
	`, tenantID, registrationID).Scan(&currentVersion, &currentEnabled, &createdAt, &updatedAt)
	exists := err == nil
	if err != nil && err != sql.ErrNoRows {
		return contract.PluginGrantView{}, false, classifyStoreError(err)
	}
	if !exists && update.ExpectedVersion != 0 || exists && (update.ExpectedVersion == 0 || update.ExpectedVersion != currentVersion) {
		return contract.PluginGrantView{}, false, storeErr(StoreErrorVersionConflict)
	}
	// Match PostgreSQL precision so initial and idempotently replayed views
	// contain identical timestamps.
	now := s.now().UTC().Truncate(time.Microsecond)
	nextVersion := int64(1)
	created := !exists
	if exists {
		nextVersion = currentVersion + 1
	} else {
		createdAt = now
	}
	updatedAt = now
	if _, _, err := beginIdempotencyTx(ctx, tx, tenantID, pluginGrantOperation(registrationID), idempotencyKey, requestDigest, "plugin_grant", registrationID, nextVersion); err != nil {
		return contract.PluginGrantView{}, false, err
	}
	if !exists {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notification_plugin_grants(tenant_id,registration_id,enabled,version,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6)
		`, tenantID, registrationID, update.Enabled, nextVersion, createdAt, updatedAt); err != nil {
			return contract.PluginGrantView{}, false, classifyStoreError(err)
		}
	} else if _, err := tx.ExecContext(ctx, `
		UPDATE notification_plugin_grants SET enabled=$3,version=$4,updated_at=$5
		WHERE tenant_id=$1 AND registration_id=$2
	`, tenantID, registrationID, update.Enabled, nextVersion, updatedAt); err != nil {
		return contract.PluginGrantView{}, false, classifyStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_plugin_grant_states(tenant_id,registration_id,version,enabled,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6)
	`, tenantID, registrationID, nextVersion, update.Enabled, createdAt, updatedAt); err != nil {
		return contract.PluginGrantView{}, false, classifyStoreError(err)
	}
	if err := tx.Commit(); err != nil {
		return contract.PluginGrantView{}, false, classifyStoreError(err)
	}
	view := contract.PluginGrantView{
		PluginRegistrationID: registrationID, TenantID: tenantID, Enabled: update.Enabled,
		Version: nextVersion, CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), UpdatedAt: updatedAt.UTC().Format(time.RFC3339Nano),
	}
	return view, created, nil
}

func findPluginGrantReplayTx(ctx context.Context, tx *sql.Tx, tenantID, registrationID, idempotencyKey, requestDigest string) (contract.PluginGrantView, bool, bool, error) {
	record, err := findIdempotencyTx(ctx, tx, tenantID, pluginGrantOperation(registrationID), idempotencyKey)
	if err != nil {
		return contract.PluginGrantView{}, false, false, err
	}
	if record == nil {
		return contract.PluginGrantView{}, false, false, nil
	}
	if record.Digest != requestDigest || record.ResourceType != "plugin_grant" || record.ResourceID != registrationID {
		return contract.PluginGrantView{}, false, false, storeErr(StoreErrorConflict)
	}
	view, err := getPluginGrantVersionTx(ctx, tx, tenantID, registrationID, record.ResourceVersion)
	if err != nil {
		return contract.PluginGrantView{}, false, false, err
	}
	return view, record.ResourceVersion == 1, true, nil
}

func (s *Store) ListPluginGrants(ctx context.Context, registrationID string, page, pageSize int) (contract.PluginGrantPage, error) {
	if page < 1 || pageSize < 1 || pageSize > maxPluginGrantPageSize || page > int(^uint(0)>>1)/pageSize {
		return contract.PluginGrantPage{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.PluginGrantPage{}, err
	}
	var total int64
	countQuery := `SELECT count(*) FROM notification_plugin_grants`
	listQuery := `
		SELECT tenant_id,registration_id,enabled,version,created_at,updated_at
		FROM notification_plugin_grants
		ORDER BY registration_id,tenant_id
		LIMIT $1 OFFSET $2
	`
	countArgs := []any{}
	listArgs := []any{pageSize, (page - 1) * pageSize}
	if registrationID != "" {
		countQuery += ` WHERE registration_id=$1`
		listQuery = `
			SELECT tenant_id,registration_id,enabled,version,created_at,updated_at
			FROM notification_plugin_grants
			WHERE registration_id=$1
			ORDER BY tenant_id
			LIMIT $2 OFFSET $3
		`
		countArgs = append(countArgs, registrationID)
		listArgs = []any{registrationID, pageSize, (page - 1) * pageSize}
	}
	if err := db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return contract.PluginGrantPage{}, classifyStoreError(err)
	}
	rows, err := db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return contract.PluginGrantPage{}, classifyStoreError(err)
	}
	defer rows.Close()
	items := make([]contract.PluginGrantView, 0, pageSize)
	for rows.Next() {
		var item contract.PluginGrantView
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&item.TenantID, &item.PluginRegistrationID, &item.Enabled, &item.Version, &createdAt, &updatedAt); err != nil {
			return contract.PluginGrantPage{}, classifyStoreError(err)
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return contract.PluginGrantPage{}, classifyStoreError(err)
	}
	return contract.PluginGrantPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func getPluginGrantVersionTx(ctx context.Context, tx *sql.Tx, tenantID, registrationID string, version int64) (contract.PluginGrantView, error) {
	var view contract.PluginGrantView
	var createdAt, updatedAt time.Time
	err := tx.QueryRowContext(ctx, `
		SELECT tenant_id,registration_id,enabled,version,created_at,updated_at
		FROM notification_plugin_grant_states
		WHERE tenant_id=$1 AND registration_id=$2 AND version=$3
	`, tenantID, registrationID, version).Scan(&view.TenantID, &view.PluginRegistrationID, &view.Enabled, &view.Version, &createdAt, &updatedAt)
	if err != nil {
		return contract.PluginGrantView{}, classifyStoreError(err)
	}
	view.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	view.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return view, nil
}

func (s *Store) UpdatePlatformPlugin(ctx context.Context, actorTenantID, registrationID, idempotencyKey, requestDigest string, update contract.PluginUpdate) (contract.PluginView, error) {
	const operationPrefix = "plugin.update.platform:"
	operation := operationPrefix + registrationID
	if !validIdempotencyInput(actorTenantID, operation, idempotencyKey, requestDigest) || registrationID == "" || update.ExpectedVersion < 1 {
		return contract.PluginView{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.PluginView{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	defer tx.Rollback()
	if record, err := findIdempotencyTx(ctx, tx, actorTenantID, operation, idempotencyKey); err != nil {
		return contract.PluginView{}, err
	} else if record != nil {
		if record.Digest != requestDigest || record.ResourceType != "plugin" || record.ResourceID != registrationID {
			return contract.PluginView{}, storeErr(StoreErrorConflict)
		}
		if err := tx.Commit(); err != nil {
			return contract.PluginView{}, classifyStoreError(err)
		}
		return s.getPlatformPluginVersion(ctx, registrationID, record.ResourceVersion)
	}
	var currentVersion int64
	err = tx.QueryRowContext(ctx, `SELECT version FROM notification_plugin_registrations WHERE id=$1 FOR UPDATE`, registrationID).Scan(&currentVersion)
	if err == sql.ErrNoRows {
		return contract.PluginView{}, storeErr(StoreErrorNotFound)
	}
	if err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	if record, err := findIdempotencyTx(ctx, tx, actorTenantID, operation, idempotencyKey); err != nil {
		return contract.PluginView{}, err
	} else if record != nil {
		if record.Digest != requestDigest || record.ResourceType != "plugin" || record.ResourceID != registrationID {
			return contract.PluginView{}, storeErr(StoreErrorConflict)
		}
		if err := tx.Commit(); err != nil {
			return contract.PluginView{}, classifyStoreError(err)
		}
		return s.getPlatformPluginVersion(ctx, registrationID, record.ResourceVersion)
	}
	if currentVersion != update.ExpectedVersion {
		return contract.PluginView{}, storeErr(StoreErrorVersionConflict)
	}
	nextVersion := currentVersion + 1
	if _, _, err := beginIdempotencyTx(ctx, tx, actorTenantID, operation, idempotencyKey, requestDigest, "plugin", registrationID, nextVersion); err != nil {
		return contract.PluginView{}, err
	}
	var health string
	if err := tx.QueryRowContext(ctx, `SELECT health FROM notification_plugin_registration_states WHERE registration_id=$1 AND version=$2`, registrationID, currentVersion).Scan(&health); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_plugin_registration_states(registration_id,version,enabled,health)
		VALUES($1,$2,$3,$4)
	`, registrationID, nextVersion, update.Enabled, health); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_plugin_registrations SET enabled=$2,version=$3 WHERE id=$1`, registrationID, update.Enabled, nextVersion); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	if err := tx.Commit(); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	return s.getPlatformPluginVersion(ctx, registrationID, nextVersion)
}

// RegisterPlatformPlugin stores a global registration without granting it to
// the installer tenant. Authorization is a separate explicit operation.
func (s *Store) RegisterPlatformPlugin(ctx context.Context, actorTenantID, idempotencyKey, requestDigest string, request contract.PluginRegistration, manifest contract.PluginManifestV1) (contract.PluginView, error) {
	return s.RegisterPlugin(ctx, actorTenantID, idempotencyKey, requestDigest, request, manifest)
}

func (s *Store) FindPlatformPluginRegistrationReplay(ctx context.Context, actorTenantID, idempotencyKey, requestDigest string) (contract.PluginView, bool, error) {
	replay, found, err := s.FindManagementReplay(ctx, actorTenantID, "plugin.register", idempotencyKey, requestDigest)
	if err != nil || !found {
		return contract.PluginView{}, found, err
	}
	if replay.ResourceType != "plugin" {
		return contract.PluginView{}, false, storeErr(StoreErrorConflict)
	}
	view, err := s.getPlatformPluginVersion(ctx, replay.ResourceID, replay.Version)
	return view, err == nil, err
}

func (s *Store) GetPlatformPlugin(ctx context.Context, registrationID string) (contract.PluginView, error) {
	if registrationID == "" {
		return contract.PluginView{}, storeErr(StoreErrorInvalid)
	}
	return s.getPlatformPluginVersion(ctx, registrationID, 0)
}

func (s *Store) ListPlatformPlugins(ctx context.Context) ([]contract.PluginView, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id,version FROM notification_plugin_registrations ORDER BY plugin_id,plugin_version,id`)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	type pluginKey struct {
		id      string
		version int64
	}
	keys := make([]pluginKey, 0)
	for rows.Next() {
		var key pluginKey
		if err := rows.Scan(&key.id, &key.version); err != nil {
			rows.Close()
			return nil, classifyStoreError(err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, classifyStoreError(err)
	}
	rows.Close()
	plugins := make([]contract.PluginView, 0, len(keys))
	for _, key := range keys {
		view, err := s.getPlatformPluginVersion(ctx, key.id, key.version)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, view)
	}
	return plugins, nil
}

func (s *Store) getPlatformPluginVersion(ctx context.Context, registrationID string, version int64) (contract.PluginView, error) {
	db, err := s.database()
	if err != nil {
		return contract.PluginView{}, err
	}
	var view contract.PluginView
	var manifest []byte
	var enabled bool
	var health string
	err = db.QueryRowContext(ctx, `
		SELECT r.id,r.plugin_id,r.plugin_version,r.origin,r.manifest_digest,r.manifest,s.enabled,s.health,s.version
		FROM notification_plugin_registrations r
		JOIN notification_plugin_registration_states s ON s.registration_id=r.id
		WHERE r.id=$1 AND s.version=COALESCE(NULLIF($2,0),r.version)
	`, registrationID, version).Scan(&view.ID, &view.PluginID, &view.PluginVersion, &view.Origin, &view.ManifestDigest, &manifest, &enabled, &health, &view.Version)
	if err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	var wire contract.PluginManifestV1
	if json.Unmarshal(manifest, &wire) != nil {
		return contract.PluginView{}, storeErr(StoreErrorUnavailable)
	}
	view.Enabled = enabled
	view.ManifestDigest = "sha256:" + strings.TrimSpace(view.ManifestDigest)
	view.Health = health
	view.Manifest = contract.PluginManifestSummary{
		PluginID: wire.PluginID, Name: wire.Name, PluginVersion: wire.PluginVersion,
		IdentityFields: append([]string(nil), wire.IdentityFields...),
		Channels:       append([]contract.Channel(nil), wire.Channels...), ContentModes: append([]string(nil), wire.ContentModes...),
		ConfigSchema: append(json.RawMessage(nil), wire.ConfigSchema...), RecipientSchema: append(json.RawMessage(nil), wire.RecipientSchema...),
		SecretFields: append([]string(nil), wire.SecretFields...), Capabilities: contract.PluginCapabilities{DeliveryReceipts: wire.Capabilities.DeliveryReceipts},
	}
	return view, nil
}
