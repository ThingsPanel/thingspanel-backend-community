package pluginmgr

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const defaultConnectorDBDSN = "postgres://postgres:postgresThingsPanel@127.0.0.1:5555/ThingsPanel?sslmode=disable"

func openDeviceConnectorDB() (*sql.DB, error) {
	dsn := strings.TrimSpace(os.Getenv("TP_CONNECTOR_DB_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("TP_DB_DSN"))
	}
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dsn == "" {
		dsn = defaultConnectorDBDSN
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

type postgresConnectorRepository struct {
	db *sql.DB
}

func newPostgresConnectorRepository(db *sql.DB) ConnectorRepository {
	return &postgresConnectorRepository{db: db}
}

func (r *postgresConnectorRepository) CreateDeviceConnector(ctx context.Context, dc *DeviceConnector) error {
	additional, err := marshalNullableJSON(dc.AdditionalInfo)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO device_connectors (
			id, connector_key, display_name, mqtt_topic_prefix, device_type_support,
			http_callbacks_enabled, description, additional_info, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, dc.ID, dc.ConnectorKey, dc.DisplayName, dc.MQTTTopicPrefix, dc.DeviceTypeSupport,
		dc.HTTPCallbacksEnabled, nullString(dc.Description), additional, dc.CreatedAt, dc.UpdatedAt)
	return err
}

func (r *postgresConnectorRepository) GetDeviceConnector(ctx context.Context, id string) (*DeviceConnector, error) {
	return r.scanDeviceConnector(r.db.QueryRowContext(ctx, `
		SELECT id, connector_key, display_name, mqtt_topic_prefix, device_type_support,
		       http_callbacks_enabled, description, additional_info,
		       legacy_service_plugin_id::text, legacy_protocol_plugin_id::text,
		       created_at, updated_at
		  FROM device_connectors
		 WHERE id = $1
	`, id))
}

func (r *postgresConnectorRepository) GetDeviceConnectorByKey(ctx context.Context, connectorKey string) (*DeviceConnector, error) {
	return r.scanDeviceConnector(r.db.QueryRowContext(ctx, `
		SELECT id, connector_key, display_name, mqtt_topic_prefix, device_type_support,
		       http_callbacks_enabled, description, additional_info,
		       legacy_service_plugin_id::text, legacy_protocol_plugin_id::text,
		       created_at, updated_at
		  FROM device_connectors
		 WHERE connector_key = $1
	`, connectorKey))
}

func (r *postgresConnectorRepository) UpdateDeviceConnector(ctx context.Context, dc *DeviceConnector) error {
	additional, err := marshalNullableJSON(dc.AdditionalInfo)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE device_connectors
		   SET display_name = $2,
		       mqtt_topic_prefix = $3,
		       description = $4,
		       additional_info = $5,
		       updated_at = $6
		 WHERE id = $1
	`, dc.ID, dc.DisplayName, dc.MQTTTopicPrefix, nullString(dc.Description), additional, dc.UpdatedAt)
	return checkRows(res, err, "device connector")
}

func (r *postgresConnectorRepository) DeleteDeviceConnector(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM device_connectors WHERE id = $1`, id)
	return checkRows(res, err, "device connector")
}

func (r *postgresConnectorRepository) ListDeviceConnectors(ctx context.Context, filter ListDeviceConnectorsRequest) ([]DeviceConnector, int, error) {
	page, size := normalizePage(filter.Page, filter.PageSize)
	offset := (page - 1) * size

	var total int
	var rows *sql.Rows
	var err error
	if strings.TrimSpace(filter.DeviceTypeSupport) != "" {
		err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM device_connectors WHERE device_type_support = $1`, filter.DeviceTypeSupport).Scan(&total)
		if err != nil {
			return nil, 0, err
		}
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, connector_key, display_name, mqtt_topic_prefix, device_type_support,
			       http_callbacks_enabled, description, additional_info,
			       legacy_service_plugin_id::text, legacy_protocol_plugin_id::text,
			       created_at, updated_at
			  FROM device_connectors
			 WHERE device_type_support = $1
			 ORDER BY created_at DESC
			 LIMIT $2 OFFSET $3
		`, filter.DeviceTypeSupport, size, offset)
	} else {
		err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM device_connectors`).Scan(&total)
		if err != nil {
			return nil, 0, err
		}
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, connector_key, display_name, mqtt_topic_prefix, device_type_support,
			       http_callbacks_enabled, description, additional_info,
			       legacy_service_plugin_id::text, legacy_protocol_plugin_id::text,
			       created_at, updated_at
			  FROM device_connectors
			 ORDER BY created_at DESC
			 LIMIT $1 OFFSET $2
		`, size, offset)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]DeviceConnector, 0)
	for rows.Next() {
		dc, err := scanDeviceConnectorRows(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, *dc)
	}
	return list, total, rows.Err()
}

