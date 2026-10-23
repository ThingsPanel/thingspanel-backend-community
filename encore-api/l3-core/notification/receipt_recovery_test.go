package notification

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

func TestReceiptRecoverySkipsUnmatchedWithoutAttemptsAndFencesWorkerFinish(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createWorkerFixtureInstance(t, store, "receipt-recovery-tenant", "https://receipt-fixture.example.test")
	accepted := submitWorkerFixtures(t, store, "receipt-recovery-tenant", instanceID, 2)
	for i, deliveryID := range accepted.DeliveryIDs {
		deliveryID := deliveryID
		event := contract.PluginReceiptV1{
			EventID: "early-receipt-" + deliveryID, ProviderMessageID: "provider-message-" + deliveryID,
			DeliveryID: &deliveryID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339),
		}
		if _, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "receipt-recovery-tenant", InstanceID: instanceID}, event, strings.Repeat(string(rune('1'+i)), 64)); err != nil {
			t.Fatalf("persist early receipt %d: %v", i, err)
		}
	}
	var unmatched int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_receipt_inbox WHERE tenant_id=$1 AND processing_status='unmatched'`, "receipt-recovery-tenant").Scan(&unmatched); err != nil || unmatched != 2 {
		t.Fatalf("early receipts were not durably unmatched: count=%d err=%v", unmatched, err)
	}
	claims, _, err := store.ClaimDeliveriesFair(ctx, 1, time.Minute, "")
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim one delivery for reconciliation: claims=%d err=%v", len(claims), err)
	}
	matched, err := store.ReconcileReceipts(ctx, 1)
	if err != nil || matched != 1 {
		t.Fatalf("bounded receipt reconciliation starved on older unmatched events: matched=%d err=%v", matched, err)
	}
	var status, inboxStatus string
	if err := db.QueryRowContext(ctx, `SELECT delivery_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, claims[0].TenantID, claims[0].DeliveryID).Scan(&status); err != nil {
		t.Fatal("read receipt-promoted delivery")
	}
	if err := db.QueryRowContext(ctx, `SELECT processing_status FROM notification_receipt_inbox WHERE tenant_id=$1 AND requested_delivery_id=$2`, claims[0].TenantID, claims[0].DeliveryID).Scan(&inboxStatus); err != nil {
		t.Fatal("read matched receipt")
	}
	if status != "delivered" || inboxStatus != "matched" {
		t.Fatalf("receipt reconciliation failed: delivery=%q inbox=%q", status, inboxStatus)
	}
	if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: claims[0].TenantID, DeliveryID: claims[0].DeliveryID, AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken, Result: contract.PluginSendResultV1{DeliveryID: claims[0].DeliveryID, Outcome: "unknown"}, FinishedAt: time.Now().UTC()}); !isStoreError(err, StoreErrorStaleClaim) {
		t.Fatalf("worker result after authoritative receipt was not fenced: %v", err)
	}
	if remaining, err := store.ReconcileReceipts(ctx, 10); err != nil || remaining != 0 {
		t.Fatalf("matched receipt was reconsidered: count=%d err=%v", remaining, err)
	}
}

func TestConcurrentReceiptAndFinishRemainRecoverable(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	instanceID := createWorkerFixtureInstance(t, store, "receipt-finish-race", "https://receipt-race.example.test")
	accepted := submitWorkerFixtures(t, store, "receipt-finish-race", instanceID, 1)
	claims, _, err := store.ClaimDeliveriesFair(ctx, 1, time.Minute, "")
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim for receipt/finish race: claims=%d err=%v", len(claims), err)
	}
	if _, err := db.ExecContext(ctx, `CREATE FUNCTION notification_pause_finish() RETURNS TRIGGER AS $$ BEGIN IF NEW.provider_message_id='provider-race' THEN PERFORM pg_sleep(0.35); END IF; RETURN NEW; END; $$ LANGUAGE plpgsql`); err != nil {
		t.Fatal("install fixture-only race gate")
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER notification_pause_finish BEFORE UPDATE ON notification_deliveries FOR EACH ROW EXECUTE FUNCTION notification_pause_finish()`); err != nil {
		t.Fatal("install fixture-only race trigger")
	}
	providerID := "provider-race"
	deliveryID := claims[0].DeliveryID
	event := contract.PluginReceiptV1{EventID: "receipt-during-finish", ProviderMessageID: providerID, DeliveryID: &deliveryID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	var finishErr, receiptErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		finishErr = store.FinishDelivery(ctx, DeliveryFinish{
			TenantID: claims[0].TenantID, DeliveryID: deliveryID, AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken,
			Result: contract.PluginSendResultV1{DeliveryID: deliveryID, Outcome: "accepted", ProviderMessageID: &providerID, ReceiptExpected: true}, FinishedAt: time.Now().UTC(),
		})
	}()
	time.Sleep(80 * time.Millisecond)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, receiptErr = store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "receipt-finish-race", InstanceID: instanceID}, event, strings.Repeat("a", 64))
	}()
	wg.Wait()
	if finishErr != nil || receiptErr != nil {
		t.Fatalf("delivery-first locking should avoid PostgreSQL deadlock: finish=%v receipt=%v", finishErr, receiptErr)
	}
	var dispatch, deliveryStatus, inboxStatus string
	if err := db.QueryRowContext(ctx, `SELECT dispatch_status,delivery_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, "receipt-finish-race", accepted.DeliveryIDs[0]).Scan(&dispatch, &deliveryStatus); err != nil {
		t.Fatal("read raced delivery")
	}
	if err := db.QueryRowContext(ctx, `SELECT processing_status FROM notification_receipt_inbox WHERE tenant_id=$1 AND event_id=$2`, "receipt-finish-race", event.EventID).Scan(&inboxStatus); err != nil {
		t.Fatal("read raced receipt inbox")
	}
	if inboxStatus != "matched" || deliveryStatus != "delivered" || dispatch != "accepted" {
		t.Fatalf("receipt was not durable and recoverable after race: dispatch=%q delivery=%q inbox=%q finishErr=%v receiptErr=%v", dispatch, deliveryStatus, inboxStatus, finishErr, receiptErr)
	}
}

