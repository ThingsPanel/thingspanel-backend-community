package brokercontrol

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"project/internal/brokercontract"
)

// PluginContext is the immutable authorization snapshot associated with one
// plugin-socket grant. It is intentionally keyed by a scope-derived ID rather
// than by a reusable wire ClientID or connection ID.
type PluginContext struct {
	ID             string
	Principal      brokercontract.Principal
	BrokerCell     string
	BrokerInstance string
	ConnectionID   string
	Targets        []brokercontract.PluginTarget
	IssuedAt       time.Time
	ExpiresAt      time.Time
}

type pluginContextRow struct {
	ID             string    `gorm:"column:id;primaryKey;size:64"`
	PrincipalJSON  []byte    `gorm:"column:principal_json;not null;type:jsonb"`
	BrokerCell     string    `gorm:"column:broker_cell;not null;size:128"`
	BrokerInstance string    `gorm:"column:broker_instance;not null;size:128"`
	ConnectionID   string    `gorm:"column:connection_id;not null;size:256"`
	TargetsJSON    []byte    `gorm:"column:targets_json;not null;type:jsonb"`
	IssuedAt       time.Time `gorm:"column:issued_at;not null"`
	ExpiresAt      time.Time `gorm:"column:expires_at;not null"`
}

func (pluginContextRow) TableName() string { return "broker_plugin_contexts" }

// ResolvePluginTargets is optional so repositories used for ordinary device
// authentication do not need plugin-specific storage.
type pluginTargetRepository interface {
	ResolvePluginTargets(context.Context, []string) ([]brokercontract.PluginTarget, error)
}

type pluginContextWriter interface {
	StorePluginContext(context.Context, PluginContext) error
}

// ReadPluginContext loads the immutable authorization context referenced by a
// grant's PluginContextID. Expiry is checked by the consumer against packet
// observed_at because persisted broker packets may be processed later.
func ReadPluginContext(ctx context.Context, db *gorm.DB, id string) (PluginContext, error) {
	if db == nil || len(id) != 64 {
		return PluginContext{}, errors.New("brokercontrol: invalid plugin context reference")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return PluginContext{}, errors.New("brokercontrol: invalid plugin context reference")
	}
	var row pluginContextRow
	err := db.Session(&gorm.Session{Logger: logger.Discard}).WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if err != nil {
		return PluginContext{}, err
	}
	var principal brokercontract.Principal
	var targets []brokercontract.PluginTarget
	if json.Unmarshal(row.PrincipalJSON, &principal) != nil || brokercontract.ValidatePrincipal(principal) != nil ||
		json.Unmarshal(row.TargetsJSON, &targets) != nil || !validPluginTargets(targets) {
		return PluginContext{}, errors.New("brokercontrol: corrupt plugin context")
	}
	expectedID, err := pluginContextID(principal, row.BrokerCell, row.BrokerInstance, row.ConnectionID, targets)
	if err != nil || expectedID != row.ID || !row.ExpiresAt.After(row.IssuedAt) {
		return PluginContext{}, errors.New("brokercontrol: corrupt plugin context")
	}
	return PluginContext{ID: row.ID, Principal: principal, BrokerCell: row.BrokerCell, BrokerInstance: row.BrokerInstance,
		ConnectionID: row.ConnectionID, Targets: targets, IssuedAt: row.IssuedAt, ExpiresAt: row.ExpiresAt}, nil
}

