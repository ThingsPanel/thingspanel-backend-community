package notification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
	"github.com/google/uuid"
)

func TestLocalQAConcurrentDifferentDigestSameIdempotencyKey(t *testing.T) {
	store, db := newMigratedTestStore(t)
	instanceID := createWorkerFixtureInstance(t, store, "local-qa-idem", "https://idem.example.test")
	base := contract.NotificationSubmit{Source: contract.SourceRef{Type: "test", ID: "local-qa-idem"}, ExpiresAt: time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)}
	text := "same key concurrent body"
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			request := base
			request.Deliveries = []contract.SubmitDelivery{{InstanceID: instanceID, Recipient: contract.Recipient{Kind: "email", Address: fmt.Sprintf("idem-%d@example.test", i)}, Content: contract.Content{Kind: "text", Text: &text}}}
			_, err := store.AcceptSubmission(context.Background(), "local-qa-idem", "same-key-concurrent", strings.Repeat(string(rune('a'+i)), 64), request)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if isStoreError(err, StoreErrorConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent idempotency outcome: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("same key/different digest outcomes success=%d conflict=%d", successes, conflicts)
	}
	var requests, deliveries, keys int
	if err := db.QueryRow(`SELECT count(*) FROM notification_requests WHERE tenant_id=$1`, "local-qa-idem").Scan(&requests); err != nil {
		t.Fatal("count accepted requests")
	}
	if err := db.QueryRow(`SELECT count(*) FROM notification_deliveries WHERE tenant_id=$1`, "local-qa-idem").Scan(&deliveries); err != nil {
		t.Fatal("count accepted deliveries")
	}
	if err := db.QueryRow(`SELECT count(*) FROM notification_idempotency WHERE tenant_id=$1 AND operation='submission' AND idempotency_key=$2`, "local-qa-idem", "same-key-concurrent").Scan(&keys); err != nil {
		t.Fatal("count idempotency records")
	}
	if requests != 1 || deliveries != 1 || keys != 1 {
		t.Fatalf("conflicting replay changed durable state: requests=%d deliveries=%d keys=%d", requests, deliveries, keys)
	}
}

func TestLocalQACrossTenantCompositeForeignKeyRejectsMixedRows(t *testing.T) {
	store, db := newMigratedTestStore(t)
	instanceA := createWorkerFixtureInstance(t, store, "local-qa-fk-a", "https://fk-a.example.test")
	instanceB := createWorkerFixtureInstance(t, store, "local-qa-fk-b", "https://fk-b.example.test")
	accepted := submitWorkerFixtures(t, store, "local-qa-fk-a", instanceA, 1)
	acceptedB := submitWorkerFixtures(t, store, "local-qa-fk-b", instanceB, 1)
	_, err := db.Exec(`INSERT INTO notification_deliveries(
		tenant_id,id,request_id,ordinal,instance_id,config_version,recipient_kind,recipient_display,
		recipient_key_ref,recipient_ciphertext,recipient_aad_version,content_key_ref,content_ciphertext,
		content_aad_version,expires_at,dispatch_status,delivery_status,next_attempt_at)
		VALUES($1,$2,$3,7,$4,1,'email','***','fixture',decode('01','hex'),1,'fixture',decode('02','hex'),1,NOW()+INTERVAL '1 minute','queued','pending',NOW())`,
		"local-qa-fk-a", uuid.NewString(), accepted.NotificationID, instanceB)
	if err == nil {
		t.Fatal("delivery with tenant A request and tenant B instance passed composite tenant foreign keys")
	}
	var rows int
	if scanErr := db.QueryRow(`SELECT count(*) FROM notification_deliveries WHERE tenant_id=$1 AND request_id=$2`, "local-qa-fk-a", accepted.NotificationID).Scan(&rows); scanErr != nil || rows != 1 {
		t.Fatalf("rejected cross-tenant row left partial data: count=%d err=%v", rows, scanErr)
	}
	for _, item := range []struct{ tenant, wantID string }{{"local-qa-fk-a", accepted.NotificationID}, {"local-qa-fk-b", acceptedB.NotificationID}} {
		handler := &NotificationHTTPHandler{Core: &Core{}, Store: store, Verifier: &coreVerifierFixture{user: UserPrincipal{UserID: "local-qa-reader", TenantID: item.tenant, Scopes: []string{"notification:read"}}}, Ready: func() error { return nil }}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/notifications?page=1&pageSize=20", nil))
		var envelope contract.Envelope[contract.NotificationListData]
		if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil || envelope.Data.Total != 1 || len(envelope.Data.Items) != 1 || envelope.Data.Items[0].ID != item.wantID {
			t.Fatalf("tenant %s list leaked or hid rows: status=%d total=%d items=%+v body=%s", item.tenant, recorder.Code, envelope.Data.Total, envelope.Data.Items, recorder.Body.String())
		}
	}
}

func TestLocalQATwoWorkersUsePostgresClaimBeforeOneHTTPSubmission(t *testing.T) {
	store, db := newMigratedTestStore(t)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		var request contract.PluginSendRequestV1
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode plugin send request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contract.PluginSendResultV1{DeliveryID: request.DeliveryID, Outcome: "accepted", ReceiptExpected: false})
	}))
	defer server.Close()
	instanceID := createWorkerFixtureInstance(t, store, "local-qa-two-workers", server.URL)
	accepted := submitWorkerFixtures(t, store, "local-qa-two-workers", instanceID, 1)
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal("construct fixture-only plugin client")
	}
	sender := &InstancePluginSender{Client: client, Runtimes: store}
	newWorker := func() *Worker {
		worker, err := NewWorker(WorkerOptions{Store: store, Sender: sender, Concurrency: 1, BatchSize: 1, PluginTimeout: 20 * time.Second, FinishTimeout: time.Second, LeaseFor: 22 * time.Second})
		if err != nil {
			t.Fatalf("construct worker: %v", err)
		}
		return worker
	}
	workerA, workerB := newWorker(), newWorker()
	first := make(chan error, 1)
	go func() {
		cycle, err := workerA.RunOnce(context.Background())
		if err == nil && (cycle.Claimed != 1 || cycle.Finished != 1) {
			err = fmt.Errorf("worker A cycle claimed=%d finished=%d", cycle.Claimed, cycle.Finished)
		}
		first <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("worker A did not reach fixture HTTP endpoint")
	}
	cycleB, err := workerB.RunOnce(context.Background())
	if err != nil || cycleB.Claimed != 0 {
		close(release)
		t.Fatalf("worker B claimed an instance already sending: cycle=%+v err=%v", cycleB, err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRow(`SELECT dispatch_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, "local-qa-two-workers", accepted.DeliveryIDs[0]).Scan(&status); err != nil || status != "accepted" || calls.Load() != 1 {
		t.Fatalf("two-worker HTTP result status=%q calls=%d err=%v", status, calls.Load(), err)
	}
}

func TestLocalQAReceiptIsScopedByTenantAndInstanceWhenProviderIDRepeats(t *testing.T) {
	store, db := newMigratedTestStore(t)
	type deliveryFixture struct{ tenant, instance, id string }
	fixtures := []deliveryFixture{}
	for _, item := range []struct{ tenant, suffix string }{{"local-qa-receipt-a", "a1"}, {"local-qa-receipt-a", "a2"}, {"local-qa-receipt-b", "b1"}} {
		instance := createWorkerFixtureInstance(t, store, item.tenant, "https://receipt-scope.example.test")
		accepted := submitWorkerFixtures(t, store, item.tenant, instance, 1)
		claims, _, err := store.ClaimDeliveriesFair(context.Background(), 1, time.Minute, "")
		if err != nil || len(claims) != 1 {
			t.Fatalf("claim scoped receipt fixture for %s: claims=%d err=%v", item.suffix, len(claims), err)
		}
		providerID := "same-provider-id"
		if err := store.FinishDelivery(context.Background(), DeliveryFinish{TenantID: item.tenant, DeliveryID: accepted.DeliveryIDs[0], AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken, Result: contract.PluginSendResultV1{DeliveryID: accepted.DeliveryIDs[0], Outcome: "accepted", ProviderMessageID: &providerID, ReceiptExpected: true}, FinishedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("finish scoped receipt fixture for %s: %v", item.suffix, err)
		}
		fixtures = append(fixtures, deliveryFixture{tenant: item.tenant, instance: instance, id: accepted.DeliveryIDs[0]})
	}
	event := contract.PluginReceiptV1{EventID: "only-tenant-a", ProviderMessageID: "same-provider-id", Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	if ack, err := store.AcceptReceipt(context.Background(), ReceiptPrincipal{TenantID: fixtures[0].tenant, InstanceID: fixtures[0].instance}, event, strings.Repeat("a", 64)); err != nil || !ack.Accepted {
		t.Fatalf("accept tenant A receipt: ack=%+v err=%v", ack, err)
	}
	for i, fixture := range fixtures {
		var status string
		if err := db.QueryRow(`SELECT delivery_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, fixture.tenant, fixture.id).Scan(&status); err != nil {
			t.Fatal("read scoped receipt result")
		}
		if i == 0 && status != "delivered" || i > 0 && status != "pending" {
			t.Fatalf("provider id crossed tenant scope: tenant=%s status=%s", fixture.tenant, status)
		}
	}
}