func TestReceiptAckSurvivesMatchTransactionFailureAndReconciles(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createWorkerFixtureInstance(t, store, "receipt-after-persist", "https://receipt-recovery.example.test")
	accepted := submitWorkerFixtures(t, store, "receipt-after-persist", instanceID, 1)
	claims, _, err := store.ClaimDeliveriesFair(ctx, 1, time.Minute, "")
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim for receipt match failure: claims=%d err=%v", len(claims), err)
	}
	if _, err := db.ExecContext(ctx, `CREATE FUNCTION notification_fail_receipt_match() RETURNS TRIGGER AS $$ BEGIN IF NEW.provider_message_id='provider-after-persist' THEN RAISE EXCEPTION 'fixture receipt match failure'; END IF; RETURN NEW; END; $$ LANGUAGE plpgsql`); err != nil {
		t.Fatal("install isolated second-stage failure trigger")
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER notification_fail_receipt_match BEFORE UPDATE ON notification_deliveries FOR EACH ROW EXECUTE FUNCTION notification_fail_receipt_match()`); err != nil {
		t.Fatal("install isolated second-stage failure trigger")
	}
	deliveryID := accepted.DeliveryIDs[0]
	event := contract.PluginReceiptV1{EventID: "receipt-after-persist", ProviderMessageID: "provider-after-persist", DeliveryID: &deliveryID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	ack, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "receipt-after-persist", InstanceID: instanceID}, event, strings.Repeat("b", 64))
	if err != nil || !ack.Accepted {
		t.Fatalf("second-stage SQL failure incorrectly denied durable ACK: ack=%+v err=%v", ack, err)
	}
	var inboxStatus string
	if err := db.QueryRowContext(ctx, `SELECT processing_status FROM notification_receipt_inbox WHERE tenant_id=$1 AND event_id=$2`, "receipt-after-persist", event.EventID).Scan(&inboxStatus); err != nil || inboxStatus != "unmatched" {
		t.Fatalf("receipt event was not durable after match rollback: status=%q err=%v", inboxStatus, err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER notification_fail_receipt_match ON notification_deliveries`); err != nil {
		t.Fatal("remove isolated second-stage failure trigger")
	}
	ack, err = store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "receipt-after-persist", InstanceID: instanceID}, event, strings.Repeat("b", 64))
	if err != nil || !ack.Accepted {
		t.Fatalf("same-digest replay failed to acknowledge and repair durable receipt: ack=%+v err=%v", ack, err)
	}
	var deliveryStatus, providerID string
	if err := db.QueryRowContext(ctx, `SELECT delivery_status,provider_message_id FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, "receipt-after-persist", accepted.DeliveryIDs[0]).Scan(&deliveryStatus, &providerID); err != nil {
		t.Fatal("read repaired delivery")
	}
	if deliveryStatus != "delivered" || providerID != event.ProviderMessageID {
		t.Fatalf("repair did not apply provider receipt: delivery=%q provider=%q", deliveryStatus, providerID)
	}
}

func TestReceiptRejectsDirectDeliveryProviderMismatchBeforePersistence(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createWorkerFixtureInstance(t, store, "receipt-provider-mismatch", "https://receipt-mismatch.example.test")
	accepted := submitWorkerFixtures(t, store, "receipt-provider-mismatch", instanceID, 1)
	claims, _, err := store.ClaimDeliveriesFair(ctx, 1, time.Minute, "")
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim for provider mismatch test: claims=%d err=%v", len(claims), err)
	}
	knownProvider := "provider-known"
	if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: "receipt-provider-mismatch", DeliveryID: claims[0].DeliveryID, AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken, Result: contract.PluginSendResultV1{DeliveryID: claims[0].DeliveryID, Outcome: "accepted", ProviderMessageID: &knownProvider, ReceiptExpected: true}, FinishedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("finish fixture delivery with known provider: %v", err)
	}
	deliveryID := accepted.DeliveryIDs[0]
	event := contract.PluginReceiptV1{EventID: "wrong-provider-receipt", ProviderMessageID: "provider-other", DeliveryID: &deliveryID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	if _, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "receipt-provider-mismatch", InstanceID: instanceID}, event, strings.Repeat("c", 64)); !isStoreError(err, StoreErrorConflict) {
		t.Fatalf("mismatched direct provider receipt = %v, want conflict", err)
	}
	var persisted int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_receipt_inbox WHERE tenant_id=$1 AND event_id=$2`, "receipt-provider-mismatch", event.EventID).Scan(&persisted); err != nil || persisted != 0 {
		t.Fatalf("mismatched receipt was persisted as valid: count=%d err=%v", persisted, err)
	}
}