func (r *postgresConnectorRepository) SetLegacyIDs(ctx context.Context, id string, servicePluginID string, protocolPluginID string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE device_connectors
		   SET legacy_service_plugin_id = NULLIF($2, ''),
		       legacy_protocol_plugin_id = NULLIF($3, ''),
		       updated_at = now()
		 WHERE id = $1
	`, id, servicePluginID, protocolPluginID)
	return checkRows(res, err, "device connector")
}

func (r *postgresConnectorRepository) CreateConnectorRuntime(ctx context.Context, rt *ConnectorRuntime) error {
	envVars, err := marshalNullableJSON(rt.EnvVars)
	if err != nil {
		return err
	}
	limits, err := marshalNullableJSON(rt.ResourceLimits)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO connector_runtimes (
			id, device_connector_id, image, env_vars, http_port, health_check_path,
			resource_limits, description, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, rt.ID, rt.DeviceConnectorID, rt.Image, envVars, nullInt(rt.HTTPPort),
		nullString(rt.HealthCheckPath), limits, nullString(rt.Description), rt.CreatedAt, rt.UpdatedAt)
	return err
}

func (r *postgresConnectorRepository) GetConnectorRuntime(ctx context.Context, id string) (*ConnectorRuntime, error) {
	var rt ConnectorRuntime
	var envRaw, limitsRaw, health, desc sql.NullString
	var httpPort sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT id, device_connector_id, image, env_vars, http_port, health_check_path,
		       resource_limits, description, created_at, updated_at
		  FROM connector_runtimes
		 WHERE id = $1
	`, id).Scan(&rt.ID, &rt.DeviceConnectorID, &rt.Image, &envRaw, &httpPort, &health,
		&limitsRaw, &desc, &rt.CreatedAt, &rt.UpdatedAt)
	if err != nil {
		return nil, err
	}
	rt.HealthCheckPath = health.String
	rt.Description = desc.String
	if httpPort.Valid {
		rt.HTTPPort = int(httpPort.Int64)
	}
	if err := unmarshalJSON(envRaw, &rt.EnvVars); err != nil {
		return nil, err
	}
	if limitsRaw.Valid && limitsRaw.String != "" {
		var limits ResourceLimits
		if err := json.Unmarshal([]byte(limitsRaw.String), &limits); err != nil {
			return nil, err
		}
		rt.ResourceLimits = &limits
	}
	return &rt, nil
}

func (r *postgresConnectorRepository) CreateConnectorInstance(ctx context.Context, inst *ConnectorInstance) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO connector_instances (
			id, connector_runtime_id, device_connector_id, http_address, namespace, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, inst.ID, inst.ConnectorRuntimeID, inst.DeviceConnectorID, inst.HTTPAddress, inst.Namespace, inst.CreatedAt, inst.UpdatedAt)
	return err
}