func (r *GormRepository) ResolvePluginTargets(ctx context.Context, ids []string) ([]brokercontract.PluginTarget, error) {
	if len(ids) == 0 || len(ids) > MaxServiceTargets || !validUniqueScopeIDs(ids) {
		return nil, errors.New("brokercontrol: invalid plugin target IDs")
	}
	// Resolve service-access devices and protocol-plugin devices in one bounded
	// query. The two relations are mutually exclusive here so that a device
	// carrying both resolves to exactly one target, with the service-access
	// relation winning as it does in brokerplugin.Resolve. Duplicate
	// relationship rows inside one branch are retained and rejected below.
	type targetRow struct {
		brokercontract.PluginTarget
		DeviceNumberCount int64 `gorm:"column:device_number_count"`
	}
	var rows []targetRow
	err := r.quietDB(ctx).Raw(`
		SELECT d.tenant_id, d.id AS device_id, d.device_number,
		       (SELECT count(*) FROM devices dx WHERE dx.device_number = d.device_number) AS device_number_count,
		       COALESCE(sp.service_config::jsonb->>'sub_topic_prefix', '') AS topic_prefix,
		       'device' AS kind,
		       'standard-v1' AS mapping_version
		FROM devices d
		JOIN service_access sa ON sa.id = d.service_access_id AND sa.tenant_id = d.tenant_id
		JOIN service_plugins sp ON sp.id = sa.service_plugin_id AND sp.service_type = 2
		WHERE d.id IN ? AND (d.access_way = 'B' OR NULLIF(btrim(d.service_access_id), '') IS NOT NULL) AND d.is_enabled = 'enabled' AND d.activate_flag = 'active'
		  AND (d.device_config_id IS NULL OR NOT EXISTS (SELECT 1 FROM device_topic_mappings m WHERE m.device_config_id=d.device_config_id AND m.enabled=TRUE))
		UNION ALL
		SELECT d.tenant_id, d.id AS device_id, d.device_number,
		       (SELECT count(*) FROM devices dx WHERE dx.device_number = d.device_number) AS device_number_count,
		       COALESCE(sp.service_config::jsonb->>'sub_topic_prefix', '') AS topic_prefix,
		       CASE WHEN dc.device_type IN ('2','3') THEN 'gateway' ELSE 'device' END AS kind,
		       'standard-v1' AS mapping_version
		FROM devices d
		JOIN device_configs dc ON dc.id = d.device_config_id AND dc.tenant_id = d.tenant_id
		JOIN service_plugins sp ON sp.service_identifier = dc.protocol_type AND sp.service_type = 1
		WHERE d.id IN ? AND d.access_way IS DISTINCT FROM 'B' AND NULLIF(btrim(d.service_access_id), '') IS NULL
		  AND d.is_enabled = 'enabled' AND d.activate_flag = 'active'
		  AND COALESCE(dc.protocol_type, '') <> '' AND upper(dc.protocol_type) <> 'MQTT'
		  AND NOT EXISTS (SELECT 1 FROM device_topic_mappings m WHERE m.device_config_id=dc.id AND m.enabled=TRUE)
		ORDER BY device_id, kind`, ids, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]brokercontract.PluginTarget, 0, len(rows))
	for _, row := range rows {
		if row.DeviceNumberCount != 1 {
			return nil, errors.New("brokercontrol: plugin device number is ambiguous")
		}
		target := row.PluginTarget
		target.TopicPrefix = strings.TrimRight(strings.TrimSpace(target.TopicPrefix), "/") + "/"
		if !brokercontract.ValidPluginPrefix(target.TopicPrefix) {
			return nil, errors.New("brokercontrol: invalid plugin topic prefix")
		}
		out = append(out, target)
	}
	return out, nil
}

func (r *GormRepository) StorePluginContext(ctx context.Context, value PluginContext) error {
	if r == nil || r.DB == nil || value.ID == "" || brokercontract.ValidatePrincipal(value.Principal) != nil || !validPluginTargets(value.Targets) || value.ConnectionID == "" || value.BrokerCell == "" || value.BrokerInstance == "" || value.IssuedAt.IsZero() || !value.ExpiresAt.After(value.IssuedAt) {
		return errors.New("brokercontrol: invalid plugin context")
	}
	expectedID, err := pluginContextID(value.Principal, value.BrokerCell, value.BrokerInstance, value.ConnectionID, value.Targets)
	if err != nil || expectedID != value.ID {
		return errors.New("brokercontrol: invalid plugin context")
	}
	principalJSON, err := json.Marshal(value.Principal)
	if err != nil {
		return err
	}
	targetsJSON, err := json.Marshal(value.Targets)
	if err != nil {
		return err
	}
	row := pluginContextRow{ID: value.ID, PrincipalJSON: principalJSON, BrokerCell: value.BrokerCell, BrokerInstance: value.BrokerInstance,
		ConnectionID: value.ConnectionID, TargetsJSON: targetsJSON, IssuedAt: value.IssuedAt, ExpiresAt: value.ExpiresAt}
	// Same hash means same principal, route, connection, and sorted target scope.
	// Only expiry may extend; identity/scope fields are never overwritten.
	return r.quietDB(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(map[string]any{
		"expires_at": gorm.Expr("GREATEST(broker_plugin_contexts.expires_at, EXCLUDED.expires_at)"),
	})}).Create(&row).Error
}

func validPluginTargets(targets []brokercontract.PluginTarget) bool {
	if len(targets) == 0 || len(targets) > MaxServiceTargets {
		return false
	}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target.TenantID == "" || target.DeviceID == "" || target.DeviceNumber == "" || target.MappingVersion != MappingVersion ||
			target.RouteEpoch == 0 || (target.BindingPhase != "active" && target.BindingPhase != "connecting") || !brokercontract.ValidPluginPrefix(target.TopicPrefix) ||
			(target.Kind != brokercontract.PrincipalDevice && target.Kind != brokercontract.PrincipalGateway) {
			return false
		}
		key := target.TenantID + "\x00" + target.DeviceID
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func validPluginPrefix(prefix string) bool { return brokercontract.ValidPluginPrefix(prefix) }