func TestLocalQAReconstructedStoreReconcilesDurableEarlyReceipt(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	tenant := "local-qa-reconcile-restart"
	instance := createWorkerFixtureInstance(t, store, tenant, "https://reconcile-restart.example.test")
	accepted := submitWorkerFixtures(t, store, tenant, instance, 1)
	deliveryID := accepted.DeliveryIDs[0]
	event := contract.PluginReceiptV1{EventID: "durable-before-claim", ProviderMessageID: "restart-provider", DeliveryID: &deliveryID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	if ack, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: tenant, InstanceID: instance}, event, strings.Repeat("e", 64)); err != nil || !ack.Accepted {
		t.Fatalf("persist pre-attempt receipt: ack=%+v err=%v", ack, err)
	}
	rebuilt, err := NewStore(StoreOptions{DB: db, Protector: newTestSnapshotProtector()})
	if err != nil {
		t.Fatal("rebuild store over persisted database")
	}
	claims, _, err := rebuilt.ClaimDeliveriesFair(ctx, 1, time.Minute, "")
	if err != nil || len(claims) != 1 {
		t.Fatalf("rebuilt worker claim: claims=%d err=%v", len(claims), err)
	}
	recovered, err := NewStore(StoreOptions{DB: db, Protector: newTestSnapshotProtector()})
	if err != nil {
		t.Fatal("rebuild recovery store over persisted database")
	}
	matched, err := recovered.ReconcileReceipts(ctx, 10)
	if err != nil || matched != 1 {
		t.Fatalf("reconstructed receipt reconciliation: matched=%d err=%v", matched, err)
	}
	var deliveryStatus, inboxStatus string
	if err := db.QueryRow(`SELECT delivery_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, deliveryID).Scan(&deliveryStatus); err != nil {
		t.Fatal("read reconstructed receipt result")
	}
	if err := db.QueryRow(`SELECT processing_status FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3`, tenant, instance, event.EventID).Scan(&inboxStatus); err != nil {
		t.Fatal("read reconstructed inbox result")
	}
	if deliveryStatus != "delivered" || inboxStatus != "matched" {
		t.Fatalf("reconstructed recovery lost durable state: delivery=%q inbox=%q", deliveryStatus, inboxStatus)
	}
}

func TestLocalQATerminalReceiptStateMatrix(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	tenant := "local-qa-receipt-matrix"
	instance := createWorkerFixtureInstance(t, store, tenant, "https://receipt-matrix.example.test")
	_ = submitWorkerFixtures(t, store, tenant, instance, 3)
	claims := make([]DeliveryClaim, 0, 3)
	cursor := ""
	for i := 0; i < 3; i++ {
		current, next, err := store.ClaimDeliveriesFair(ctx, 1, time.Minute, cursor)
		if err != nil || len(current) != 1 {
			t.Fatalf("claim terminal matrix fixture %d: claims=%d err=%v", i, len(current), err)
		}
		claim := current[0]
		providerID := "matrix-" + claim.DeliveryID
		if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: tenant, DeliveryID: claim.DeliveryID, AttemptID: claim.AttemptID, LeaseToken: claim.LeaseToken, Result: contract.PluginSendResultV1{DeliveryID: claim.DeliveryID, Outcome: "accepted", ProviderMessageID: &providerID, ReceiptExpected: true}, FinishedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("finish matrix fixture: %v", err)
		}
		claims = append(claims, claim)
		cursor = next
	}
	type receiptStep struct{ status string }
	sequences := []struct {
		initial   string
		steps     []receiptStep
		want      string
		conflicts int
	}{
		{initial: "failed", steps: []receiptStep{{"delivered"}, {"unknown"}}, want: "failed", conflicts: 2},
		{initial: "delivered", steps: []receiptStep{{"failed"}, {"unknown"}}, want: "delivered", conflicts: 2},
		{initial: "unknown", steps: []receiptStep{{"delivered"}}, want: "delivered", conflicts: 0},
	}
	for idx, sequence := range sequences {
		claim := claims[idx]
		statuses := append([]receiptStep{{sequence.initial}}, sequence.steps...)
		for stepIdx, step := range statuses {
			deliveryID := claim.DeliveryID
			event := contract.PluginReceiptV1{EventID: fmt.Sprintf("matrix-%d-%d", idx, stepIdx), ProviderMessageID: "matrix-" + claim.DeliveryID, DeliveryID: &deliveryID, Status: step.status, OccurredAt: time.Now().UTC().Format(time.RFC3339)}
			if ack, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: tenant, InstanceID: instance}, event, strings.Repeat("a", 64)); err != nil || !ack.Accepted {
				t.Fatalf("accept receipt matrix status %s: ack=%+v err=%v", step.status, ack, err)
			}
		}
		var got string
		if err := db.QueryRow(`SELECT delivery_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, claim.DeliveryID).Scan(&got); err != nil || got != sequence.want {
			t.Fatalf("terminal matrix sequence %q then %v = %q, want %q (err=%v)", sequence.initial, sequence.steps, got, sequence.want, err)
		}
		var conflicts int
		if err := db.QueryRow(`SELECT count(*) FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2 AND event_id LIKE $3 AND processing_status='conflict'`, tenant, instance, fmt.Sprintf("matrix-%d-%%", idx)).Scan(&conflicts); err != nil || conflicts != sequence.conflicts {
			t.Fatalf("terminal matrix conflict evidence=%d want=%d err=%v", conflicts, sequence.conflicts, err)
		}
	}
	for i, status := range []string{"sent", "ringing"} {
		invalid := contract.PluginReceiptV1{EventID: fmt.Sprintf("invalid-intermediate-%d", i), ProviderMessageID: "matrix-invalid", Status: status, OccurredAt: time.Now().UTC().Format(time.RFC3339)}
		if _, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: tenant, InstanceID: instance}, invalid, strings.Repeat("b", 64)); !isStoreError(err, StoreErrorInvalid) {
			t.Fatalf("intermediate provider status %q was accepted as terminal: %v", status, err)
		}
	}
}

type localQAReceiptVerifier struct{ principal ReceiptPrincipal }

func (v localQAReceiptVerifier) VerifyUser(context.Context, string) (UserPrincipal, error) {
	return UserPrincipal{}, errors.New("unused")
}
func (v localQAReceiptVerifier) VerifySource(context.Context, string) (SourcePrincipal, error) {
	return SourcePrincipal{}, errors.New("unused")
}
func (v localQAReceiptVerifier) VerifySourceProjection(context.Context, string) (SourceProjectionPrincipal, error) {
	return SourceProjectionPrincipal{}, errors.New("unused")
}
func (v localQAReceiptVerifier) VerifyReceipt(context.Context, string, string) (ReceiptPrincipal, error) {
	return v.principal, nil
}

