package pluginmgr

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPostgresLegacyBridgeSmoke(t *testing.T) {
	if os.Getenv("CONNECTOR_E2E_USE_POSTGRES") != "1" {
		t.Skip("set CONNECTOR_E2E_USE_POSTGRES=1 to run against local ThingsPanel Postgres")
	}

	ctx := context.Background()
	db, err := openDeviceConnectorDB()
	if err != nil {
		t.Fatalf("open connector db: %v", err)
	}
	defer db.Close()

	key := "codex-smoke-xiaomi-plug"
	cleanupConnectorSmoke(t, db, key)
	t.Cleanup(func() { cleanupConnectorSmoke(t, db, key) })

	repo := newPostgresConnectorRepository(db)
	bridge := newPostgresLegacyBridge(db)
	runner := NewFakeRunner()
	runner.FakeHTTPAddress = "127.0.0.1:9001"
	svc := NewDeviceConnectorService(repo, bridge, runner)

	httpCallbacks := true
	dc, err := svc.create(ctx, &CreateDeviceConnectorRequest{
		ConnectorKey:         key,
		DisplayName:          "Codex Smoke Xiaomi Plug",
		MQTTTopicPrefix:      "plugin/codex-smoke/",
		DeviceTypeSupport:    "direct",
		HTTPCallbacksEnabled: &httpCallbacks,
		Description:          "created by DeviceConnector postgres smoke test",
	})
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	if dc.LegacyServicePluginID == "" || dc.LegacyProtocolPluginID == "" {
		t.Fatalf("expected legacy IDs after projection, got service=%q protocol=%q", dc.LegacyServicePluginID, dc.LegacyProtocolPluginID)
	}

	rt := &ConnectorRuntime{
		ID:                newID(),
		DeviceConnectorID: dc.ID,
		Image:             "registry.example.com/xiaomi-plug:1.0.0",
		HTTPPort:          9001,
		HealthCheckPath:   "/health",
		EnvVars:           map[string]string{"LOG_LEVEL": "debug"},
	}
	if err := repo.CreateConnectorRuntime(ctx, rt); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	inst, err := svc.createInstance(ctx, &CreateConnectorInstanceRequest{ConnectorRuntimeID: rt.ID})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if inst.HTTPAddress == "" {
		t.Fatalf("expected HTTPAddress from fake runner")
	}

	assertLegacyProjection(t, db, key, inst.HTTPAddress, "plugin/codex-smoke/", "Codex Smoke Xiaomi Plug")

	if _, err := svc.updateConnector(ctx, dc.ID, &UpdateDeviceConnectorRequest{
		DisplayName:     "Codex Smoke Xiaomi Plug Updated",
		MQTTTopicPrefix: "plugin/codex-smoke-updated/",
		Description:     "updated by DeviceConnector postgres smoke test",
	}); err != nil {
		t.Fatalf("update connector: %v", err)
	}
	assertLegacyProjection(t, db, key, inst.HTTPAddress, "plugin/codex-smoke-updated/", "Codex Smoke Xiaomi Plug Updated")
}

func TestLegacyServiceTypeFromAdditionalInfo(t *testing.T) {
	tests := []struct {
		name string
		info map[string]interface{}
		want int
	}{
		{name: "default protocol", info: nil, want: 1},
		{name: "numeric service type", info: map[string]interface{}{"legacy_service_type": 2}, want: 2},
		{name: "json numeric service type", info: map[string]interface{}{"legacy_service_type": float64(2)}, want: 2},
		{name: "string service type", info: map[string]interface{}{"legacy_service_type": "2"}, want: 2},
		{name: "service access integration", info: map[string]interface{}{"integration_kind": "service_access"}, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			if tt.info != nil {
				data, err := json.Marshal(tt.info)
				if err != nil {
					t.Fatalf("marshal additional info: %v", err)
				}
				raw = data
			}
			if got := legacyServiceType(raw); got != tt.want {
				t.Fatalf("legacyServiceType() = %d, want %d", got, tt.want)
			}
		})
	}
}

