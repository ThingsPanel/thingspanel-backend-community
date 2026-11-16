package devicemetadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"encore.app/devicemetadata/accessconfig"
	"encore.app/pkg/dbx"
)

// runtimeConfigurationSnapshot is frozen in the metadata transaction, never
// reconstructed by a retry from a newer template revision.
type runtimeConfigurationSnapshot struct {
	Generation    int64           `json:"generation"`
	Digest        string          `json:"digest"`
	Configuration json.RawMessage `json:"configuration"`
}

func canonicalRuntimeConfiguration(raw []byte) ([]byte, string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, "", err
	}
	if value == nil {
		return nil, "", fmt.Errorf("configuration must be an object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, "", fmt.Errorf("configuration has trailing data")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(hash[:]), nil
}

func freezeRuntimeConfigurationTx(ctx context.Context, tx dbx.Tx, tenantID, deviceID string) (*runtimeConfigurationSnapshot, error) {
	var kind string
	var generation int64
	var configuration, direct, parameters []byte
	err := tx.QueryRow(ctx, `SELECT d.device_kind,COALESCE(b.configuration_generation,1),t.configuration,b.direct_access,b.device_parameters
 FROM devices d LEFT JOIN device_access_bindings b ON b.tenant_id=d.tenant_id AND b.device_id=d.id
 LEFT JOIN device_access_templates t ON t.tenant_id=b.tenant_id AND t.id=b.template_id AND t.deleted_at IS NULL
 WHERE d.tenant_id=$1 AND d.id=$2 AND d.deleted_at IS NULL`, tenantID, deviceID).Scan(&kind, &generation, &configuration, &direct, &parameters)
	if err != nil {
		return nil, err
	}
	if kind != "standalone" {
		return nil, nil
	}
	origin, err := scanNativeImportedAccess(tx.QueryRow(ctx, nativeImportedAccessQuery, tenantID, deviceID))
	if err != nil {
		return nil, err
	}
	if origin != nil {
		return nil, nil
	}
	if len(configuration) == 0 || string(configuration) == "null" {
		configuration = direct
	}
	if len(configuration) == 0 || string(configuration) == "null" {
		return nil, nil
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &document); err != nil {
		return nil, err
	}
	var access struct {
		Mode     string `json:"mode"`
		Protocol string `json:"protocol"`
	}
	if err := json.Unmarshal(document["access"], &access); err != nil {
		return nil, err
	}
	if access.Mode != "direct" || access.Protocol != "mqtt" {
		return nil, nil
	}
	var accessPolicy accessconfig.AccessConfiguration
	if err := json.Unmarshal(document["access"], &accessPolicy); err != nil {
		return nil, err
	}
	if err := validateNativeMQTTRuntimeCapabilities(kind, accessPolicy); err != nil {
		return nil, err
	}
	document["deviceKind"] = json.RawMessage(`"standalone"`)
	if selection := document["model"]; len(selection) > 0 && string(selection) != "null" {
		model, err := resolveAccessTemplateModelBindingTx(ctx, tx, tenantID, selection)
		if err != nil {
			return nil, err
		}
		document["model"], err = json.Marshal(map[string]any{"modelId": model.ModelID, "snapshotId": model.SnapshotID, "publishedRevision": model.PublishedRevision})
		if err != nil {
			return nil, err
		}
	}
	// MQTT device parameters are not a separate runtime configuration namespace.
	// Reject nonempty overrides rather than silently pretending they were applied.
	if len(parameters) > 0 && !emptyJSONObject(parameters) {
		return nil, invalid("deviceParameters", "runtime MQTT parameter overrides are not supported")
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	canonical, digest, err := canonicalRuntimeConfiguration(encoded)
	if err != nil {
		return nil, err
	}
	return &runtimeConfigurationSnapshot{Generation: generation, Digest: digest, Configuration: canonical}, nil
}

func injectRuntimeConfigurationTx(ctx context.Context, tx dbx.Tx, tenantID, deviceID, kind string, encoded []byte) ([]byte, error) {
	if kind != operationKindCredentialChange && kind != operationKindTemplateBinding {
		return encoded, nil
	}
	var desired map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &desired); err != nil {
		return nil, err
	}
	if kind == operationKindCredentialChange {
		var change string
		_ = json.Unmarshal(desired["change"], &change)
		if change != "activation" {
			return encoded, nil
		}
	}
	snapshot, err := freezeRuntimeConfigurationTx(ctx, tx, tenantID, deviceID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return encoded, nil
	}
	desired["runtimeConfiguration"], err = json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	return json.Marshal(desired)
}

// runtimeConfigurationNeedsSyncTx also repairs pre-snapshot registrations when
// an administrator explicitly saves an unchanged binding/template.
func runtimeConfigurationNeedsSyncTx(ctx context.Context, tx dbx.Tx, tenantID, deviceID string) (bool, error) {
	snapshot, err := freezeRuntimeConfigurationTx(ctx, tx, tenantID, deviceID)
	if err != nil {
		return false, err
	}
	if snapshot == nil {
		return false, nil
	}
	var found bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_runtime_outbox o JOIN metadata_operations m ON m.tenant_id=o.tenant_id AND m.id=o.operation_id
 WHERE o.tenant_id=$1 AND o.device_id=$2 AND o.desired->'runtimeConfiguration'->>'digest'=$3
 AND o.desired->'runtimeConfiguration'->>'generation'=$4
 AND (o.state='applied' OR m.state='pending'))`, tenantID, deviceID, snapshot.Digest, fmt.Sprint(snapshot.Generation)).Scan(&found)
	return !found, err
}

func enqueueTemplateRuntimeConfigurationsTx(ctx context.Context, tx dbx.Tx, tenantID, subjectID, templateID, requestID string) error {
	rows, err := tx.Query(ctx, `SELECT d.id::text FROM devices d JOIN device_access_bindings b ON b.tenant_id=d.tenant_id AND b.device_id=d.id
 WHERE d.tenant_id=$1 AND b.template_id=$2 AND d.deleted_at IS NULL ORDER BY d.id FOR UPDATE OF d,b`, tenantID, templateID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		needed, err := runtimeConfigurationNeedsSyncTx(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if !needed {
			continue
		}
		snapshot, err := freezeRuntimeConfigurationTx(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if snapshot.Generation >= 9007199254740991 {
			return nativeDeviceConflict("device configuration version is exhausted")
		}
		operation, err := createDeviceOperationTx(ctx, tx, tenantID, subjectID, operationKindTemplateBinding, id, "updateDeviceAccessTemplate", requestID, snapshot.Digest, map[string]any{"awaiting": "runtime-application"})
		if err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `UPDATE device_access_bindings SET configuration_generation=configuration_generation+1,pending_operation_id=$3,updated_by=$4,updated_at=NOW() WHERE tenant_id=$1 AND device_id=$2 AND configuration_generation=$5`, tenantID, id, operation, subjectID, snapshot.Generation)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return nativeDeviceConflict("device configuration changed concurrently")
		}
		result, err = tx.Exec(ctx, `UPDATE devices SET resource_version=resource_version+1,updated_at=NOW(),updated_by=$3 WHERE tenant_id=$1 AND id=$2 AND resource_version<9007199254740991`, tenantID, id, subjectID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return nativeDeviceConflict("device resource version is exhausted")
		}
		if err = enqueueDeviceRuntimeTx(ctx, tx, tenantID, id, operation, operationKindTemplateBinding, snapshot.Generation+1, map[string]any{"deviceId": id, "templateId": templateID, "configurationGeneration": snapshot.Generation + 1}); err != nil {
			return err
		}
	}
	return nil
}

// Runtime capabilities are checked before saving a native MQTT access intent.
// Unsupported settings must not become a permanently pending owner operation.
func validateNativeMQTTRuntimeCapabilities(kind string, access accessconfig.AccessConfiguration) error {
	if kind != "standalone" || access.Mode != "direct" || access.Protocol != "mqtt" {
		return nil
	}
	if !emptyJSONObject(access.Parameters) {
		return invalid("access.parameters", "built-in MQTT parameters are not supported")
	}
	if access.CredentialPolicy.AutoRegister {
		return invalid("access.credentialPolicy.autoRegister", "native automatic registration is not supported")
	}
	if access.CredentialPolicy.Kind != "none" {
		return invalid("access.credentialPolicy.kind", "template credential policy is not supported; use managed device credentials")
	}
	if access.OnlinePolicy.Mode == "transport" && access.OnlinePolicy.TimeoutSeconds != 60 {
		return invalid("access.onlinePolicy.timeoutSeconds", "transport status uses the MQTT connection; custom timeout is not supported")
	}
	return nil
}