func (r *postgresConnectorRepository) GetConnectorInstance(ctx context.Context, id string) (*ConnectorInstance, error) {
	var inst ConnectorInstance
	var httpAddress, ns sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, connector_runtime_id, device_connector_id, http_address, namespace, created_at, updated_at
		  FROM connector_instances
		 WHERE id = $1
	`, id).Scan(&inst.ID, &inst.ConnectorRuntimeID, &inst.DeviceConnectorID, &httpAddress, &ns, &inst.CreatedAt, &inst.UpdatedAt)
	if err != nil {
		return nil, err
	}
	inst.HTTPAddress = httpAddress.String
	inst.Namespace = ns.String
	return &inst, nil
}

func (r *postgresConnectorRepository) UpdateConnectorInstanceHTTPAddress(ctx context.Context, id string, httpAddress string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE connector_instances
		   SET http_address = $2, updated_at = now()
		 WHERE id = $1
	`, id, httpAddress)
	return checkRows(res, err, "connector instance")
}

func (r *postgresConnectorRepository) DeleteConnectorInstance(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM connector_instances WHERE id = $1`, id)
	return checkRows(res, err, "connector instance")
}

func (r *postgresConnectorRepository) UpsertConnectorStatus(ctx context.Context, st *ConnectorStatus) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO connector_statuses (
			connector_instance_id, phase, process_healthy, last_heartbeat_at, message, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (connector_instance_id) DO UPDATE
		   SET phase = EXCLUDED.phase,
		       process_healthy = EXCLUDED.process_healthy,
		       last_heartbeat_at = EXCLUDED.last_heartbeat_at,
		       message = EXCLUDED.message,
		       updated_at = EXCLUDED.updated_at
	`, st.ConnectorInstanceID, st.Phase, st.ProcessHealthy, st.LastHeartbeatAt, nullString(st.Message), st.UpdatedAt)
	return err
}

func (r *postgresConnectorRepository) GetConnectorStatus(ctx context.Context, instanceID string) (*ConnectorStatus, error) {
	var st ConnectorStatus
	var last sql.NullTime
	var msg sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT connector_instance_id, phase, process_healthy, last_heartbeat_at, message, updated_at
		  FROM connector_statuses
		 WHERE connector_instance_id = $1
	`, instanceID).Scan(&st.ConnectorInstanceID, &st.Phase, &st.ProcessHealthy, &last, &msg, &st.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if last.Valid {
		st.LastHeartbeatAt = &last.Time
	}
	st.Message = msg.String
	return &st, nil
}

func (r *postgresConnectorRepository) AppendConnectorEvent(ctx context.Context, e *ConnectorEvent) error {
	instanceID := any(nil)
	if strings.TrimSpace(e.ConnectorInstanceID) != "" {
		instanceID = e.ConnectorInstanceID
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO connector_events (id, connector_instance_id, reason, message, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
	`, e.ID, instanceID, e.Reason, nullString(e.Message), e.OccurredAt)
	return err
}

func (r *postgresConnectorRepository) ListConnectorEvents(ctx context.Context, instanceID string, page, pageSize int) ([]ConnectorEvent, int, error) {
	page, size := normalizePage(page, pageSize)
	offset := (page - 1) * size
	var total int
	if strings.TrimSpace(instanceID) == "" {
		if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM connector_events WHERE connector_instance_id IS NULL`).Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err := r.db.QueryContext(ctx, `
			SELECT id, coalesce(connector_instance_id::text, ''), reason, message, occurred_at
			  FROM connector_events
			 WHERE connector_instance_id IS NULL
			 ORDER BY occurred_at ASC
			 LIMIT $1 OFFSET $2
		`, size, offset)
		if err != nil {
			return nil, 0, err
		}
		return scanConnectorEvents(rows, total)
	}

	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM connector_events WHERE connector_instance_id = $1`, instanceID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, connector_instance_id::text, reason, message, occurred_at
		  FROM connector_events
		 WHERE connector_instance_id = $1
		 ORDER BY occurred_at ASC
		 LIMIT $2 OFFSET $3
	`, instanceID, size, offset)
	if err != nil {
		return nil, 0, err
	}
	return scanConnectorEvents(rows, total)
}

func (r *postgresConnectorRepository) CreateConnectorBinding(ctx context.Context, b *ConnectorBinding) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO connector_bindings (
			id, connector_instance_id, device_connector_id, device_id, voucher, legacy_service_access_id, created_at
		) VALUES ($1,$2,$3,$4,$5,NULLIF($6, ''),$7)
	`, b.ID, b.ConnectorInstanceID, b.DeviceConnectorID, b.DeviceID, nullString(b.Voucher), b.LegacyServiceAccessID, b.CreatedAt)
	return err
}

