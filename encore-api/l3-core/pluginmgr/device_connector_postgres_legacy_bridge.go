package pluginmgr

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type postgresLegacyBridge struct {
	db *sql.DB
}

func newPostgresLegacyBridge(db *sql.DB) LegacyBridge {
	return &postgresLegacyBridge{db: db}
}

func (b *postgresLegacyBridge) ProjectConnector(ctx context.Context, dc DeviceConnector) (string, string, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	servicePluginID := newID()
	protocolPluginID := newID()
	deviceType := legacyDeviceType(dc.DeviceTypeSupport)
	dictCode := legacyDictCode(dc.DeviceTypeSupport)
	serviceType := legacyServiceType(dc.AdditionalInfo)
	serviceConfig, err := json.Marshal(map[string]any{
		"http_address":     "",
		"device_type":      deviceType,
		"sub_topic_prefix": dc.MQTTTopicPrefix,
		"access_address":   "",
	})
	if err != nil {
		return "", "", err
	}

	if err := tx.QueryRowContext(ctx, `
		INSERT INTO service_plugins (
			id, name, service_identifier, service_type, last_active_time, version,
			create_at, update_at, description, service_config, remark
		) VALUES ($1,$2,$3,$4,$5,'v1.0.0',$5,$5,$6,$7::json,'device-connector bridge')
		ON CONFLICT (service_identifier) DO UPDATE
		   SET name = EXCLUDED.name,
		       service_type = EXCLUDED.service_type,
		       update_at = EXCLUDED.update_at,
		       description = EXCLUDED.description,
		       service_config = EXCLUDED.service_config,
		       remark = EXCLUDED.remark
		RETURNING id
	`, servicePluginID, dc.DisplayName, dc.ConnectorKey, serviceType, now, nullString(dc.Description), string(serviceConfig)).Scan(&servicePluginID); err != nil {
		return "", "", fmt.Errorf("project service_plugins: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM protocol_plugins WHERE protocol_type = $1`, dc.ConnectorKey); err != nil {
		return "", "", fmt.Errorf("replace protocol_plugins: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO protocol_plugins (
			id, name, device_type, protocol_type, access_address, http_address,
			sub_topic_prefix, description, additional_info, created_at, update_at, remark
		) VALUES ($1,$2,$3,$4,'','',$5,$6,$7,$8,$8,'device-connector bridge')
	`, protocolPluginID, dc.DisplayName, deviceType, dc.ConnectorKey, dc.MQTTTopicPrefix,
		nullString(dc.Description), connectorAdditionalInfo(dc), now); err != nil {
		return "", "", fmt.Errorf("project protocol_plugins: %w", err)
	}

	dictID := newID()
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO sys_dict (id, dict_code, dict_value, created_at, remark)
		VALUES ($1,$2,$3,$4,'device-connector bridge')
		ON CONFLICT (dict_code, dict_value) DO UPDATE
		   SET remark = EXCLUDED.remark
		RETURNING id
	`, dictID, dictCode, dc.ConnectorKey, now).Scan(&dictID); err != nil {
		return "", "", fmt.Errorf("project sys_dict: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sys_dict_language (id, dict_id, language_code, translation)
		VALUES ($1,$2,'zh-CN',$3)
		ON CONFLICT (dict_id, language_code) DO UPDATE
		   SET translation = EXCLUDED.translation
	`, newID(), dictID, dc.DisplayName); err != nil {
		return "", "", fmt.Errorf("project sys_dict_language: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return servicePluginID, protocolPluginID, nil
}

func (b *postgresLegacyBridge) UpdateHTTPAddress(ctx context.Context, connectorKey, httpAddress string) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE service_plugins
		   SET service_config = jsonb_set(
		           coalesce(service_config::jsonb, '{}'::jsonb),
		           '{http_address}',
		           to_jsonb($2::text),
		           true
		       )::json,
		       update_at = now()
		 WHERE service_identifier = $1
	`, connectorKey, httpAddress); err != nil {
		return fmt.Errorf("update service_plugins http_address: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE protocol_plugins
		   SET http_address = $2,
		       update_at = now()
		 WHERE protocol_type = $1
	`, connectorKey, httpAddress); err != nil {
		return fmt.Errorf("update protocol_plugins http_address: %w", err)
	}
	return tx.Commit()
}

func (b *postgresLegacyBridge) UpdateConnectorProjection(ctx context.Context, dc DeviceConnector) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	serviceType := legacyServiceType(dc.AdditionalInfo)
	deviceType := legacyDeviceType(dc.DeviceTypeSupport)
	dictCode := legacyDictCode(dc.DeviceTypeSupport)

	if _, err := tx.ExecContext(ctx, `
		UPDATE service_plugins
		   SET name = $2,
		       service_type = $3,
		       description = $4,
		       service_config = jsonb_set(
		           coalesce(service_config::jsonb, '{}'::jsonb),
		           '{sub_topic_prefix}',
		           to_jsonb($5::text),
		           true
		       )::json,
		       update_at = now()
		 WHERE service_identifier = $1
	`, dc.ConnectorKey, dc.DisplayName, serviceType, nullString(dc.Description), dc.MQTTTopicPrefix); err != nil {
		return fmt.Errorf("update service_plugins connector projection: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE protocol_plugins
		   SET name = $2,
		       device_type = $3,
		       sub_topic_prefix = $4,
		       description = $5,
		       additional_info = $6,
		       update_at = now()
		 WHERE protocol_type = $1
	`, dc.ConnectorKey, dc.DisplayName, deviceType, dc.MQTTTopicPrefix,
		nullString(dc.Description), connectorAdditionalInfo(dc)); err != nil {
		return fmt.Errorf("update protocol_plugins connector projection: %w", err)
	}

	dictID := newID()
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO sys_dict (id, dict_code, dict_value, created_at, remark)
		VALUES ($1,$2,$3,now(),'device-connector bridge')
		ON CONFLICT (dict_code, dict_value) DO UPDATE
		   SET remark = EXCLUDED.remark
		RETURNING id
	`, dictID, dictCode, dc.ConnectorKey).Scan(&dictID); err != nil {
		return fmt.Errorf("update sys_dict connector projection: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sys_dict_language (id, dict_id, language_code, translation)
		VALUES ($1,$2,'zh-CN',$3)
		ON CONFLICT (dict_id, language_code) DO UPDATE
		   SET translation = EXCLUDED.translation
	`, newID(), dictID, dc.DisplayName); err != nil {
		return fmt.Errorf("update sys_dict_language connector projection: %w", err)
	}

	return tx.Commit()
}

func (b *postgresLegacyBridge) DeleteConnector(ctx context.Context, connectorKey string) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM protocol_plugins WHERE protocol_type = $1`, connectorKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sys_dict WHERE dict_value = $1 AND dict_code IN ('DRIECT_ATTACHED_PROTOCOL','GATEWAY_PROTOCOL')`, connectorKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM service_plugins WHERE service_identifier = $1`, connectorKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (b *postgresLegacyBridge) ProjectBinding(ctx context.Context, binding ConnectorBinding, dc DeviceConnector) (string, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	servicePluginID := dc.LegacyServicePluginID
	if servicePluginID == "" {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM service_plugins WHERE service_identifier = $1`, dc.ConnectorKey).Scan(&servicePluginID); err != nil {
			return "", fmt.Errorf("lookup service plugin for %q: %w", dc.ConnectorKey, err)
		}
	}

	var tenantID string
	if err := tx.QueryRowContext(ctx, `SELECT tenant_id FROM devices WHERE id = $1`, binding.DeviceID).Scan(&tenantID); err != nil {
		return "", fmt.Errorf("lookup tenant for device %q: %w", binding.DeviceID, err)
	}

	serviceAccessID := newID()
	voucher := strings.TrimSpace(binding.Voucher)
	if voucher == "" {
		voucher = binding.ID
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO service_access (
			id, name, service_plugin_id, voucher, description, service_access_config,
			remark, create_at, update_at, tenant_id
		) VALUES ($1,$2,$3,$4,'device-connector binding','{}'::json,'device-connector bridge',$5,$5,$6)
	`, serviceAccessID, "dc-"+binding.DeviceID, servicePluginID, voucher, now, tenantID); err != nil {
		return "", fmt.Errorf("insert service_access: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET service_access_id = $2 WHERE id = $1`, binding.DeviceID, serviceAccessID); err != nil {
		return "", fmt.Errorf("update device service_access_id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return serviceAccessID, nil
}

func (b *postgresLegacyBridge) DeleteBinding(ctx context.Context, bindingID string) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var serviceAccessID, deviceID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT legacy_service_access_id::text, device_id::text
		  FROM connector_bindings
		 WHERE id = $1
	`, bindingID).Scan(&serviceAccessID, &deviceID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if serviceAccessID.Valid && serviceAccessID.String != "" {
		if deviceID.Valid && deviceID.String != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE devices SET service_access_id = NULL WHERE id = $1 AND service_access_id = $2`, deviceID.String, serviceAccessID.String); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM service_access WHERE id = $1`, serviceAccessID.String); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func legacyDeviceType(deviceTypeSupport string) int {
	switch strings.ToLower(strings.TrimSpace(deviceTypeSupport)) {
	case "gateway", "both":
		return 2
	default:
		return 1
	}
}

func legacyDictCode(deviceTypeSupport string) string {
	if legacyDeviceType(deviceTypeSupport) == 2 {
		return "GATEWAY_PROTOCOL"
	}
	return "DRIECT_ATTACHED_PROTOCOL"
}

func legacyServiceType(additionalInfo json.RawMessage) int {
	if len(additionalInfo) == 0 {
		return 1
	}
	var info map[string]any
	if err := json.Unmarshal(additionalInfo, &info); err != nil {
		return 1
	}
	if info == nil {
		return 1
	}
	switch v := info["legacy_service_type"].(type) {
	case float64:
		if int(v) == 2 {
			return 2
		}
	case int:
		if v == 2 {
			return 2
		}
	case string:
		if strings.TrimSpace(v) == "2" {
			return 2
		}
	}
	if strings.EqualFold(fmt.Sprint(info["integration_kind"]), "service_access") {
		return 2
	}
	return 1
}

func connectorAdditionalInfo(dc DeviceConnector) string {
	info := map[string]any{}
	if len(dc.AdditionalInfo) > 0 {
		if err := json.Unmarshal(dc.AdditionalInfo, &info); err != nil {
			info = map[string]any{}
		}
	}
	info["connector_key"] = dc.ConnectorKey
	raw, err := json.Marshal(info)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
