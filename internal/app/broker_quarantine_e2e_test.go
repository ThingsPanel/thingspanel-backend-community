package app

import (
	"context"
	"io"
	"testing"

	"project/internal/brokerledger"
	"project/internal/model"

	"github.com/sirupsen/logrus"
)

// This is a real PostgreSQL check of the app acceptor boundary: rejection is
// durable and deduplicated, while it never creates an accepted business receipt.
func TestBrokerIngressAcceptorRejectPersistsWithoutBusinessAcceptance(t *testing.T) {
	db, _, closeDB := nativeE2EPostgres(t)
	defer closeDB()
	root := repoRootForNativeE2E(t)
	if err := migrateNativeE2ESchema(t, db, root); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Device{ID: "quarantine-device", TenantID: "quarantine-tenant", DeviceNumber: "quarantine-number", IsEnabled: "enabled", ActivateFlag: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES(?,?,?,?,?)`, "quarantine-tenant", "quarantine-device", "cell-a", 7, "active").Error; err != nil {
		t.Fatal(err)
	}
	store := brokerledger.New(db)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	acceptor := &brokerIngressAcceptor{store: store, logger: logger}
	input := brokerledger.Rejection{TenantID: "quarantine-tenant", DeviceID: "quarantine-device", BrokerCell: "cell-a", RouteEpoch: 7, Topic: "devices/command/response/foreign-id", Body: []byte{0, 0xff, 1}, Reason: "command_ack_scope_denied"}
	if err := acceptor.Reject(context.Background(), input); err != nil {
		t.Fatalf("persist rejection: %v", err)
	}
	if err := acceptor.Reject(context.Background(), input); err != nil {
		t.Fatalf("persist duplicate rejection: %v", err)
	}
	var quarantined, receipts, effects int64
	if err := db.Table("broker_ingress_quarantine").Where("tenant_id = ? AND device_id = ?", input.TenantID, input.DeviceID).Count(&quarantined).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("broker_ingress_receipts").Count(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("broker_ingress_effects").Count(&effects).Error; err != nil {
		t.Fatal(err)
	}
	if quarantined != 1 || receipts != 0 || effects != 0 {
		t.Fatalf("quarantine=%d receipts=%d effects=%d; rejected ingress must not become platform acceptance", quarantined, receipts, effects)
	}
	var occurrences int64
	if err := db.Table("broker_ingress_quarantine").Select("occurrences").Where("tenant_id = ? AND device_id = ?", input.TenantID, input.DeviceID).Scan(&occurrences).Error; err != nil {
		t.Fatal(err)
	}
	if occurrences != 2 {
		t.Fatalf("duplicate rejection occurrences=%d, want 2", occurrences)
	}
}