func TestLocalQAReceiptInboxInsertFailureReturnsNoAckAndPersistsNothing(t *testing.T) {
	store, db := newMigratedTestStore(t)
	tenant := "local-qa-receipt-insert-fail"
	instance := createWorkerFixtureInstance(t, store, tenant, "https://receipt-failure.example.test")
	if _, err := db.Exec(`CREATE FUNCTION notification_localqa_fail_receipt_insert() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'fixture first-stage receipt failure'; END; $$ LANGUAGE plpgsql`); err != nil {
		t.Fatal("install isolated receipt insert failure function")
	}
	if _, err := db.Exec(`CREATE TRIGGER notification_localqa_fail_receipt_insert BEFORE INSERT ON notification_receipt_inbox FOR EACH ROW EXECUTE FUNCTION notification_localqa_fail_receipt_insert()`); err != nil {
		t.Fatal("install isolated receipt insert failure trigger")
	}
	deliveryID := "fixture-delivery-not-used"
	receipt := contract.PluginReceiptV1{EventID: "first-stage-fails", ProviderMessageID: "fixture-provider", DeliveryID: &deliveryID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	body, _ := json.Marshal(receipt)
	handler := &NotificationHTTPHandler{Verifier: localQAReceiptVerifier{principal: ReceiptPrincipal{TenantID: tenant, InstanceID: instance}}, Core: &Core{}, Store: store, Ready: func() error { return nil }}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notification-instances/"+instance+"/receipts", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer fixture-receipt")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2`, tenant, instance).Scan(&count); err != nil {
		t.Fatal("count failed receipt inbox event")
	}
	if response.Code == http.StatusOK || count != 0 {
		t.Fatalf("failed first-stage persistence was acknowledged: status=%d inboxRows=%d", response.Code, count)
	}
}

func TestLocalQAMaintenancePreservesUnknownInboxAndIdempotencyHistory(t *testing.T) {
	store, db := newMigratedTestStore(t)
	tenant := "local-qa-retention"
	instance := createWorkerFixtureInstance(t, store, tenant, "https://retention.example.test")
	accepted := submitWorkerFixtures(t, store, tenant, instance, 1)
	claims, _, err := store.ClaimDeliveriesFair(context.Background(), 1, time.Minute, "")
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim retention fixture: claims=%d err=%v", len(claims), err)
	}
	if err := store.FinishDelivery(context.Background(), DeliveryFinish{TenantID: tenant, DeliveryID: accepted.DeliveryIDs[0], AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken, Result: uncertainPluginSubmission(accepted.DeliveryIDs[0]), FinishedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("finish unknown retention fixture: %v", err)
	}
	providerID := "unmatched-retained-provider"
	if ack, err := store.AcceptReceipt(context.Background(), ReceiptPrincipal{TenantID: tenant, InstanceID: instance}, contract.PluginReceiptV1{EventID: "retained-unmatched", ProviderMessageID: providerID, Status: "unknown", OccurredAt: time.Now().UTC().Format(time.RFC3339)}, strings.Repeat("d", 64)); err != nil || !ack.Accepted {
		t.Fatalf("persist unmatched retention receipt: ack=%+v err=%v", ack, err)
	}
	var requestDigest, deliveryStatus, inboxDigest string
	readSnapshot := func() {
		t.Helper()
		if err := db.QueryRow(`SELECT intent_digest FROM notification_requests WHERE tenant_id=$1 AND id=$2`, tenant, accepted.NotificationID).Scan(&requestDigest); err != nil {
			t.Fatal("read retained request digest")
		}
		if err := db.QueryRow(`SELECT dispatch_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, accepted.DeliveryIDs[0]).Scan(&deliveryStatus); err != nil {
			t.Fatal("read retained delivery status")
		}
		if err := db.QueryRow(`SELECT event_digest FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2 AND event_id=$3`, tenant, instance, "retained-unmatched").Scan(&inboxDigest); err != nil {
			t.Fatal("read retained inbox digest")
		}
	}
	readSnapshot()
	if requestDigest == "" || deliveryStatus != "unknown" || inboxDigest != strings.Repeat("d", 64) {
		t.Fatalf("retention fixture was not durable: digest=%q status=%q inbox=%q", requestDigest, deliveryStatus, inboxDigest)
	}
	if _, err := store.ExpireQueued(context.Background(), time.Now().Add(time.Hour), 100); err != nil {
		t.Fatalf("run bounded expiration maintenance: %v", err)
	}
	if _, err := store.RecoverExpiredClaimsBounded(context.Background(), time.Now().Add(time.Hour), 100); err != nil {
		t.Fatalf("run bounded lease recovery maintenance: %v", err)
	}
	if _, err := store.ReconcileReceipts(context.Background(), 100); err != nil {
		t.Fatalf("run bounded receipt reconciliation maintenance: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM notification_idempotency WHERE tenant_id=$1 AND operation='submission'`, tenant).Scan(&count); err != nil || count != 1 {
		t.Fatalf("maintenance removed idempotency tombstone: count=%d err=%v", count, err)
	}
	readSnapshot()
	if deliveryStatus != "unknown" || requestDigest == "" || inboxDigest != strings.Repeat("d", 64) {
		t.Fatalf("maintenance changed protected history: digest=%q status=%q inbox=%q", requestDigest, deliveryStatus, inboxDigest)
	}
}

func TestLocalQAMigrationsPreserveExistingBusinessRowsAcrossUpgrade(t *testing.T) {
	db := openNotificationTestDB(t)
	ctx := context.Background()
	if err := runTestMigrationOnce(ctx, db, "0001", readNotificationMigration(t)); err != nil {
		t.Fatalf("apply initial migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO notification_requests(tenant_id,id,source_type,source_id,source_deployment_id,source_event_id,action_id,intent_digest,intake_status,blocked_reason,expires_at)
		VALUES('legacy-tenant','legacy-request','source','legacy-event','legacy-deployment','legacy-event','legacy-action',$1,'blocked','legacy_blocked',NOW()+INTERVAL '1 day')
	`, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("seed pre-upgrade business request: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO notification_idempotency(tenant_id,operation,idempotency_key,request_digest,resource_type,resource_id,resource_version)
		VALUES('legacy-tenant','source','legacy-event-key',$1,'notification','legacy-request',1)
	`, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("seed pre-upgrade idempotency row: %v", err)
	}
	for _, migration := range []struct{ version, body string }{
		{"0002", readNotificationGrantMigration(t)},
		{"0003", readNotificationAlarmSourceMigration(t)},
	} {
		if err := runTestMigrationOnce(ctx, db, migration.version, migration.body); err != nil {
			t.Fatalf("upgrade with migration %s: %v", migration.version, err)
		}
	}
	// Simulate a second service startup after all migrations are already applied.
	for _, migration := range []struct{ version, body string }{
		{"0001", readNotificationMigration(t)},
		{"0002", readNotificationGrantMigration(t)},
		{"0003", readNotificationAlarmSourceMigration(t)},
	} {
		if err := runTestMigrationOnce(ctx, db, migration.version, migration.body); err != nil {
			t.Fatalf("repeat startup migration %s: %v", migration.version, err)
		}
	}
	var requestID, sourceType, sourceEventID, digest, intakeStatus, blockedReason string
	if err := db.QueryRowContext(ctx, `SELECT id,source_type,source_event_id,intent_digest,intake_status,blocked_reason FROM notification_requests WHERE tenant_id='legacy-tenant' AND id='legacy-request'`).Scan(&requestID, &sourceType, &sourceEventID, &digest, &intakeStatus, &blockedReason); err != nil {
		t.Fatal("read preserved old business request")
	}
	if requestID != "legacy-request" || sourceType != "alarm" || sourceEventID != "legacy-event" || digest != strings.Repeat("a", 64) || intakeStatus != "blocked" || blockedReason != "legacy_blocked" {
		t.Fatalf("upgrade deleted or altered prior business facts: id=%q source=%q event=%q digest=%q intake=%q reason=%q", requestID, sourceType, sourceEventID, digest, intakeStatus, blockedReason)
	}
	var idemCount, versionCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_idempotency WHERE tenant_id='legacy-tenant' AND idempotency_key='legacy-event-key' AND resource_id='legacy-request' AND request_digest=$1`, strings.Repeat("a", 64)).Scan(&idemCount); err != nil || idemCount != 1 {
		t.Fatalf("old idempotency evidence count=%d err=%v", idemCount, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_test_migration_versions WHERE version IN ('0001','0002','0003')`).Scan(&versionCount); err != nil || versionCount != 3 {
		t.Fatalf("applied migration version count=%d err=%v", versionCount, err)
	}
}

