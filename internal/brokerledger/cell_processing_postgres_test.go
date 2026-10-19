package brokerledger

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestPostgresProcessOneInCellKeepsOlderOtherCellReceiptUntouched(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant := uuid.NewString()
	deviceA, deviceB := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, deviceA, "cell-a", 1, "active")
	insertBinding(t, db, tenant, deviceB, "cell-b", 1, "active")
	messageB := testMessage(tenant, deviceB, "cell-b-first")
	messageB.BrokerCell = "cell-b"
	idB, _, err := store.Accept(context.Background(), messageB)
	if err != nil {
		t.Fatal(err)
	}
	messageA := testMessage(tenant, deviceA, "cell-a-second")
	idA, _, err := store.Accept(context.Background(), messageA)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE broker_ingress_receipts SET created_at=now()-interval '2 seconds' WHERE id=?", idB).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE broker_ingress_receipts SET created_at=now()-interval '1 second' WHERE id=?", idA).Error; err != nil {
		t.Fatal(err)
	}
	var handled []string
	processed, err := store.ProcessOneInCell(context.Background(), "cell-a", func(_ context.Context, _ *gorm.DB, msg Message) error {
		handled = append(handled, msg.SourceEventID)
		return nil
	})
	if err != nil || !processed || len(handled) != 1 || handled[0] != "cell-a-second" {
		t.Fatalf("cell-a processing = (%v,%v,%v), want only its later receipt", processed, err, handled)
	}
	receiptB, err := store.GetReceipt(context.Background(), tenant, deviceB, idB)
	if err != nil {
		t.Fatal(err)
	}
	if receiptB.State != statePending || receiptB.Attempts != 0 || receiptB.ProcessedAt != nil {
		t.Fatalf("cell-b receipt changed while cell-a ran: %+v", receiptB)
	}
	processed, err = store.ProcessOneInCell(context.Background(), "cell-b", func(_ context.Context, _ *gorm.DB, msg Message) error {
		handled = append(handled, msg.SourceEventID)
		return nil
	})
	if err != nil || !processed || len(handled) != 2 || handled[1] != "cell-b-first" {
		t.Fatalf("cell-b processing = (%v,%v,%v), want its own earlier receipt", processed, err, handled)
	}
	if _, err := store.ProcessOneInCell(context.Background(), "  ", func(context.Context, *gorm.DB, Message) error { t.Fatal("empty cell invoked handler"); return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty cell error = %v, want ErrInvalid", err)
	}
}

func TestPostgresProcessOneInCellFencesRouteChangedReceiptAtOriginalCell(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")
	message := testMessage(tenant, device, "move-cell")
	id, _, err := store.Accept(context.Background(), message)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE broker_transport_bindings SET broker_cell='cell-b',route_epoch=2 WHERE tenant_id=? AND device_id=?", tenant, device).Error; err != nil {
		t.Fatal(err)
	}
	called := false
	processed, err := store.ProcessOneInCell(context.Background(), "cell-b", func(context.Context, *gorm.DB, Message) error { called = true; return nil })
	if err != nil || processed || called {
		t.Fatalf("new cell processed old receipt: processed=%v called=%v err=%v", processed, called, err)
	}
	processed, err = store.ProcessOneInCell(context.Background(), "cell-a", func(context.Context, *gorm.DB, Message) error { called = true; return nil })
	if err != nil || !processed || called {
		t.Fatalf("old cell did not isolate/fence receipt: processed=%v called=%v err=%v", processed, called, err)
	}
	receipt, err := store.GetReceipt(context.Background(), tenant, device, id)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != stateFenced || receipt.BrokerCell != "cell-a" || receipt.RouteEpoch != 1 {
		t.Fatalf("old receipt was relabeled or not fenced: %+v", receipt)
	}
}

func TestPostgresProcessOneInCellRetryAndConcurrentCellsAreIsolated(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	deviceA, deviceB := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenantA, deviceA, "cell-a", 1, "active")
	insertBinding(t, db, tenantB, deviceB, "cell-b", 1, "active")
	msgA := testMessage(tenantA, deviceA, "retry-a")
	msgB := testMessage(tenantB, deviceB, "concurrent-b")
	msgB.BrokerCell = "cell-b"
	idA, _, err := store.Accept(context.Background(), msgA)
	if err != nil {
		t.Fatal(err)
	}
	idB, _, err := store.Accept(context.Background(), msgB)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("transient cell-a handler failure")
	processed, err := store.ProcessOneInCell(context.Background(), "cell-a", func(context.Context, *gorm.DB, Message) error { return failure })
	if !processed || !errors.Is(err, failure) {
		t.Fatalf("cell-a retry failure result processed=%v err=%v", processed, err)
	}
	receiptA, err := store.GetReceipt(context.Background(), tenantA, deviceA, idA)
	if err != nil {
		t.Fatal(err)
	}
	receiptB, err := store.GetReceipt(context.Background(), tenantB, deviceB, idB)
	if err != nil {
		t.Fatal(err)
	}
	if receiptA.State != statePending || receiptA.Attempts != 1 || receiptB.State != statePending || receiptB.Attempts != 0 {
		t.Fatalf("retry crossed cell boundary: A=%+v B=%+v", receiptA, receiptB)
	}
	if err := db.Exec("UPDATE broker_ingress_receipts SET retry_after=now()-interval '1 second' WHERE id=?", idA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE cell_processing_effects(receipt_id varchar(36) PRIMARY KEY, broker_cell varchar(128) NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var callsA, callsB atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, item := range []struct {
		cell  string
		want  string
		calls *atomic.Int32
	}{{"cell-a", "retry-a", &callsA}, {"cell-b", "concurrent-b", &callsB}} {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			processed, runErr := store.ProcessOneInCell(context.Background(), item.cell, func(_ context.Context, tx *gorm.DB, got Message) error {
				if got.BrokerCell != item.cell || got.SourceEventID != item.want {
					return errors.New("worker received foreign-cell receipt")
				}
				item.calls.Add(1)
				return tx.Exec("INSERT INTO cell_processing_effects(receipt_id,broker_cell) VALUES (?,?)", got.ID, item.cell).Error
			})
			if runErr != nil || !processed {
				errs <- errors.Join(runErr, func() error {
					if !processed {
						return errors.New("no receipt processed")
					}
					return nil
				}())
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for runErr := range errs {
		t.Error(runErr)
	}
	if callsA.Load() != 1 || callsB.Load() != 1 {
		t.Fatalf("concurrent calls A=%d B=%d, want one each", callsA.Load(), callsB.Load())
	}
	var effectCount int64
	if err := db.Table("cell_processing_effects").Count(&effectCount).Error; err != nil {
		t.Fatal(err)
	}
	if effectCount != 2 {
		t.Fatalf("effect rows=%d, want exactly 2", effectCount)
	}
}
