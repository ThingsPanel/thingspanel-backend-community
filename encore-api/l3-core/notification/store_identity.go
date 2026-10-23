package notification

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"encore.app/l3-core/notification/contract"
	"github.com/google/uuid"
)

// RegisterPlugin stores a platform-level registration version. Tenant access
// is created separately through the explicit grant operation.
func (s *Store) RegisterPlugin(ctx context.Context, tenantID, idempotencyKey, requestDigest string, request contract.PluginRegistration, manifest contract.PluginManifestV1) (contract.PluginView, error) {
	if !validIdempotencyInput(tenantID, "plugin.register", idempotencyKey, requestDigest) || request.AuthSecret == "" || request.PluginID == "" || request.PluginVersion == "" || request.Origin == "" || request.ManifestDigest == "" {
		return contract.PluginView{}, storeErr(StoreErrorInvalid)
	}
	if manifest.PluginID != request.PluginID || manifest.PluginVersion != request.PluginVersion || manifest.APIVersion != "1.0" || !validIdentityFieldDeclarations(manifest.IdentityFields) {
		return contract.PluginView{}, storeErr(StoreErrorInvalid)
	}
	// The wire uses sha256:<hex>; the column stores only the 64-byte hash.
	manifestHash := strings.TrimPrefix(request.ManifestDigest, "sha256:")
	if len(manifestHash) != 64 {
		return contract.PluginView{}, storeErr(StoreErrorInvalid)
	}
	if _, err := hex.DecodeString(manifestHash); err != nil {
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

	registrationID := uuid.NewString()
	record, replay, err := beginIdempotencyTx(ctx, tx, tenantID, "plugin.register", idempotencyKey, requestDigest, "plugin", registrationID, 1)
	if err != nil {
		return contract.PluginView{}, err
	}
	if replay {
		if err := tx.Commit(); err != nil {
			return contract.PluginView{}, classifyStoreError(err)
		}
		return s.getPlatformPluginVersion(ctx, record.ResourceID, record.ResourceVersion)
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return contract.PluginView{}, storeErr(StoreErrorInvalid)
	}
	protected, err := s.seal(ctx, SnapshotAAD{
		ScopeKind:  "platform",
		ResourceID: registrationID,
		Version:    1,
		Purpose:    "plugin-auth",
		Slot:       "authSecret",
	}, []byte(request.AuthSecret))
	if err != nil {
		return contract.PluginView{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_plugin_registrations
			(id, plugin_id, plugin_version, origin, manifest_digest, manifest,
			 auth_key_ref, auth_ciphertext, auth_aad_version, registration_version,
			 enabled, health, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, TRUE, 'unknown', 1)
	`, registrationID, request.PluginID, request.PluginVersion, request.Origin, manifestHash,
		string(manifestJSON), protected.KeyRef, protected.Ciphertext, protected.AADVersion); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_plugin_registration_states(registration_id, version, enabled, health)
		VALUES ($1, 1, TRUE, 'unknown')
	`, registrationID); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	if err := tx.Commit(); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	return s.getPlatformPluginVersion(ctx, registrationID, 1)
}

func (s *Store) GetPlugin(ctx context.Context, tenantID, registrationID string) (contract.PluginView, error) {
	return s.getPluginVersion(ctx, tenantID, registrationID, 0)
}

func (s *Store) ListPlugins(ctx context.Context, tenantID string) ([]contract.PluginView, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT r.id, r.version
		FROM notification_plugin_grants g
		JOIN notification_plugin_registrations r ON r.id = g.registration_id
		WHERE g.tenant_id = $1 AND g.enabled=TRUE
		ORDER BY r.plugin_id, r.plugin_version, r.id
	`, tenantID)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	type idVersion struct {
		id      string
		version int64
	}
	var keys []idVersion
	for rows.Next() {
		var key idVersion
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
		plugin, err := s.getPluginVersion(ctx, tenantID, key.id, key.version)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, plugin)
	}
	return plugins, nil
}

func (s *Store) UpdatePlugin(ctx context.Context, tenantID, registrationID, idempotencyKey, requestDigest string, update contract.PluginUpdate) (contract.PluginView, error) {
	if !validIdempotencyInput(tenantID, "plugin.update", idempotencyKey, requestDigest) || registrationID == "" || update.ExpectedVersion < 1 {
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

	if record, err := findIdempotencyTx(ctx, tx, tenantID, "plugin.update", idempotencyKey); err != nil {
		return contract.PluginView{}, err
	} else if record != nil {
		if record.Digest != requestDigest {
			return contract.PluginView{}, storeErr(StoreErrorConflict)
		}
		if err := tx.Commit(); err != nil {
			return contract.PluginView{}, classifyStoreError(err)
		}
		return s.getPluginVersion(ctx, tenantID, record.ResourceID, record.ResourceVersion)
	}

	var currentVersion int64
	err = tx.QueryRowContext(ctx, `
		SELECT r.version
		FROM notification_plugin_registrations r
		JOIN notification_plugin_grants g ON g.registration_id = r.id AND g.tenant_id = $1
		WHERE r.id = $2
		FOR UPDATE OF r
	`, tenantID, registrationID).Scan(&currentVersion)
	if err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	if record, err := findIdempotencyTx(ctx, tx, tenantID, "plugin.update", idempotencyKey); err != nil {
		return contract.PluginView{}, err
	} else if record != nil {
		if record.Digest != requestDigest {
			return contract.PluginView{}, storeErr(StoreErrorConflict)
		}
		if err := tx.Commit(); err != nil {
			return contract.PluginView{}, classifyStoreError(err)
		}
		return s.getPluginVersion(ctx, tenantID, record.ResourceID, record.ResourceVersion)
	}
	if currentVersion != update.ExpectedVersion {
		return contract.PluginView{}, storeErr(StoreErrorVersionConflict)
	}
	nextVersion := currentVersion + 1
	if _, _, err := beginIdempotencyTx(ctx, tx, tenantID, "plugin.update", idempotencyKey, requestDigest, "plugin", registrationID, nextVersion); err != nil {
		return contract.PluginView{}, err
	}
	var health string
	if err := tx.QueryRowContext(ctx, `SELECT health FROM notification_plugin_registration_states WHERE registration_id = $1 AND version = $2`, registrationID, currentVersion).Scan(&health); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_plugin_registration_states(registration_id, version, enabled, health)
		VALUES ($1, $2, $3, $4)
	`, registrationID, nextVersion, update.Enabled, health); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_plugin_registrations SET enabled = $2, version = $3 WHERE id = $1`, registrationID, update.Enabled, nextVersion); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	if err := tx.Commit(); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	return s.getPluginVersion(ctx, tenantID, registrationID, nextVersion)
}

func (s *Store) getPluginVersion(ctx context.Context, tenantID, registrationID string, version int64) (contract.PluginView, error) {
	db, err := s.database()
	if err != nil {
		return contract.PluginView{}, err
	}
	var (
		view         contract.PluginView
		manifest     []byte
		health       string
		stateEnabled bool
	)
	query := `
		SELECT r.id, r.plugin_id, r.plugin_version, r.origin, r.manifest_digest,
		       r.manifest, s.enabled, s.health, s.version
		FROM notification_plugin_grants g
		JOIN notification_plugin_registrations r ON r.id = g.registration_id
		JOIN notification_plugin_registration_states s ON s.registration_id = r.id
		WHERE g.tenant_id = $1 AND g.enabled=TRUE AND r.id = $2 AND s.version = COALESCE(NULLIF($3, 0), r.version)
	`
	if err := db.QueryRowContext(ctx, query, tenantID, registrationID, version).Scan(
		&view.ID, &view.PluginID, &view.PluginVersion, &view.Origin, &view.ManifestDigest,
		&manifest, &stateEnabled, &health, &view.Version,
	); err != nil {
		return contract.PluginView{}, classifyStoreError(err)
	}
	var wire contract.PluginManifestV1
	if err := json.Unmarshal(manifest, &wire); err != nil {
		return contract.PluginView{}, storeErr(StoreErrorUnavailable)
	}
	view.Enabled = stateEnabled
	view.ManifestDigest = "sha256:" + strings.TrimSpace(view.ManifestDigest)
	view.Health = health
	view.Manifest = contract.PluginManifestSummary{
		PluginID:        wire.PluginID,
		Name:            wire.Name,
		PluginVersion:   wire.PluginVersion,
		IdentityFields:  append([]string(nil), wire.IdentityFields...),
		Channels:        append([]contract.Channel(nil), wire.Channels...),
		ContentModes:    append([]string(nil), wire.ContentModes...),
		ConfigSchema:    append(json.RawMessage(nil), wire.ConfigSchema...),
		RecipientSchema: append(json.RawMessage(nil), wire.RecipientSchema...),
		SecretFields:    append([]string(nil), wire.SecretFields...),
		Capabilities:    contract.PluginCapabilities{DeliveryReceipts: wire.Capabilities.DeliveryReceipts},
	}
	return view, nil
}

func (s *Store) CreateInstance(ctx context.Context, tenantID, idempotencyKey, requestDigest string, request contract.InstanceCreate, secretValues map[string]string) (contract.InstanceView, error) {
	if !validIdempotencyInput(tenantID, "instance.create", idempotencyKey, requestDigest) || request.PluginRegistrationID == "" || request.Name == "" || request.Channel == "" {
		return contract.InstanceView{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.InstanceView{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return contract.InstanceView{}, classifyStoreError(err)
	}
	defer tx.Rollback()

	instanceID := uuid.NewString()
	record, replay, err := beginIdempotencyTx(ctx, tx, tenantID, "instance.create", idempotencyKey, requestDigest, "instance", instanceID, 1)
	if err != nil {
		return contract.InstanceView{}, err
	}
	if replay {
		if err := tx.Commit(); err != nil {
			return contract.InstanceView{}, classifyStoreError(err)
		}
		return s.getInstanceVersion(ctx, tenantID, record.ResourceID, record.ResourceVersion)
	}
	if err := ensureGrantedPluginTx(ctx, tx, tenantID, request.PluginRegistrationID, request.Channel, request.Config, secretValues); err != nil {
		return contract.InstanceView{}, err
	}
	config, err := normalizeJSONObject(request.Config)
	if err != nil {
		return contract.InstanceView{}, err
	}
	providerIdentity, err := json.Marshal(request.ProviderIdentity)
	if err != nil {
		return contract.InstanceView{}, storeErr(StoreErrorInvalid)
	}
	secretState := secretStateFor(secretValues)
	secretStateJSON, _ := json.Marshal(secretState)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_instances(tenant_id, id, name, enabled, version, current_config_version)
		VALUES ($1, $2, $3, FALSE, 1, 1)
	`, tenantID, instanceID, request.Name); err != nil {
		return contract.InstanceView{}, classifyStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_instance_configs
			(tenant_id, instance_id, config_version, plugin_registration_id, channel, name, enabled, config, provider_identity, secret_state)
		VALUES ($1, $2, 1, $3, $4, $5, FALSE, $6, $7, $8)
	`, tenantID, instanceID, request.PluginRegistrationID, request.Channel, request.Name, string(config), string(providerIdentity), string(secretStateJSON)); err != nil {
		return contract.InstanceView{}, classifyStoreError(err)
	}
	if err := s.insertInstanceSecrets(ctx, tx, tenantID, instanceID, 1, secretValues); err != nil {
		return contract.InstanceView{}, err
	}
	if err := tx.Commit(); err != nil {
		return contract.InstanceView{}, classifyStoreError(err)
	}
	return s.getInstanceVersion(ctx, tenantID, instanceID, 1)
}

func (s *Store) GetInstance(ctx context.Context, tenantID, instanceID string) (contract.InstanceView, error) {
	return s.getInstanceVersion(ctx, tenantID, instanceID, 0)
}

func (s *Store) ListInstances(ctx context.Context, tenantID string) ([]contract.InstanceView, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, current_config_version FROM notification_instances WHERE tenant_id = $1 ORDER BY created_at, id`, tenantID)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	type rowRef struct {
		id      string
		version int64
	}
	var refs []rowRef
	for rows.Next() {
		var ref rowRef
		if err := rows.Scan(&ref.id, &ref.version); err != nil {
			rows.Close()
			return nil, classifyStoreError(err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, classifyStoreError(err)
	}
	rows.Close()
	instances := make([]contract.InstanceView, 0, len(refs))
	for _, ref := range refs {
		view, err := s.getInstanceVersion(ctx, tenantID, ref.id, ref.version)
		if err != nil {
			return nil, err
		}
		instances = append(instances, view)
	}
	return instances, nil
}

func (s *Store) UpdateInstance(ctx context.Context, tenantID, instanceID, idempotencyKey, requestDigest string, update contract.InstanceUpdate) (contract.InstanceView, error) {
	if !validIdempotencyInput(tenantID, "instance.update", idempotencyKey, requestDigest) || instanceID == "" || update.ExpectedVersion < 1 {
		return contract.InstanceView{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.InstanceView{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return contract.InstanceView{}, classifyStoreError(err)
	}
	defer tx.Rollback()

	if record, err := findIdempotencyTx(ctx, tx, tenantID, "instance.update", idempotencyKey); err != nil {
		return contract.InstanceView{}, err
	} else if record != nil {
		if record.Digest != requestDigest {
			return contract.InstanceView{}, storeErr(StoreErrorConflict)
		}
		if err := tx.Commit(); err != nil {
			return contract.InstanceView{}, classifyStoreError(err)
		}
		return s.getInstanceVersion(ctx, tenantID, record.ResourceID, record.ResourceVersion)
	}
	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM notification_instances WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, instanceID).Scan(&currentVersion); err != nil {
		return contract.InstanceView{}, classifyStoreError(err)
	}
	if record, err := findIdempotencyTx(ctx, tx, tenantID, "instance.update", idempotencyKey); err != nil {
		return contract.InstanceView{}, err
	} else if record != nil {
		if record.Digest != requestDigest {
			return contract.InstanceView{}, storeErr(StoreErrorConflict)
		}
		if err := tx.Commit(); err != nil {
			return contract.InstanceView{}, classifyStoreError(err)
		}
		return s.getInstanceVersion(ctx, tenantID, record.ResourceID, record.ResourceVersion)
	}
	if currentVersion != update.ExpectedVersion {
		return contract.InstanceView{}, storeErr(StoreErrorVersionConflict)
	}
	old, secrets, err := s.loadInstanceSnapshotTx(ctx, tx, tenantID, instanceID, currentVersion)
	if err != nil {
		return contract.InstanceView{}, err
	}
	beforeConfig, err := completeInstanceConfig(old.Config, secrets)
	if err != nil {
		return contract.InstanceView{}, storeErr(StoreErrorUnavailable)
	}
	if update.Name != nil {
		old.Name = *update.Name
	}
	if update.Enabled != nil {
		old.Enabled = *update.Enabled
	}
	if len(update.ConfigPatch) > 0 {
		old.Config, err = mergeConfigPatchFields(old.Config, update.ConfigPatch)
		if err != nil {
			return contract.InstanceView{}, err
		}
	}
	if update.Secrets != nil {
		for key, value := range update.Secrets.Set {
			if containsString(update.Secrets.Clear, key) {
				return contract.InstanceView{}, storeErr(StoreErrorInvalid)
			}
			secrets[key] = value
		}
		for _, key := range update.Secrets.Clear {
			delete(secrets, key)
		}
	}
	identityFields, err := loadPluginIdentityFieldsTx(ctx, tx, old.PluginRegistrationID)
	if err != nil {
		return contract.InstanceView{}, err
	}
	afterConfig, err := completeInstanceConfig(old.Config, secrets)
	if err != nil {
		return contract.InstanceView{}, storeErr(StoreErrorUnavailable)
	}
	identityChanged, err := identityFieldsChanged(identityFields, beforeConfig, afterConfig)
	if err != nil {
		return contract.InstanceView{}, storeErr(StoreErrorUnavailable)
	}
	if identityChanged {
		return contract.InstanceView{}, storeErr(StoreErrorConflict)
	}
	nextVersion := currentVersion + 1
	if _, _, err := beginIdempotencyTx(ctx, tx, tenantID, "instance.update", idempotencyKey, requestDigest, "instance", instanceID, nextVersion); err != nil {
		return contract.InstanceView{}, err
	}
	secretState := secretStateFor(secrets)
	secretStateJSON, _ := json.Marshal(secretState)
	configJSON, _ := json.Marshal(old.Config)
	providerJSON, _ := json.Marshal(old.ProviderIdentity)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_instance_configs
			(tenant_id, instance_id, config_version, plugin_registration_id, channel, name, enabled, config, provider_identity, secret_state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, tenantID, instanceID, nextVersion, old.PluginRegistrationID, old.Channel, old.Name, old.Enabled, string(configJSON), string(providerJSON), string(secretStateJSON)); err != nil {
		return contract.InstanceView{}, classifyStoreError(err)
	}
	if err := s.insertInstanceSecrets(ctx, tx, tenantID, instanceID, nextVersion, secrets); err != nil {
		return contract.InstanceView{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE notification_instances
		SET name = $3, enabled = $4, version = $5, current_config_version = $5, updated_at = $6
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, instanceID, old.Name, old.Enabled, nextVersion, s.now()); err != nil {
		return contract.InstanceView{}, classifyStoreError(err)
	}
	if err := tx.Commit(); err != nil {
		return contract.InstanceView{}, classifyStoreError(err)
	}
	return s.getInstanceVersion(ctx, tenantID, instanceID, nextVersion)
}

func loadPluginIdentityFieldsTx(ctx context.Context, tx *sql.Tx, registrationID string) ([]string, error) {
	if registrationID == "" {
		return nil, storeErr(StoreErrorUnavailable)
	}
	var manifestJSON []byte
	if err := tx.QueryRowContext(ctx, `SELECT manifest FROM notification_plugin_registrations WHERE id=$1`, registrationID).Scan(&manifestJSON); err != nil {
		return nil, classifyStoreError(err)
	}
	if !validateIdentityFieldsPayload(manifestJSON) {
		return nil, storeErr(StoreErrorUnavailable)
	}
	var manifest contract.PluginManifestV1
	if json.Unmarshal(manifestJSON, &manifest) != nil || !validIdentityFieldDeclarations(manifest.IdentityFields) {
		return nil, storeErr(StoreErrorUnavailable)
	}
	return append([]string(nil), manifest.IdentityFields...), nil
}

func (s *Store) getInstanceVersion(ctx context.Context, tenantID, instanceID string, version int64) (contract.InstanceView, error) {
	db, err := s.database()
	if err != nil {
		return contract.InstanceView{}, err
	}
	var (
		view            contract.InstanceView
		configJSON      []byte
		providerJSON    []byte
		secretStateJSON []byte
	)
	query := `
		SELECT c.instance_id, c.plugin_registration_id, c.name, c.channel, c.enabled,
		       c.config_version, c.config, c.provider_identity, c.secret_state
		FROM notification_instances i
		JOIN notification_instance_configs c
		  ON c.tenant_id = i.tenant_id AND c.instance_id = i.id
		 AND c.config_version = COALESCE(NULLIF($3, 0), i.current_config_version)
		WHERE i.tenant_id = $1 AND i.id = $2
	`
	if err := db.QueryRowContext(ctx, query, tenantID, instanceID, version).Scan(
		&view.ID, &view.PluginRegistrationID, &view.Name, &view.Channel, &view.Enabled,
		&view.ConfigVersion, &configJSON, &providerJSON, &secretStateJSON,
	); err != nil {
		return contract.InstanceView{}, classifyStoreError(err)
	}
	view.Version = view.ConfigVersion
	if err := json.Unmarshal(configJSON, &view.Config); err != nil {
		return contract.InstanceView{}, storeErr(StoreErrorUnavailable)
	}
	if view.Config == nil {
		view.Config = map[string]json.RawMessage{}
	}
	if err := json.Unmarshal(providerJSON, &view.ProviderIdentity); err != nil {
		return contract.InstanceView{}, storeErr(StoreErrorUnavailable)
	}
	if view.ProviderIdentity == nil {
		view.ProviderIdentity = map[string]string{}
	}
	if err := json.Unmarshal(secretStateJSON, &view.SecretState); err != nil {
		return contract.InstanceView{}, storeErr(StoreErrorUnavailable)
	}
	if view.SecretState == nil {
		view.SecretState = map[string]bool{}
	}
	return view, nil
}

func (s *Store) loadInstanceSnapshotTx(ctx context.Context, tx *sql.Tx, tenantID, instanceID string, version int64) (contract.InstanceView, map[string]string, error) {
	var (
		view                                      contract.InstanceView
		configJSON, providerJSON, secretStateJSON []byte
	)
	err := tx.QueryRowContext(ctx, `
		SELECT c.instance_id, c.plugin_registration_id, c.name, c.channel, c.enabled,
		       c.config_version, c.config, c.provider_identity, c.secret_state
		FROM notification_instance_configs c
		WHERE c.tenant_id = $1 AND c.instance_id = $2 AND c.config_version = $3
	`, tenantID, instanceID, version).Scan(&view.ID, &view.PluginRegistrationID, &view.Name, &view.Channel, &view.Enabled, &view.ConfigVersion, &configJSON, &providerJSON, &secretStateJSON)
	if err != nil {
		return contract.InstanceView{}, nil, classifyStoreError(err)
	}
	view.Version = view.ConfigVersion
	if err := json.Unmarshal(configJSON, &view.Config); err != nil {
		return contract.InstanceView{}, nil, storeErr(StoreErrorUnavailable)
	}
	if err := json.Unmarshal(providerJSON, &view.ProviderIdentity); err != nil {
		return contract.InstanceView{}, nil, storeErr(StoreErrorUnavailable)
	}
	if err := json.Unmarshal(secretStateJSON, &view.SecretState); err != nil {
		return contract.InstanceView{}, nil, storeErr(StoreErrorUnavailable)
	}
	secrets, err := s.readInstanceSecretsTx(ctx, tx, tenantID, instanceID, version)
	if err != nil {
		return contract.InstanceView{}, nil, err
	}
	return view, secrets, nil
}

func (s *Store) insertInstanceSecrets(ctx context.Context, tx *sql.Tx, tenantID, instanceID string, version int64, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(key) != key {
			return storeErr(StoreErrorInvalid)
		}
		secretJSON, err := json.Marshal(values[key])
		if err != nil {
			return storeErr(StoreErrorInvalid)
		}
		protected, err := s.seal(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: tenantID, ResourceID: instanceID, Version: version, Purpose: "instance-secret", Slot: key}, secretJSON)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notification_instance_secrets(tenant_id, instance_id, config_version, slot, key_ref, ciphertext, aad_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, tenantID, instanceID, version, key, protected.KeyRef, protected.Ciphertext, protected.AADVersion); err != nil {
			return classifyStoreError(err)
		}
	}
	return nil
}

func (s *Store) readInstanceSecretsTx(ctx context.Context, tx *sql.Tx, tenantID, instanceID string, version int64) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT slot, key_ref, ciphertext, aad_version
		FROM notification_instance_secrets
		WHERE tenant_id = $1 AND instance_id = $2 AND config_version = $3
		ORDER BY slot
	`, tenantID, instanceID, version)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	defer rows.Close()
	secrets := map[string]string{}
	for rows.Next() {
		var slot, keyRef string
		var ciphertext []byte
		var aadVersion int
		if err := rows.Scan(&slot, &keyRef, &ciphertext, &aadVersion); err != nil {
			return nil, classifyStoreError(err)
		}
		plaintext, err := s.open(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: tenantID, ResourceID: instanceID, Version: version, Purpose: "instance-secret", Slot: slot}, ProtectedSnapshot{
			KeyRef: keyRef, Ciphertext: ciphertext, AADVersion: aadVersion,
		})
		if err != nil {
			return nil, err
		}
		var value string
		if err := json.Unmarshal(plaintext, &value); err != nil {
			return nil, storeErr(StoreErrorUnavailable)
		}
		secrets[slot] = value
	}
	if err := rows.Err(); err != nil {
		return nil, classifyStoreError(err)
	}
	return secrets, nil
}

func ensureGrantedPluginTx(ctx context.Context, tx *sql.Tx, tenantID, registrationID string, channel contract.Channel, config json.RawMessage, secretValues map[string]string) error {
	var enabled bool
	var manifestJSON []byte
	err := tx.QueryRowContext(ctx, `
		SELECT r.enabled, r.manifest
		FROM notification_plugin_registrations r
		JOIN notification_plugin_grants g ON g.registration_id = r.id
		WHERE g.tenant_id = $1 AND r.id = $2 AND g.enabled = TRUE
	`, tenantID, registrationID).Scan(&enabled, &manifestJSON)
	if err != nil {
		return classifyStoreError(err)
	}
	if !enabled {
		return storeErr(StoreErrorConflict)
	}
	var manifest contract.PluginManifestV1
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		return storeErr(StoreErrorUnavailable)
	}
	channelFound := false
	for _, allowed := range manifest.Channels {
		if allowed == channel {
			channelFound = true
			break
		}
	}
	if !channelFound {
		return storeErr(StoreErrorInvalid)
	}
	var configValues map[string]json.RawMessage
	if err := json.Unmarshal(config, &configValues); err != nil {
		return storeErr(StoreErrorInvalid)
	}
	secretFields := make(map[string]struct{}, len(manifest.SecretFields))
	for _, secretField := range manifest.SecretFields {
		secretFields[secretField] = struct{}{}
		if _, included := configValues[secretField]; included {
			return storeErr(StoreErrorInvalid)
		}
	}
	for secretField := range secretValues {
		if _, declared := secretFields[secretField]; !declared {
			return storeErr(StoreErrorInvalid)
		}
	}
	return nil
}

func normalizeJSONObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, storeErr(StoreErrorInvalid)
	}
	return append(json.RawMessage(nil), raw...), nil
}

func secretStateFor(values map[string]string) map[string]bool {
	state := make(map[string]bool, len(values))
	for key := range values {
		state[key] = true
	}
	return state
}

func mergeConfigPatchFields(original map[string]json.RawMessage, patch json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(patch, &fields); err != nil || fields == nil {
		return nil, storeErr(StoreErrorInvalid)
	}
	merged := make(map[string]json.RawMessage, len(original)+len(fields))
	for key, value := range original {
		merged[key] = append(json.RawMessage(nil), value...)
	}
	for key, value := range fields {
		merged[key] = append(json.RawMessage(nil), value...)
	}
	return merged, nil
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