func TestLocalQASecretPatchOmitSetClearAndConcurrentVersionReplay(t *testing.T) {
	const jwtKey = "fixture-secret-patch-jwt-key"
	t.Setenv("TP_JWT_KEY", jwtKey)
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	store, db := newMigratedTestStore(t)
	const tenantID = "local-qa-secret-patch"
	registrationID := createIdentityGuardPlugin(t, store, tenantID, "secret-patch-fixture", []string{"region"})
	instance := createIdentityGuardInstance(t, store, tenantID, registrationID, "secret-patch")
	handler := identityGuardHTTPHandler(t, store)
	bearer := makePluginGrantToken(jwtKey, "secret-patch-admin", "", "SYS_ADMIN")
	type putResult struct {
		key  string
		body []byte
		resp *httptest.ResponseRecorder
	}
	encodeUpdate := func(update contract.InstanceUpdate) []byte {
		t.Helper()
		body, err := json.Marshal(update)
		if err != nil {
			t.Fatal("marshal secret update")
		}
		return body
	}
	put := func(key string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/v2/platform/notification-tenants/"+tenantID+"/notification-instances/"+instance.ID, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		return resp
	}
	secretAtVersion := func(version int64) string {
		t.Helper()
		var keyRef string
		var ciphertext []byte
		var aadVersion int
		err := db.QueryRowContext(context.Background(), `SELECT key_ref,ciphertext,aad_version FROM notification_instance_secrets WHERE tenant_id=$1 AND instance_id=$2 AND config_version=$3 AND slot='password'`, tenantID, instance.ID, version).Scan(&keyRef, &ciphertext, &aadVersion)
		if err != nil {
			t.Fatalf("read password snapshot at version %d: %v", version, err)
		}
		opened, err := store.open(context.Background(), SnapshotAAD{ScopeKind: "tenant", TenantID: tenantID, ResourceID: instance.ID, Version: version, Purpose: "instance-secret", Slot: "password"}, ProtectedSnapshot{KeyRef: keyRef, Ciphertext: ciphertext, AADVersion: aadVersion})
		if err != nil {
			t.Fatalf("decrypt password snapshot at version %d: %v", version, err)
		}
		var value string
		if err := json.Unmarshal(opened, &value); err != nil {
			t.Fatalf("decode password snapshot at version %d: %v", version, err)
		}
		return value
	}
	omitted := put("secret-patch-omit", encodeUpdate(contract.InstanceUpdate{ExpectedVersion: 1, ConfigPatch: json.RawMessage(`{"host":"smtp-omit.example.test"}`)}))
	if omitted.Code != http.StatusOK || secretAtVersion(2) != "old-password" {
		t.Fatalf("omitted secret patch lost the prior value: status=%d body=%s", omitted.Code, omitted.Body.String())
	}
	setBody := encodeUpdate(contract.InstanceUpdate{ExpectedVersion: 2, Secrets: &contract.SecretPatch{Set: map[string]string{"password": "set-password"}}})
	set := put("secret-patch-set", setBody)
	if set.Code != http.StatusOK || secretAtVersion(3) != "set-password" {
		t.Fatalf("explicit secret set failed: status=%d body=%s", set.Code, set.Body.String())
	}
	clear := put("secret-patch-clear", encodeUpdate(contract.InstanceUpdate{ExpectedVersion: 3, Secrets: &contract.SecretPatch{Clear: []string{"password"}}}))
	if clear.Code != http.StatusOK {
		t.Fatalf("explicit secret clear failed: status=%d body=%s", clear.Code, clear.Body.String())
	}
	var clearedCount int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM notification_instance_secrets WHERE tenant_id=$1 AND instance_id=$2 AND config_version=4 AND slot='password'`, tenantID, instance.ID).Scan(&clearedCount); err != nil || clearedCount != 0 {
		t.Fatalf("clear retained a version-4 secret: rows=%d err=%v", clearedCount, err)
	}
	requests := []struct {
		key  string
		body []byte
	}{
		{"secret-patch-concurrent-set", encodeUpdate(contract.InstanceUpdate{ExpectedVersion: 4, Secrets: &contract.SecretPatch{Set: map[string]string{"password": "concurrent-password"}}})},
		{"secret-patch-concurrent-host", encodeUpdate(contract.InstanceUpdate{ExpectedVersion: 4, ConfigPatch: json.RawMessage(`{"host":"smtp-concurrent.example.test"}`)})},
	}
	start := make(chan struct{})
	results := make(chan putResult, 2)
	for _, item := range requests {
		item := item
		go func() {
			<-start
			results <- putResult{key: item.key, body: item.body, resp: put(item.key, item.body)}
		}()
	}
	close(start)
	var winner *putResult
	conflicts := 0
	for range requests {
		got := <-results
		if got.resp.Code == http.StatusOK {
			copy := got
			winner = &copy
		} else if got.resp.Code == http.StatusConflict {
			conflicts++
		} else {
			t.Fatalf("concurrent secret CAS status=%d body=%s", got.resp.Code, got.resp.Body.String())
		}
	}
	if winner == nil || conflicts != 1 {
		t.Fatalf("expected one secret update winner and one version conflict: winner=%+v conflicts=%d", winner, conflicts)
	}
	var winnerEnvelope contract.Envelope[contract.InstanceView]
	if err := json.Unmarshal(winner.resp.Body.Bytes(), &winnerEnvelope); err != nil {
		t.Fatal("decode concurrent update response")
	}
	next := put("secret-patch-advance-after-winner", encodeUpdate(contract.InstanceUpdate{ExpectedVersion: winnerEnvelope.Data.ConfigVersion, Enabled: boolPtr(true)}))
	if next.Code != http.StatusOK {
		t.Fatalf("advance current version after winner: status=%d body=%s", next.Code, next.Body.String())
	}
	replay := put(winner.key, winner.body)
	if replay.Code != http.StatusOK {
		t.Fatalf("original successful update replay was rejected by newer version: status=%d body=%s", replay.Code, replay.Body.String())
	}
	var replayEnvelope contract.Envelope[contract.InstanceView]
	if err := json.Unmarshal(replay.Body.Bytes(), &replayEnvelope); err != nil || replayEnvelope.Data.ConfigVersion != winnerEnvelope.Data.ConfigVersion {
		t.Fatalf("original update replay drifted from stored version: got=%d original=%d err=%v", replayEnvelope.Data.ConfigVersion, winnerEnvelope.Data.ConfigVersion, err)
	}
}

func TestLocalQAWorkerHTTPRetryAfterMaxAttemptsAndUnknownResponses(t *testing.T) {
	t.Run("retryable rejection honors retry after and stops at max attempts", func(t *testing.T) {
		baseStore, db := newMigratedTestStore(t)
		clock := time.Now().UTC().Truncate(time.Microsecond)
		store, err := NewStore(StoreOptions{DB: db, Protector: newTestSnapshotProtector(), Clock: func() time.Time { return clock }, RetryPolicy: JitteredRetryPolicy{BaseDelay: time.Second, MaxDelay: 30 * time.Second, Jitter: func(time.Duration) time.Duration { return 0 }}})
		if err != nil {
			t.Fatal("construct fake-clock fixture store")
		}
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			call := calls.Add(1)
			retryAfter := 1
			if call > int32(maxDeliveryAttempts) {
				retryAfter = 30
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(contract.PluginSendResultV1{DeliveryID: requestDeliveryID(t, r), Outcome: "rejected", ReceiptExpected: false, Error: &contract.PluginDeliveryErrorV1{Code: "rate_limited", Message: "retry later", Retryable: true, RetryAfterSeconds: intPointer(retryAfter)}})
		}))
		defer server.Close()
		tenant := "local-qa-worker-retry"
		instance := createWorkerFixtureInstance(t, store, tenant, server.URL)
		accepted := submitWorkerFixtures(t, store, tenant, instance, 1)
		// The database's default next_attempt_at is server NOW(); move the fake
		// clock past fixture insertion before asking the worker to claim it.
		clock = clock.Add(2 * time.Second)
		client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: 3 * time.Second})
		if err != nil {
			t.Fatal("construct fixture-only HTTP client")
		}
		worker, err := NewWorker(WorkerOptions{Store: store, Sender: &InstancePluginSender{Client: client, Runtimes: store}, Clock: func() time.Time { return clock }, Concurrency: 1, BatchSize: 1, PluginTimeout: 3 * time.Second, FinishTimeout: time.Second, LeaseFor: 5 * time.Second})
		if err != nil {
			t.Fatal("construct fixture worker")
		}
		for attempt := 1; attempt <= maxDeliveryAttempts; attempt++ {
			before := clock
			cycle, err := worker.RunOnce(context.Background())
			if err != nil || cycle.Claimed != 1 || cycle.Finished != 1 {
				t.Fatalf("attempt %d cycle=%+v err=%v", attempt, cycle, err)
			}
			var dispatch string
			var due sql.NullTime
			if err := db.QueryRow(`SELECT dispatch_status,next_attempt_at FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, accepted.DeliveryIDs[0]).Scan(&dispatch, &due); err != nil {
				t.Fatal("read retried delivery")
			}
			if attempt < maxDeliveryAttempts {
				if dispatch != "queued" || !due.Valid || due.Time.Before(before.Add(time.Second)) {
					t.Fatalf("attempt %d did not honor Retry-After lower bound: status=%q due=%v before=%v", attempt, dispatch, due, before)
				}
				clock = clock.Add(2 * time.Second)
			} else if dispatch != "failed" || due.Valid {
				t.Fatalf("retry attempt limit did not end in failed: status=%q due=%v", dispatch, due)
			}
		}
		if calls.Load() != int32(maxDeliveryAttempts) {
			t.Fatalf("retryable rejection calls=%d, want max attempts %d", calls.Load(), maxDeliveryAttempts)
		}
		cycle, err := worker.RunOnce(context.Background())
		if err != nil || cycle.Claimed != 0 || calls.Load() != int32(maxDeliveryAttempts) {
			t.Fatalf("terminal failure was retried: cycle=%+v calls=%d err=%v", cycle, calls.Load(), err)
		}
		expiredRequest := fixtureSubmission(instance, "retry-expired@example.test")
		// Keep enough wall-clock margin for the PG fixture while remaining shorter
		// than Retry-After, so the intended assertion is expiry fencing, not timing.
		expiredRequest.ExpiresAt = clock.Add(10 * time.Second).Format(time.RFC3339)
		expired, err := baseStore.AcceptSubmission(context.Background(), tenant, "expired-retry-key", strings.Repeat("9", 64), expiredRequest)
		if err != nil {
			t.Fatalf("accept near-expiry retry fixture: %v", err)
		}
		cycle, err = worker.RunOnce(context.Background())
		if err != nil || cycle.Claimed != 1 || cycle.Finished != 1 {
			t.Fatalf("near-expiry rejection cycle=%+v err=%v", cycle, err)
		}
		var expiredStatus string
		var expiredRetry sql.NullTime
		if err := db.QueryRow(`SELECT dispatch_status,next_attempt_at FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, expired.DeliveryIDs[0]).Scan(&expiredStatus, &expiredRetry); err != nil || expiredStatus != "failed" || expiredRetry.Valid {
			t.Fatalf("retry escaped expiry deadline: status=%q next=%v err=%v", expiredStatus, expiredRetry, err)
		}
		if calls.Load() != int32(maxDeliveryAttempts+1) {
			t.Fatalf("near-expiry delivery was resent: calls=%d", calls.Load())
		}
	})

	t.Run("429 503 and malformed 200 are unknown and never retried", func(t *testing.T) {
		store, db := newMigratedTestStore(t)
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch calls.Add(1) {
			case 1:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"outcome":"rejected","retryable":true}`))
			case 2:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"outcome":"rejected","retryable":true}`))
			default:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"delivery_id":`))
			}
		}))
		defer server.Close()
		tenant := "local-qa-worker-unknown"
		instance := createWorkerFixtureInstance(t, store, tenant, server.URL)
		accepted := submitWorkerFixtures(t, store, tenant, instance, 3)
		client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: 3 * time.Second})
		if err != nil {
			t.Fatal("construct fixture-only HTTP client")
		}
		worker, err := NewWorker(WorkerOptions{Store: store, Sender: &InstancePluginSender{Client: client, Runtimes: store}, Concurrency: 1, BatchSize: 1, PluginTimeout: 3 * time.Second, FinishTimeout: time.Second, LeaseFor: 5 * time.Second})
		if err != nil {
			t.Fatal("construct fixture worker")
		}
		for i := 0; i < 3; i++ {
			cycle, err := worker.RunOnce(context.Background())
			if err != nil || cycle.Claimed != 1 || cycle.Unknown != 1 {
				t.Fatalf("unknown response cycle %d=%+v err=%v", i, cycle, err)
			}
		}
		cycle, err := worker.RunOnce(context.Background())
		if err != nil || cycle.Claimed != 0 || calls.Load() != 3 {
			t.Fatalf("unknown outcomes were automatically retried: cycle=%+v calls=%d err=%v", cycle, calls.Load(), err)
		}
		for _, id := range accepted.DeliveryIDs {
			var dispatch string
			if err := db.QueryRow(`SELECT dispatch_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&dispatch); err != nil || dispatch != "unknown" {
				t.Fatalf("ambiguous HTTP outcome did not stay unknown: id=%s status=%q err=%v", id, dispatch, err)
			}
		}
	})
}

func TestLocalQAWorkerStartStopPersistsUnknownAndRebuiltWorkerResumesQueue(t *testing.T) {
	store, db := newMigratedTestStore(t)
	tenant := "local-qa-worker-shutdown"
	firstStarted := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			select {
			case firstStarted <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-releaseFirst:
			}
			return
		}
		var request contract.PluginSendRequestV1
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode resumed send: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contract.PluginSendResultV1{DeliveryID: request.DeliveryID, Outcome: "accepted", ReceiptExpected: false})
	}))
	defer server.Close()
	defer releaseOnce.Do(func() { close(releaseFirst) })
	instance := createWorkerFixtureInstance(t, store, tenant, server.URL)
	first := submitWorkerFixtures(t, store, tenant, instance, 1)
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal("construct fixture-only HTTP client")
	}
	newWorker := func(rebuilt *Store) *Worker {
		worker, err := NewWorker(WorkerOptions{Store: rebuilt, Sender: &InstancePluginSender{Client: client, Runtimes: rebuilt}, PollInterval: 10 * time.Millisecond, Concurrency: 1, BatchSize: 1, PluginTimeout: 10 * time.Second, FinishTimeout: time.Second, LeaseFor: 12 * time.Second})
		if err != nil {
			t.Fatalf("construct worker: %v", err)
		}
		return worker
	}
	worker := newWorker(store)
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("start PG worker: %v", err)
	}
	select {
	case <-firstStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("started worker did not reach fake HTTP side effect")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := worker.Stop(stopCtx); err != nil {
		t.Fatalf("stop waited for PG finish persistence: %v", err)
	}
	var firstStatus, firstAttemptStatus string
	if err := db.QueryRow(`SELECT dispatch_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, first.DeliveryIDs[0]).Scan(&firstStatus); err != nil {
		t.Fatal("read stopped in-flight delivery")
	}
	if err := db.QueryRow(`SELECT status FROM notification_delivery_attempts WHERE tenant_id=$1 AND delivery_id=$2 ORDER BY started_at DESC LIMIT 1`, tenant, first.DeliveryIDs[0]).Scan(&firstAttemptStatus); err != nil {
		t.Fatal("read stopped in-flight attempt")
	}
	if firstStatus != "unknown" || firstAttemptStatus != "unknown" {
		t.Fatalf("shutdown did not persist ambiguous call as unknown: delivery=%q attempt=%q", firstStatus, firstAttemptStatus)
	}
	second := submitWorkerFixtures(t, store, tenant, instance, 1)
	rebuilt, err := NewStore(StoreOptions{DB: db, Protector: newTestSnapshotProtector()})
	if err != nil {
		t.Fatal("rebuild Store over PG after worker stop")
	}
	restartedWorker := newWorker(rebuilt)
	cycle, err := restartedWorker.RunOnce(context.Background())
	if err != nil || cycle.Claimed != 1 || cycle.Finished != 1 || calls.Load() != 2 {
		t.Fatalf("rebuilt worker did not resume durable queue once: cycle=%+v calls=%d err=%v", cycle, calls.Load(), err)
	}
	var secondStatus string
	if err := db.QueryRow(`SELECT dispatch_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, second.DeliveryIDs[0]).Scan(&secondStatus); err != nil || secondStatus != "accepted" {
		t.Fatalf("rebuilt queue result=%q err=%v", secondStatus, err)
	}
	if _, err := restartedWorker.RunOnce(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatalf("rebuilt worker resent terminal/unknown rows: calls=%d err=%v", calls.Load(), err)
	}
}

func TestLocalQAReceiptCredentialPurposeAndInstancePathMatrix(t *testing.T) {
	store, db := newMigratedTestStore(t)
	tenant := "local-qa-receipt-auth"
	instanceA := createWorkerFixtureInstance(t, store, tenant, "https://receipt-auth.example.test")
	instanceB := createWorkerFixtureInstance(t, store, "local-qa-receipt-other-tenant", "https://receipt-auth.example.test")
	validToken, wrongPurposeToken := "fixture-receipt-purpose-token", "fixture-source-purpose-token"
	tokenHash := func(value string) string { sum := sha256.Sum256([]byte(value)); return hex.EncodeToString(sum[:]) }
	if err := store.CreateNotificationCredential(context.Background(), NotificationCredentialRecord{ID: "local-qa-receipt-cred", Purpose: CredentialPurposeReceipt, TokenHash: tokenHash(validToken), TenantID: tenant, InstanceID: instanceA, ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal("insert fixture receipt credential")
	}
	if err := store.CreateNotificationCredential(context.Background(), NotificationCredentialRecord{ID: "local-qa-source-cred", Purpose: CredentialPurposeSource, TokenHash: tokenHash(wrongPurposeToken), DeploymentID: "fixture-deployment", AllowedTenantIDs: []string{tenant}, ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal("insert fixture source-only credential")
	}
	infraPath := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(infraPath, []byte(`{"log_config":"info"}`), 0600); err != nil {
		t.Fatal("write privacy fixture")
	}
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00")
	privacy, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatal("construct privacy fixture")
	}
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: time.Second})
	if err != nil {
		t.Fatal("construct plugin client fixture")
	}
	management, err := NewManagementService(store, store, store, store, store, validationSessionFixture{}, client, newTestSnapshotProtector(), privacy, ManagementOptions{TestMode: true, AllowHTTPTestOrigin: true})
	if err != nil {
		t.Fatal("construct fixture management identity")
	}
	handler := &NotificationHTTPHandler{Verifier: management, Core: &Core{}, Store: store, Ready: func() error { return nil }}
	receipt := contract.PluginReceiptV1{EventID: "auth-valid-event", ProviderMessageID: "auth-provider", Status: "unknown", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	body, _ := json.Marshal(receipt)
	request := func(path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	pathA := "/api/v1/notification-instances/" + instanceA + "/receipts"
	pathB := "/api/v1/notification-instances/" + instanceB + "/receipts"
	if got := request(pathA, wrongPurposeToken); got.Code != http.StatusUnauthorized {
		t.Fatalf("source-purpose credential passed receipt route: status=%d", got.Code)
	}
	if got := request(pathB, validToken); got.Code != http.StatusUnauthorized {
		t.Fatalf("receipt credential crossed its bound instance/tenant: status=%d", got.Code)
	}
	if got := request(pathA, validToken); got.Code != http.StatusOK {
		t.Fatalf("valid scoped receipt credential rejected: status=%d body=%s", got.Code, got.Body.String())
	}
	var rowsA, rowsB int
	if err := db.QueryRow(`SELECT count(*) FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2`, tenant, instanceA).Scan(&rowsA); err != nil {
		t.Fatal("count authorized receipt")
	}
	if err := db.QueryRow(`SELECT count(*) FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2`, "local-qa-receipt-other-tenant", instanceB).Scan(&rowsB); err != nil {
		t.Fatal("count cross-scope receipt")
	}
	if rowsA != 1 || rowsB != 0 {
		t.Fatalf("receipt auth matrix rows A=%d B=%d", rowsA, rowsB)
	}
	if err := store.RevokeReceiptCredential(context.Background(), tenant, instanceA, "local-qa-receipt-cred"); err != nil {
		t.Fatal("revoke scoped receipt credential")
	}
	if got := request(pathA, validToken); got.Code != http.StatusUnauthorized {
		t.Fatalf("revoked receipt credential still accepted: status=%d", got.Code)
	}
	var rowsAfterRevoke int
	if err := db.QueryRow(`SELECT count(*) FROM notification_receipt_inbox WHERE tenant_id=$1 AND instance_id=$2`, tenant, instanceA).Scan(&rowsAfterRevoke); err != nil || rowsAfterRevoke != 1 {
		t.Fatalf("revoked request persisted an event: rows=%d err=%v", rowsAfterRevoke, err)
	}
}

func TestLocalQAReceiptCancelsAQueuedRetryBeforeAnotherProviderCall(t *testing.T) {
	_, db := newMigratedTestStore(t)
	clock := time.Now().UTC().Truncate(time.Microsecond).Add(2 * time.Second)
	store, err := NewStore(StoreOptions{DB: db, Protector: newTestSnapshotProtector(), Clock: func() time.Time { return clock }})
	if err != nil {
		t.Fatal("construct fixture store with claim clock")
	}
	tenant := "local-qa-receipt-cancels-retry"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contract.PluginSendResultV1{DeliveryID: requestDeliveryID(t, r), Outcome: "rejected", ReceiptExpected: false, Error: &contract.PluginDeliveryErrorV1{Code: "temporary_rejection", Message: "retry later", Retryable: true, RetryAfterSeconds: intPointer(30)}})
	}))
	defer server.Close()
	instance := createWorkerFixtureInstance(t, store, tenant, server.URL)
	accepted := submitWorkerFixtures(t, store, tenant, instance, 1)
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal("construct fixture-only HTTP client")
	}
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: &InstancePluginSender{Client: client, Runtimes: store}, Clock: func() time.Time { return clock }, Concurrency: 1, BatchSize: 1, PluginTimeout: 3 * time.Second, FinishTimeout: time.Second, LeaseFor: 5 * time.Second})
	if err != nil {
		t.Fatal("construct fixture worker")
	}
	cycle, err := worker.RunOnce(context.Background())
	if err != nil || cycle.Claimed != 1 || cycle.Finished != 1 {
		t.Fatalf("retryable rejection cycle=%+v err=%v", cycle, err)
	}
	var queued string
	if err := db.QueryRow(`SELECT dispatch_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, accepted.DeliveryIDs[0]).Scan(&queued); err != nil || queued != "queued" {
		t.Fatalf("fixture did not schedule retry: status=%q err=%v", queued, err)
	}
	deliveryID := accepted.DeliveryIDs[0]
	event := contract.PluginReceiptV1{EventID: "receipt-cancels-scheduled-retry", ProviderMessageID: "late-confirmed-provider", DeliveryID: &deliveryID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	if ack, err := store.AcceptReceipt(context.Background(), ReceiptPrincipal{TenantID: tenant, InstanceID: instance}, event, strings.Repeat("c", 64)); err != nil || !ack.Accepted {
		t.Fatalf("accept receipt during retry wait: ack=%+v err=%v", ack, err)
	}
	cycle, err = worker.RunOnce(context.Background())
	if err != nil || cycle.Claimed != 0 || calls.Load() != 1 {
		t.Fatalf("receipt did not cancel scheduled resend: cycle=%+v calls=%d err=%v", cycle, calls.Load(), err)
	}
	var dispatch, status string
	if err := db.QueryRow(`SELECT dispatch_status,delivery_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, deliveryID).Scan(&dispatch, &status); err != nil || dispatch != "accepted" || status != "delivered" {
		t.Fatalf("receipt did not preserve delivered outcome: dispatch=%q delivery=%q err=%v", dispatch, status, err)
	}
}

func TestLocalQADisabledInstanceSkipsQueuedSendButAcceptsGraceReceipt(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	const tenant = "local-qa-credential-grace"
	providerID := "provider-accepted-before-rotation"
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contract.PluginSendResultV1{DeliveryID: requestDeliveryID(t, r), Outcome: "accepted", ReceiptExpected: true, ProviderMessageID: &providerID})
	}))
	defer server.Close()
	instanceID := createWorkerFixtureInstance(t, store, tenant, server.URL)
	priorSend := submitWorkerFixtures(t, store, tenant, instanceID, 1)
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: time.Second})
	if err != nil {
		t.Fatal("construct fixture plugin client")
	}
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: &InstancePluginSender{Client: client, Runtimes: store}, Concurrency: 1, BatchSize: 1, PluginTimeout: time.Second, FinishTimeout: time.Second, LeaseFor: 3 * time.Second})
	if err != nil {
		t.Fatal("construct fixture worker")
	}
	cycle, err := worker.RunOnce(ctx)
	if err != nil || cycle.Claimed != 1 || cycle.Finished != 1 || providerCalls.Load() != 1 {
		t.Fatalf("send historical fixture through worker HTTP: cycle=%+v calls=%d err=%v", cycle, providerCalls.Load(), err)
	}
	queued := submitWorkerFixtures(t, store, tenant, instanceID, 1)
	oldToken, newToken := "fixture-old-receipt-token", "fixture-new-receipt-token"
	hashToken := func(value string) string { sum := sha256.Sum256([]byte(value)); return hex.EncodeToString(sum[:]) }
	oldCredential := NotificationCredentialRecord{ID: "local-qa-receipt-old", Purpose: CredentialPurposeReceipt, TokenHash: hashToken(oldToken), TenantID: tenant, InstanceID: instanceID, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := store.CreateNotificationCredential(ctx, oldCredential); err != nil {
		t.Fatalf("create old receipt credential: %v", err)
	}
	nextCredential := NotificationCredentialRecord{ID: "local-qa-receipt-new", Purpose: CredentialPurposeReceipt, TokenHash: hashToken(newToken), TenantID: tenant, InstanceID: instanceID, ExpiresAt: time.Now().UTC().Add(2 * time.Hour)}
	if _, replayed, err := store.RotateReceiptCredential(ctx, tenant, "local-qa-grace-rotate", strings.Repeat("7", 64), "", nextCredential, json.RawMessage(`{"credential":"fixture-new-receipt-token"}`)); err != nil || replayed {
		t.Fatalf("rotate receipt credential: replayed=%v err=%v", replayed, err)
	}
	grace, err := store.FindNotificationCredential(ctx, CredentialPurposeReceipt, oldCredential.TokenHash)
	if err != nil || grace.RevokedAt != nil || !grace.ExpiresAt.After(time.Now().UTC()) || time.Until(grace.ExpiresAt) > 15*time.Minute {
		t.Fatalf("old receipt credential was not kept within grace window: %+v err=%v", grace, err)
	}
	disabled := false
	if _, err := store.UpdateInstance(ctx, tenant, instanceID, "local-qa-disable-after-rotation", strings.Repeat("8", 64), contract.InstanceUpdate{ExpectedVersion: 2, Enabled: &disabled}); err != nil {
		t.Fatalf("disable instance after rotation: %v", err)
	}
	cycle, err = worker.RunOnce(ctx)
	if err != nil || cycle.Claimed != 0 || providerCalls.Load() != 1 {
		t.Fatalf("disabled instance sent a queued delivery: cycle=%+v calls=%d err=%v", cycle, providerCalls.Load(), err)
	}
	var queuedStatus string
	var attempts int
	if err := db.QueryRowContext(ctx, `SELECT dispatch_status,attempt_count FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, queued.DeliveryIDs[0]).Scan(&queuedStatus, &attempts); err != nil || queuedStatus != "queued" || attempts != 0 {
		t.Fatalf("disabled queued row changed: status=%q attempts=%d err=%v", queuedStatus, attempts, err)
	}

	infraPath := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(infraPath, []byte(`{"log_config":"info"}`), 0600); err != nil {
		t.Fatal("write privacy fixture")
	}
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00")
	privacy, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatal("construct privacy guard")
	}
	management, err := NewManagementService(store, store, store, store, store, validationSessionFixture{}, client, newTestSnapshotProtector(), privacy, ManagementOptions{TestMode: true, AllowHTTPTestOrigin: true})
	if err != nil {
		t.Fatal("construct management credential verifier")
	}
	handler := &NotificationHTTPHandler{Verifier: management, Core: &Core{}, Store: store, Ready: func() error { return nil }}
	deliveryID := priorSend.DeliveryIDs[0]
	event := contract.PluginReceiptV1{EventID: "receipt-old-token-grace", ProviderMessageID: providerID, DeliveryID: &deliveryID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-instances/"+instanceID+"/receipts", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+oldToken)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("grace-period historical receipt was rejected: status=%d body=%s", resp.Code, resp.Body.String())
	}
	updated, err := store.GetDelivery(ctx, tenant, deliveryID)
	if err != nil || updated.DeliveryStatus != contract.DeliveryDelivered {
		t.Fatalf("historical receipt did not update its own delivery: view=%+v err=%v", updated, err)
	}
	if cycle, err := worker.RunOnce(ctx); err != nil || cycle.Claimed != 0 || providerCalls.Load() != 1 {
		t.Fatalf("receipt/disable combination caused a new provider send: cycle=%+v calls=%d err=%v", cycle, providerCalls.Load(), err)
	}
}

