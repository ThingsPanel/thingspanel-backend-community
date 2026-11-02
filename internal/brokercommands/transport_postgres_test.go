package brokercommands

import (
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"testing"
)

func TestPreparedPluginRouteFreezesBothEpochsAndRejectsReparent(t *testing.T) {
	_, db := postgresStore(t)
	if err := db.Exec(`ALTER TABLE devices ADD COLUMN activate_flag text NOT NULL DEFAULT 'active'`).Error; err != nil {
		t.Fatal(err)
	}
	seedDevices(t, db, "t", "root", "child")
	if err := db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES('t','child','cell-a',3,'active')`).Error; err != nil {
		t.Fatal(err)
	}
	scope, err := CapturePluginRoute(db, "t", "child")
	if err != nil {
		t.Fatal(err)
	}
	if scope.TargetID != "child" || scope.TargetEpoch != 3 || scope.TransportID != "root" || scope.TransportEpoch != 7 {
		t.Fatalf("incorrect frozen scope: %+v", scope)
	}
	payload, err := json.Marshal(PreparedCommand{TransportRoute: scope})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return lockPreparedRoute(tx, "t", "child", payload) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE broker_transport_bindings SET route_epoch=8 WHERE device_id='root'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return lockPreparedRoute(tx, "t", "child", payload) }); !errors.Is(err, ErrFenced) {
		t.Fatalf("physical migration failed to fence frozen command: %v", err)
	}
	if err := db.Exec(`UPDATE broker_transport_bindings SET route_epoch=7 WHERE device_id='root';UPDATE devices SET parent_id=NULL WHERE id='child'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return lockPreparedRoute(tx, "t", "child", payload) }); !errors.Is(err, ErrFenced) {
		t.Fatalf("reparent failed to fence frozen command: %v", err)
	}
}

func TestPreparedPluginRouteMarkerCannotFallBackToNative(t *testing.T) {
	_, db := postgresStore(t)
	for _, payload := range []string{`{"transport_route":null}`, `{"transport_route":{"tenant_id":"t"}}`, `{"transport_route":"forged"}`} {
		if err := lockPreparedRoute(db, "t", "child", []byte(payload)); !errors.Is(err, ErrFenced) {
			t.Fatalf("invalid marker fell back to legacy native route: %s %v", payload, err)
		}
	}
}