func (r *postgresConnectorRepository) GetConnectorBinding(ctx context.Context, id string) (*ConnectorBinding, error) {
	var b ConnectorBinding
	var voucher, legacyID sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, connector_instance_id, device_connector_id, device_id::text, voucher, legacy_service_access_id::text, created_at
		  FROM connector_bindings
		 WHERE id = $1
	`, id).Scan(&b.ID, &b.ConnectorInstanceID, &b.DeviceConnectorID, &b.DeviceID, &voucher, &legacyID, &b.CreatedAt)
	if err != nil {
		return nil, err
	}
	b.Voucher = voucher.String
	b.LegacyServiceAccessID = legacyID.String
	return &b, nil
}

func (r *postgresConnectorRepository) DeleteConnectorBinding(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM connector_bindings WHERE id = $1`, id)
	return checkRows(res, err, "connector binding")
}

func (r *postgresConnectorRepository) SetBindingLegacyServiceAccessID(ctx context.Context, bindingID string, serviceAccessID string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE connector_bindings
		   SET legacy_service_access_id = NULLIF($2, '')
		 WHERE id = $1
	`, bindingID, serviceAccessID)
	return checkRows(res, err, "connector binding")
}

func (r *postgresConnectorRepository) scanDeviceConnector(row *sql.Row) (*DeviceConnector, error) {
	return scanDeviceConnector(row.Scan)
}

type scanner func(dest ...any) error

func scanDeviceConnector(scan scanner) (*DeviceConnector, error) {
	var dc DeviceConnector
	var desc, additionalRaw, spID, ppID sql.NullString
	if err := scan(&dc.ID, &dc.ConnectorKey, &dc.DisplayName, &dc.MQTTTopicPrefix, &dc.DeviceTypeSupport,
		&dc.HTTPCallbacksEnabled, &desc, &additionalRaw, &spID, &ppID, &dc.CreatedAt, &dc.UpdatedAt); err != nil {
		return nil, err
	}
	dc.Description = desc.String
	dc.LegacyServicePluginID = spID.String
	dc.LegacyProtocolPluginID = ppID.String
	if err := unmarshalJSON(additionalRaw, &dc.AdditionalInfo); err != nil {
		return nil, err
	}
	return &dc, nil
}

func scanDeviceConnectorRows(rows *sql.Rows) (*DeviceConnector, error) {
	return scanDeviceConnector(rows.Scan)
}

func scanConnectorEvents(rows *sql.Rows, total int) ([]ConnectorEvent, int, error) {
	defer rows.Close()
	list := make([]ConnectorEvent, 0)
	for rows.Next() {
		var ev ConnectorEvent
		var msg sql.NullString
		if err := rows.Scan(&ev.ID, &ev.ConnectorInstanceID, &ev.Reason, &msg, &ev.OccurredAt); err != nil {
			return nil, 0, err
		}
		ev.Message = msg.String
		list = append(list, ev)
	}
	return list, total, rows.Err()
}

func marshalNullableJSON(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return nil, nil
	}
	return string(b), nil
}

func unmarshalJSON(raw sql.NullString, dst any) error {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw.String), dst)
}

func nullString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func normalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	return page, size
}

func checkRows(res sql.Result, err error, name string) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil
	}
	if n == 0 {
		return fmt.Errorf("%s not found", name)
	}
	return nil
}
