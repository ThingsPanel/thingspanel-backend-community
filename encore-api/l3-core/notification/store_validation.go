package notification

import (
	"context"
	"database/sql"
	"encoding/json"
)

// ResolveInstanceConfig returns the immutable persisted config snapshot with
// its secret slots merged in memory for plugin validation. Disabled instances
// remain valid validation inputs; dispatch uses an enabled-only resolver.
// Callers must not log or serialize the returned bytes.
func (s *Store) ResolveInstanceConfig(ctx context.Context, tenantID, instanceID string, version int64) (json.RawMessage, error) {
	if tenantID == "" || instanceID == "" || version < 1 {
		return nil, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, classifyStoreError(err)
	}
	defer tx.Rollback()
	var configJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT c.config FROM notification_instances i JOIN notification_instance_configs c ON c.tenant_id=i.tenant_id AND c.instance_id=i.id JOIN notification_plugin_grants g ON g.tenant_id=i.tenant_id AND g.registration_id=c.plugin_registration_id AND g.enabled=TRUE JOIN notification_plugin_registrations p ON p.id=c.plugin_registration_id AND p.enabled=TRUE WHERE i.tenant_id=$1 AND i.id=$2 AND c.config_version=$3`, tenantID, instanceID, version).Scan(&configJSON)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	secrets, err := s.readInstanceSecretsTx(ctx, tx, tenantID, instanceID, version)
	if err != nil {
		return nil, err
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(configJSON, &config) != nil {
		return nil, storeErr(StoreErrorUnavailable)
	}
	for slot, value := range secrets {
		encoded, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return nil, storeErr(StoreErrorUnavailable)
		}
		config[slot] = encoded
	}
	result, err := json.Marshal(config)
	if err != nil {
		return nil, storeErr(StoreErrorUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, classifyStoreError(err)
	}
	return result, nil
}

// ResolveRegistrationRuntime resolves only a registration currently granted
// to the tenant. The returned secret is an internal, transient value.
func (s *Store) ResolveRegistrationRuntime(ctx context.Context, tenantID, registrationID string) (PluginRuntime, error) {
	if tenantID == "" || registrationID == "" {
		return PluginRuntime{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return PluginRuntime{}, err
	}
	var runtime PluginRuntime
	var keyRef string
	var ciphertext []byte
	var aadVersion int
	err = db.QueryRowContext(ctx, `SELECT p.id,p.registration_version,p.origin,p.auth_key_ref,p.auth_ciphertext,p.auth_aad_version FROM notification_plugin_grants g JOIN notification_plugin_registrations p ON p.id=g.registration_id WHERE g.tenant_id=$1 AND g.registration_id=$2 AND g.enabled=TRUE AND p.enabled=TRUE`, tenantID, registrationID).Scan(&runtime.RegistrationID, &runtime.RegistrationVersion, &runtime.Origin, &keyRef, &ciphertext, &aadVersion)
	if err != nil {
		return PluginRuntime{}, classifyStoreError(err)
	}
	plaintext, err := s.open(ctx, SnapshotAAD{ScopeKind: "platform", ResourceID: runtime.RegistrationID, Version: runtime.RegistrationVersion, Purpose: "plugin-auth", Slot: "authSecret"}, ProtectedSnapshot{KeyRef: keyRef, Ciphertext: ciphertext, AADVersion: aadVersion})
	if err != nil {
		return PluginRuntime{}, storeErr(StoreErrorUnavailable)
	}
	runtime.AuthSecret = string(plaintext)
	return runtime, nil
}