func assertLegacyProjection(t *testing.T, db *sql.DB, key, httpAddress, wantPrefix, wantTranslation string) {
	t.Helper()

	var serviceConfig string
	if err := db.QueryRow(`
		SELECT service_config::text
		  FROM service_plugins
		 WHERE service_identifier = $1
	`, key).Scan(&serviceConfig); err != nil {
		t.Fatalf("service_plugins projection missing: %v", err)
	}
	if serviceConfig == "" {
		t.Fatalf("service_plugins.service_config empty")
	}
	if !strings.Contains(serviceConfig, wantPrefix) {
		t.Fatalf("service_plugins.service_config = %q, want prefix %q", serviceConfig, wantPrefix)
	}
	if !strings.Contains(serviceConfig, httpAddress) {
		t.Fatalf("service_plugins.service_config = %q, want httpAddress %q", serviceConfig, httpAddress)
	}

	var protocolHTTP, prefix string
	if err := db.QueryRow(`
		SELECT http_address, sub_topic_prefix
		  FROM protocol_plugins
		 WHERE protocol_type = $1
	`, key).Scan(&protocolHTTP, &prefix); err != nil {
		t.Fatalf("protocol_plugins projection missing: %v", err)
	}
	if protocolHTTP != httpAddress {
		t.Fatalf("protocol_plugins.http_address = %q, want %q", protocolHTTP, httpAddress)
	}
	if prefix != wantPrefix {
		t.Fatalf("protocol_plugins.sub_topic_prefix = %q, want %q", prefix, wantPrefix)
	}

	var translation string
	if err := db.QueryRow(`
		SELECT l.translation
		  FROM sys_dict d
		  JOIN sys_dict_language l ON l.dict_id = d.id
		 WHERE d.dict_code = 'DRIECT_ATTACHED_PROTOCOL'
		   AND d.dict_value = $1
		   AND l.language_code = 'zh-CN'
	`, key).Scan(&translation); err != nil {
		t.Fatalf("sys_dict/sys_dict_language projection missing: %v", err)
	}
	if translation != wantTranslation {
		t.Fatalf("translation = %q, want %q", translation, wantTranslation)
	}
}

func cleanupConnectorSmoke(t *testing.T, db *sql.DB, key string) {
	t.Helper()
	_, _ = db.Exec(`
		DELETE FROM connector_events
		 WHERE connector_instance_id IN (
			SELECT ci.id
			  FROM connector_instances ci
			  JOIN device_connectors dc ON dc.id = ci.device_connector_id
			 WHERE dc.connector_key = $1
		 )
		    OR connector_instance_id IS NULL
	`, key)
	_, _ = db.Exec(`
		DELETE FROM connector_statuses
		 WHERE connector_instance_id IN (
			SELECT ci.id
			  FROM connector_instances ci
			  JOIN device_connectors dc ON dc.id = ci.device_connector_id
			 WHERE dc.connector_key = $1
		 )
	`, key)
	_, _ = db.Exec(`
		DELETE FROM connector_bindings
		 WHERE device_connector_id IN (SELECT id FROM device_connectors WHERE connector_key = $1)
	`, key)
	_, _ = db.Exec(`
		DELETE FROM connector_instances
		 WHERE device_connector_id IN (SELECT id FROM device_connectors WHERE connector_key = $1)
	`, key)
	_, _ = db.Exec(`
		DELETE FROM connector_runtimes
		 WHERE device_connector_id IN (SELECT id FROM device_connectors WHERE connector_key = $1)
	`, key)
	_, _ = db.Exec(`DELETE FROM device_connectors WHERE connector_key = $1`, key)
	_, _ = db.Exec(`DELETE FROM protocol_plugins WHERE protocol_type = $1`, key)
	_, _ = db.Exec(`DELETE FROM sys_dict WHERE dict_value = $1 AND dict_code IN ('DRIECT_ATTACHED_PROTOCOL','GATEWAY_PROTOCOL')`, key)
	_, _ = db.Exec(`DELETE FROM service_plugins WHERE service_identifier = $1`, key)
}