func TestLocalQAConcurrentGroupUpdatesCASAndReplayKeepWinningVersion(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	ctx := context.Background()
	group, err := store.CreateGroup(ctx, "local-qa-group-cas", "group-cas-create", strings.Repeat("a", 64), contract.GroupCreate{Name: "Initial", Enabled: true, Bindings: []contract.GroupBinding{}})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	type result struct {
		key, digest string
		view        contract.GroupView
		err         error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	requests := []struct {
		key, digest, name string
	}{
		{key: "group-cas-update-a", digest: strings.Repeat("b", 64), name: "Winner A"},
		{key: "group-cas-update-b", digest: strings.Repeat("c", 64), name: "Winner B"},
	}
	for _, request := range requests {
		request := request
		go func() {
			<-start
			view, err := store.UpdateGroup(ctx, "local-qa-group-cas", group.ID, request.key, request.digest, contract.GroupUpdate{ExpectedVersion: 1, GroupCreate: contract.GroupCreate{Name: request.name, Enabled: true, Bindings: []contract.GroupBinding{}}})
			results <- result{key: request.key, digest: request.digest, view: view, err: err}
		}()
	}
	close(start)
	var winner *result
	conflicts := 0
	for range requests {
		got := <-results
		if got.err == nil {
			if got.view.Version != 2 || got.view.Revision != 2 {
				t.Fatalf("successful CAS returned wrong version: %+v", got.view)
			}
			copy := got
			winner = &copy
		} else if isStoreError(got.err, StoreErrorVersionConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent group update error: %v", got.err)
		}
	}
	if winner == nil || conflicts != 1 {
		t.Fatalf("expected one successful version advance and one conflict: winner=%+v conflicts=%d", winner, conflicts)
	}
	replayed, ok, err := store.FindGroupReplay(ctx, "local-qa-group-cas", group.ID, "group.update:"+group.ID, winner.key, winner.digest)
	if err != nil || !ok || replayed.Version != 2 || replayed.Revision != 2 || replayed.Name != winner.view.Name {
		t.Fatalf("winning idempotency replay changed after concurrent loser: view=%+v ok=%v err=%v", replayed, ok, err)
	}
	current, err := store.GetGroup(ctx, "local-qa-group-cas", group.ID)
	if err != nil || current.Version != 2 || current.Name != winner.view.Name {
		t.Fatalf("group CAS current state=(%+v,%v)", current, err)
	}
}

func TestLocalQASourceSubmissionsPinGroupRevisionAcrossUpdate(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	tenant, deployment := "local-qa-group-pin", "local-qa-group-pin-deployment"
	instance := createFixtureInstance(t, store, tenant)
	group, err := store.CreateGroup(ctx, tenant, "group-pin-create", strings.Repeat("d", 64), contract.GroupCreate{Name: "Published v1", Enabled: true, Bindings: []contract.GroupBinding{{BindingID: "binding-1", InstanceID: instance, RecipientSource: json.RawMessage(`{"kind":"literal","recipient":{"kind":"email","address":"group@example.test"}}`), ContentBinding: json.RawMessage(`{"kind":"text","title":"v1","text":"v1"}`)}}})
	if err != nil {
		t.Fatalf("create published group: %v", err)
	}
	projectionPrincipal := SourceProjectionPrincipal{DeploymentID: deployment, TenantIDs: []string{tenant}}
	registerRevision := func(revision int64) {
		t.Helper()
		_, err := store.RegisterGroupProjection(ctx, projectionPrincipal, contract.SourceGroupProjectionRequest{SourceDeploymentID: deployment, TenantID: tenant, LegacyGroupID: "legacy-group", NotificationGroupID: group.ID, GroupRevision: revision})
		if err != nil {
			t.Fatalf("register projection revision %d: %v", revision, err)
		}
	}
	registerRevision(1)
	accept := func(eventID, idem string, revision int64, text string) contract.AcceptedData {
		t.Helper()
		event := contract.SourceEventV1{SchemaVersion: "1.0", SourceDeploymentID: deployment, SourceEventID: eventID, ActionID: "send-email", TenantID: tenant, NotificationGroupID: group.ID, GroupRevision: revision, OccurredAt: time.Now().UTC().Format(time.RFC3339), ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339)}
		content := contract.Content{Kind: "text", Title: &text, Text: &text}
		plan := SourceAcceptancePlan{Event: event, Deliveries: []contract.SubmitDelivery{{InstanceID: instance, Recipient: contract.Recipient{Kind: "email", Address: "group@example.test"}, Content: content}}}
		digest := strings.Repeat("e", 64)
		if revision == 2 {
			digest = strings.Repeat("f", 64)
		}
		accepted, err := store.AcceptSourcePlan(ctx, tenant, idem, digest, plan)
		if err != nil || !accepted.Accepted || len(accepted.DeliveryIDs) != 1 {
			t.Fatalf("accept source revision %d: result=%+v err=%v", revision, accepted, err)
		}
		return accepted
	}
	firstBody, secondBody := "published body v1", "published body v2"
	old := accept("group-pin-event-1", "group-pin-source-1", 1, firstBody)
	oldDelivery := old.DeliveryIDs[0]
	if _, err := store.UpdateGroup(ctx, tenant, group.ID, "group-pin-update", strings.Repeat("f", 64), contract.GroupUpdate{ExpectedVersion: 1, GroupCreate: contract.GroupCreate{Name: "Published v2", Enabled: true, Bindings: []contract.GroupBinding{{BindingID: "binding-1", InstanceID: instance, RecipientSource: json.RawMessage(`{"kind":"literal","recipient":{"kind":"email","address":"group@example.test"}}`), ContentBinding: json.RawMessage(`{"kind":"text","title":"v2","text":"v2"}`)}}}}); err != nil {
		t.Fatalf("publish group revision 2: %v", err)
	}
	registerRevision(2)
	oldReplay, ok, err := store.FindSourceReplay(ctx, tenant, "group-pin-source-1", strings.Repeat("e", 64), deployment, "group-pin-event-1", "send-email")
	if err != nil || !ok || oldReplay.NotificationID != old.NotificationID || len(oldReplay.DeliveryIDs) != 1 || oldReplay.DeliveryIDs[0] != oldDelivery {
		t.Fatalf("old source replay changed after group publish: replay=%+v ok=%v err=%v", oldReplay, ok, err)
	}
	new := accept("group-pin-event-2", "group-pin-source-2", 2, secondBody)
	for _, item := range []struct {
		requestID string
		want      int64
	}{{old.NotificationID, 1}, {new.NotificationID, 2}} {
		var revision int64
		if err := db.QueryRowContext(ctx, `SELECT group_revision FROM notification_requests WHERE tenant_id=$1 AND id=$2`, tenant, item.requestID).Scan(&revision); err != nil || revision != item.want {
			t.Fatalf("source request %s revision=%d err=%v want=%d", item.requestID, revision, err, item.want)
		}
	}
	var contentKey string
	var contentCipher []byte
	var contentAADVersion int
	if err := db.QueryRowContext(ctx, `SELECT content_key_ref,content_ciphertext,content_aad_version FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, tenant, oldDelivery).Scan(&contentKey, &contentCipher, &contentAADVersion); err != nil {
		t.Fatal("read original delivery snapshot")
	}
	opened, err := store.open(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: tenant, ResourceID: oldDelivery, Version: 1, Purpose: "delivery-content", Slot: "content"}, ProtectedSnapshot{KeyRef: contentKey, Ciphertext: contentCipher, AADVersion: contentAADVersion})
	if err != nil {
		t.Fatal("open original delivery snapshot")
	}
	var pinned contract.Content
	if err := json.Unmarshal(opened, &pinned); err != nil || pinned.Title == nil || *pinned.Title != firstBody || pinned.Text == nil || *pinned.Text != firstBody {
		t.Fatalf("old delivery snapshot changed after publishing revision 2: content=%s err=%v", opened, err)
	}
	oldView, err := store.ResolvePublished(ctx, tenant, group.ID, 1)
	newView, newErr := store.ResolvePublished(ctx, tenant, group.ID, 2)
	if err != nil || newErr != nil || oldView.Name != "Published v1" || newView.Name != "Published v2" {
		t.Fatalf("published history changed: rev1=(%+v,%v) rev2=(%+v,%v)", oldView, err, newView, newErr)
	}
}

func requestDeliveryID(t *testing.T, r *http.Request) string {
	t.Helper()
	var request contract.PluginSendRequestV1
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Errorf("decode plugin send request: %v", err)
	}
	return request.DeliveryID
}

func intPointer(value int) *int { return &value }
