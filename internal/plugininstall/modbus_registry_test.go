package plugininstall

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"project/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type modbusCatalogAgent struct{ *fakeAgent }

func (a modbusCatalogAgent) Catalog(context.Context) ([]CatalogEntry, error) {
	return []CatalogEntry{{ID: ModbusPluginID, ServiceIdentifier: ModbusServiceIdentifier, Version: "1.0.0"}}, nil
}

func TestModbusFailedHealthLeavesRegistryUntouched(t *testing.T) {
	env := newEnv(t)
	env.e.Agent = modbusCatalogAgent{env.agent}
	env.health.err = errors.New("managed process failed to start")
	row, err := env.e.Submit(context.Background(), PlatformTenantID, ModbusPluginID, "modbus-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	row = env.settle(t, PlatformTenantID, row.ID, StateFailed)
	if row.OpStep != "health" || env.svc.n != 0 || env.ids.live || env.agent.count("revoke-1") != 1 {
		t.Fatalf("failed Modbus installation did not roll back: step=%s registrations=%d live=%v",
			row.OpStep, env.svc.n, env.ids.live)
	}
}

func TestModbusRegistersBothProtocolsAndPreservesExistingRecord(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&model.ServicePlugin{}); err != nil {
		t.Fatal(err)
	}
	config := `{"access_address":"existing:502"}`
	existing := model.ServicePlugin{ID: "existing", Name: "TCP", ServiceIdentifier: ModbusTCP,
		ServiceType: 1, ServiceConfig: &config}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	entry := CatalogEntry{ID: ModbusPluginID, ServiceIdentifier: ModbusServiceIdentifier, Version: "1.0.0",
		NetworkAlias: "tp-plugin-modbus", HTTPPort: 1503, ServicePlugin: &ServicePluginSpec{
			Name: "Modbus", ServiceType: 1, Config: map[string]string{"http_address": "{alias}:{http_port}"},
		}}
	registry := NewGormServices(func() *gorm.DB { return db })
	for i := 0; i < 2; i++ {
		if _, _, err := registry.Ensure(context.Background(), entry); err != nil {
			t.Fatal(err)
		}
	}
	var rows []model.ServicePlugin
	if err := db.Order("service_identifier DESC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != existing.ID || rows[1].ServiceIdentifier != ModbusRTU {
		t.Fatalf("unexpected registrations: %+v", rows)
	}
	var updated map[string]any
	if err := json.Unmarshal([]byte(*rows[0].ServiceConfig), &updated); err != nil {
		t.Fatal(err)
	}
	if updated["access_address"] != "existing:502" || updated["http_address"] != "tp-plugin-modbus:1503" ||
		updated["device_type"] != float64(2) || updated["managed_plugin_id"] != ModbusPluginID {
		t.Fatalf("unexpected TCP config: %+v", updated)
	}
}
