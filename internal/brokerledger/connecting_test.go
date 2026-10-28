package brokerledger

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

func TestPostgresConnectingBindingAcceptsPendingButDoesNotProcess(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant, device := "tenant-connecting", "device-connecting"
	insertBinding(t, db, tenant, device, "cell-a", 3, "connecting")
	message := testMessage(tenant, device, "event-connecting")
	message.RouteEpoch = 3
	id, duplicate, err := store.Accept(context.Background(), message)
	if err != nil || duplicate || id == "" {
		t.Fatalf("connecting ingress accept id=%q duplicate=%v err=%v", id, duplicate, err)
	}
	called := false
	processed, err := store.ProcessOne(context.Background(), func(context.Context, *gorm.DB, Message) error {
		called = true
		return nil
	})
	if err != nil || processed || called {
		t.Fatalf("connecting receipt processed=%v called=%v err=%v", processed, called, err)
	}
	receipt, err := store.GetReceipt(context.Background(), tenant, device, id)
	if err != nil || receipt.State != statePending {
		t.Fatalf("connecting receipt state=%q err=%v, want pending", receipt.State, err)
	}
	if err := db.Exec("UPDATE broker_transport_bindings SET phase='active' WHERE tenant_id=? AND device_id=?", tenant, device).Error; err != nil {
		t.Fatal(err)
	}
	processed, err = store.ProcessOne(context.Background(), func(ctx context.Context, tx *gorm.DB, got Message) error {
		called = true
		if got.ID != id {
			t.Fatalf("processed receipt=%s, want %s", got.ID, id)
		}
		return nil
	})
	if err != nil || !processed || !called {
		t.Fatalf("activated receipt processed=%v called=%v err=%v", processed, called, err)
	}
	receipt, err = store.GetReceipt(context.Background(), tenant, device, id)
	if err != nil || receipt.State != stateProcessed {
		t.Fatalf("activated receipt state=%q err=%v, want processed", receipt.State, err)
	}
}
