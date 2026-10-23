package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

func TestAcceptSubmissionSecondDeliveryFailureRollsBackAndSameKeyCanRetry(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-submit")
	request := fixtureSubmission(instanceID, "person@example.test")
	request.Deliveries = append(request.Deliveries, request.Deliveries[0])
	digest := strings.Repeat("d", 64)
	_, err := db.ExecContext(ctx, `CREATE FUNCTION notification_fail_second_delivery() RETURNS trigger AS $$ BEGIN IF NEW.ordinal=1 THEN RAISE EXCEPTION 'fixture second delivery failure'; END IF; RETURN NEW; END; $$ LANGUAGE plpgsql`)
	if err != nil {
		t.Fatal("install isolated failure trigger")
	}
	_, err = db.ExecContext(ctx, `CREATE TRIGGER notification_fail_second_delivery BEFORE INSERT ON notification_deliveries FOR EACH ROW EXECUTE FUNCTION notification_fail_second_delivery()`)
	if err != nil {
		t.Fatal("install isolated failure trigger")
	}
	if _, err := store.AcceptSubmission(ctx, "tenant-submit", "rollback-submit-001", digest, request); err == nil {
		t.Fatal("expected second-delivery failure")
	}
	for _, table := range []string{"notification_requests", "notification_deliveries"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", "tenant-submit").Scan(&count); err != nil {
			t.Fatal("verify failed fanout rollback")
		}
		if count != 0 {
			t.Fatalf("failed fanout left %d rows in %s", count, table)
		}
	}
	var submitIdem int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_idempotency WHERE tenant_id=$1 AND operation='submission'`, "tenant-submit").Scan(&submitIdem); err != nil || submitIdem != 0 {
		t.Fatalf("failed fanout idempotency rows=(%d,%v)", submitIdem, err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER notification_fail_second_delivery ON notification_deliveries`); err != nil {
		t.Fatal("remove isolated failure trigger")
	}
	accepted, err := store.AcceptSubmission(ctx, "tenant-submit", "rollback-submit-001", digest, request)
	if err != nil || len(accepted.DeliveryIDs) != 2 {
		t.Fatalf("retry after rolled back transaction = (%+v, %v)", accepted, err)
	}
}

func TestConcurrentIdempotentSubmissionTenantIsolationAndSingleInstanceClaim(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-concurrent")
	request := fixtureSubmission(instanceID, "recipient-secret@example.test")
	digest := strings.Repeat("e", 64)
	const callers = 2
	start := make(chan struct{})
	results := make([]contract.AcceptedData, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = store.AcceptSubmission(ctx, "tenant-concurrent", "concurrent-submit-01", digest, request)
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("concurrent submit %d: %v", i, errs[i])
		}
		if results[i].NotificationID != results[0].NotificationID {
			t.Fatal("same idempotency key returned different notification IDs")
		}
	}
	if _, err := store.AcceptSubmission(ctx, "tenant-concurrent", "concurrent-submit-01", strings.Repeat("9", 64), request); !isStoreError(err, StoreErrorConflict) {
		t.Fatalf("same key with a different digest error=%v, want conflict", err)
	}
	var requestCount, deliveryCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_requests`).Scan(&requestCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_deliveries`).Scan(&deliveryCount); err != nil {
		t.Fatal(err)
	}
	if requestCount != 1 || deliveryCount != 1 {
		t.Fatalf("idempotent submission rows = requests %d deliveries %d", requestCount, deliveryCount)
	}
	if _, err := store.GetNotification(ctx, "tenant-other", results[0].NotificationID); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("cross-tenant notification lookup = %v", err)
	}
	page, err := store.ListDeliveries(ctx, "tenant-concurrent", DeliveryFilter{Page: 1, PageSize: 10, SourceType: "test", SourceID: "fixture-source"})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Recipient.Display == "recipient-secret@example.test" {
		t.Fatalf("tenant-scoped delivery query=(%+v,%v)", page, err)
	}
	otherPage, err := store.ListDeliveries(ctx, "tenant-other", DeliveryFilter{Page: 1, PageSize: 10})
	if err != nil || otherPage.Total != 0 || len(otherPage.Items) != 0 {
		t.Fatalf("cross-tenant delivery query=(%+v,%v)", otherPage, err)
	}
	if _, err := store.GetDelivery(ctx, "tenant-other", results[0].DeliveryIDs[0]); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("cross-tenant delivery lookup=%v", err)
	}
	var rawCipher []byte
	if err := db.QueryRowContext(ctx, `SELECT recipient_ciphertext FROM notification_deliveries`).Scan(&rawCipher); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawCipher), "recipient-secret@example.test") {
		t.Fatal("recipient plaintext was stored")
	}
	claims := make([][]DeliveryClaim, 2)
	claimErr := make([]error, 2)
	start = make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			claims[i], claimErr[i] = store.ClaimDeliveries(ctx, 1, time.Minute)
		}(i)
	}
	close(start)
	wg.Wait()
	for _, err := range claimErr {
		if err != nil {
			t.Fatalf("concurrent claim: %v", err)
		}
	}
	total := len(claims[0]) + len(claims[1])
	if total != 1 {
		t.Fatalf("two workers claimed %d deliveries for one instance; want one", total)
	}
}

func TestEarlyReceiptMatchesOnlyAfterAttemptAndConflictingInboxIsDurable(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-receipt")
	request := fixtureSubmission(instanceID, "receipt@example.test")
	accepted, err := store.AcceptSubmission(ctx, "tenant-receipt", "receipt-submit-001", strings.Repeat("f", 64), request)
	if err != nil {
		t.Fatal("accept receipt fixture submission")
	}
	occurred := time.Now().UTC().Format(time.RFC3339)
	event := contract.PluginReceiptV1{EventID: "provider-event-1", ProviderMessageID: "provider-message-1", Status: "delivered", OccurredAt: occurred}
	ack, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "tenant-receipt", InstanceID: instanceID}, event, strings.Repeat("1", 64))
	if err != nil || !ack.Accepted {
		t.Fatalf("early receipt = (%+v, %v)", ack, err)
	}
	var inboxStatus string
	if err := db.QueryRowContext(ctx, `SELECT processing_status FROM notification_receipt_inbox WHERE event_id=$1`, event.EventID).Scan(&inboxStatus); err != nil || inboxStatus != "unmatched" {
		t.Fatalf("receipt before attempt status=(%s,%v), want unmatched", inboxStatus, err)
	}
	claims, err := store.ClaimDeliveries(ctx, 1, time.Minute)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim for early receipt = (%d,%v)", len(claims), err)
	}
	providerID := event.ProviderMessageID
	result := contract.PluginSendResultV1{DeliveryID: accepted.DeliveryIDs[0], Outcome: "accepted", ProviderMessageID: &providerID, ReceiptExpected: true}
	if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: "tenant-receipt", DeliveryID: accepted.DeliveryIDs[0], AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken, Result: result, FinishedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("finish provider attempt: %v", err)
	}
	delivery, err := store.GetDelivery(ctx, "tenant-receipt", accepted.DeliveryIDs[0])
	if err != nil || delivery.DeliveryStatus != contract.DeliveryDelivered || delivery.DispatchStatus != contract.DispatchAccepted {
		t.Fatalf("receipt did not promote delivery: (%+v,%v)", delivery, err)
	}
	if _, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "tenant-receipt", InstanceID: instanceID}, event, strings.Repeat("2", 64)); !isStoreError(err, StoreErrorConflict) {
		t.Fatalf("conflicting receipt should durably return 409 conflict: %v", err)
	}
	var conflicts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_receipt_conflicts WHERE event_id=$1`, event.EventID).Scan(&conflicts); err != nil || conflicts != 1 {
		t.Fatalf("receipt conflict evidence=(%d,%v), want one", conflicts, err)
	}
	contrary := contract.PluginReceiptV1{EventID: "provider-event-contrary", ProviderMessageID: event.ProviderMessageID, DeliveryID: &accepted.DeliveryIDs[0], Status: "failed", OccurredAt: occurred}
	if _, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "tenant-receipt", InstanceID: instanceID}, contrary, strings.Repeat("3", 64)); err != nil {
		t.Fatalf("contradictory receipt should preserve first terminal state as conflict evidence: %v", err)
	}
	delivery, err = store.GetDelivery(ctx, "tenant-receipt", accepted.DeliveryIDs[0])
	if err != nil || delivery.DeliveryStatus != contract.DeliveryDelivered {
		t.Fatalf("later failed receipt downgraded delivered terminal state: (%+v,%v)", delivery, err)
	}
}

func TestDirectDeliveryReceiptCanSupersedeUnfinishedProviderResponse(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-receipt-direct")
	accepted, err := store.AcceptSubmission(ctx, "tenant-receipt-direct", "direct-receipt-submit-01", strings.Repeat("a", 64), fixtureSubmission(instanceID, "direct@example.test"))
	if err != nil || len(accepted.DeliveryIDs) != 1 {
		t.Fatalf("accept submission=(%+v,%v)", accepted, err)
	}
	claims, err := store.ClaimDeliveries(ctx, 1, time.Minute)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim delivery=(%d,%v)", len(claims), err)
	}
	providerID := "provider-response-lost"
	deliveryID := accepted.DeliveryIDs[0]
	event := contract.PluginReceiptV1{EventID: "direct-receipt-event-01", ProviderMessageID: providerID, DeliveryID: &deliveryID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano)}
	ack, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "tenant-receipt-direct", InstanceID: instanceID}, event, strings.Repeat("b", 64))
	if err != nil || !ack.Accepted {
		t.Fatalf("direct receipt=(%+v,%v)", ack, err)
	}
	if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: "tenant-receipt-direct", DeliveryID: deliveryID, AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken, Result: contract.PluginSendResultV1{DeliveryID: deliveryID, Outcome: "unknown"}, FinishedAt: time.Now().UTC()}); !isStoreError(err, StoreErrorStaleClaim) {
		t.Fatalf("late response after receipt should be fenced: %v", err)
	}
	delivery, err := store.GetDelivery(ctx, "tenant-receipt-direct", deliveryID)
	if err != nil || delivery.DeliveryStatus != contract.DeliveryDelivered || delivery.DispatchStatus != contract.DispatchAccepted {
		t.Fatalf("receipt evidence lost after late response=(%+v,%v)", delivery, err)
	}
}

func TestFailedDeliveryCannotBePromotedByContraryReceipt(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-receipt-failed")
	accepted, err := store.AcceptSubmission(ctx, "tenant-receipt-failed", "failed-receipt-submit-01", strings.Repeat("c", 64), fixtureSubmission(instanceID, "failed@example.test"))
	if err != nil || len(accepted.DeliveryIDs) != 1 {
		t.Fatalf("accept submission=(%+v,%v)", accepted, err)
	}
	claims, err := store.ClaimDeliveries(ctx, 1, time.Minute)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim delivery=(%d,%v)", len(claims), err)
	}
	deliveryID := accepted.DeliveryIDs[0]
	errorInfo := &contract.PluginDeliveryErrorV1{Code: "permanent_rejection"}
	result := contract.PluginSendResultV1{DeliveryID: deliveryID, Outcome: "rejected", Error: errorInfo}
	if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: "tenant-receipt-failed", DeliveryID: deliveryID, AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken, Result: result, FinishedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("finish permanent rejection: %v", err)
	}
	event := contract.PluginReceiptV1{EventID: "contrary-delivered-event", ProviderMessageID: "late-provider-id", DeliveryID: &deliveryID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if _, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "tenant-receipt-failed", InstanceID: instanceID}, event, strings.Repeat("d", 64)); err != nil {
		t.Fatalf("contrary receipt should be durably recorded as conflict: %v", err)
	}
	delivery, err := store.GetDelivery(ctx, "tenant-receipt-failed", deliveryID)
	if err != nil || delivery.DeliveryStatus != contract.DeliveryFailed || delivery.DispatchStatus != contract.DispatchFailed {
		t.Fatalf("contrary receipt promoted terminal failure=(%+v,%v)", delivery, err)
	}
}

func TestReceiptRequiresUniqueProviderMatchUnlessDeliveryIDIsSupplied(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-receipt-ambiguous")
	request := fixtureSubmission(instanceID, "ambiguous@example.test")
	request.Deliveries = append(request.Deliveries, request.Deliveries[0])
	accepted, err := store.AcceptSubmission(ctx, "tenant-receipt-ambiguous", "ambiguous-submit-001", strings.Repeat("e", 64), request)
	if err != nil || len(accepted.DeliveryIDs) != 2 {
		t.Fatalf("accept submission=(%+v,%v)", accepted, err)
	}
	providerID := "shared-provider-message-id"
	for i := 0; i < 2; i++ {
		claims, err := store.ClaimDeliveries(ctx, 1, time.Minute)
		if err != nil || len(claims) != 1 {
			t.Fatalf("claim %d=(%d,%v)", i, len(claims), err)
		}
		result := contract.PluginSendResultV1{DeliveryID: claims[0].DeliveryID, Outcome: "accepted", ProviderMessageID: &providerID, ReceiptExpected: true}
		if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: "tenant-receipt-ambiguous", DeliveryID: claims[0].DeliveryID, AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken, Result: result, FinishedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("finish accepted attempt %d: %v", i, err)
		}
	}
	withoutID := contract.PluginReceiptV1{EventID: "ambiguous-no-delivery-id", ProviderMessageID: providerID, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if _, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "tenant-receipt-ambiguous", InstanceID: instanceID}, withoutID, strings.Repeat("f", 64)); err != nil {
		t.Fatalf("store ambiguous receipt as conflict evidence: %v", err)
	}
	var state string
	if err := db.QueryRowContext(ctx, `SELECT processing_status FROM notification_receipt_inbox WHERE tenant_id=$1 AND event_id=$2`, "tenant-receipt-ambiguous", withoutID.EventID).Scan(&state); err != nil || state != "conflict" {
		t.Fatalf("ambiguous receipt state=(%q,%v), want conflict", state, err)
	}
	selected := accepted.DeliveryIDs[1]
	withID := contract.PluginReceiptV1{EventID: "ambiguous-with-delivery-id", ProviderMessageID: providerID, DeliveryID: &selected, Status: "delivered", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if _, err := store.AcceptReceipt(ctx, ReceiptPrincipal{TenantID: "tenant-receipt-ambiguous", InstanceID: instanceID}, withID, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("delivery-scoped receipt with duplicate provider ID: %v", err)
	}
	delivery, err := store.GetDelivery(ctx, "tenant-receipt-ambiguous", selected)
	if err != nil || delivery.DeliveryStatus != contract.DeliveryDelivered {
		t.Fatalf("explicit delivery receipt did not match unique delivery=(%+v,%v)", delivery, err)
	}
}

func TestSourceBlockedMissingProjectionAndExpiredEventAreDurable(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	event := contract.SourceEventV1{SchemaVersion: "v1", SourceDeploymentID: "deployment-fixture", SourceEventID: "event-missing-group", ActionID: "action-1", TenantID: "tenant-source", NotificationGroupID: "missing-group", GroupRevision: 9, OccurredAt: time.Now().UTC().Format(time.RFC3339), ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339)}
	plan := SourceAcceptancePlan{Event: event, BlockedReason: "blocked_group_unavailable"}
	accepted, err := store.AcceptSourcePlan(ctx, event.TenantID, "source-blocked-001", strings.Repeat("3", 64), plan)
	if err != nil || !accepted.Accepted || accepted.IntakeStatus != "blocked" || len(accepted.DeliveryIDs) != 0 {
		t.Fatalf("blocked source intake=(%+v,%v)", accepted, err)
	}
	var recordedGroup sql.NullString
	var fkGroup sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT source_group_id,group_id FROM notification_requests WHERE id=$1`, accepted.NotificationID).Scan(&recordedGroup, &fkGroup); err != nil || !recordedGroup.Valid || recordedGroup.String != "missing-group" || fkGroup.Valid {
		t.Fatalf("blocked diagnostic group values=(%+v,%+v,%v)", recordedGroup, fkGroup, err)
	}
	replayed, ok, err := store.FindSourceReplay(ctx, event.TenantID, "source-blocked-001", strings.Repeat("3", 64), event.SourceDeploymentID, event.SourceEventID, event.ActionID)
	if err != nil || !ok || replayed.NotificationID != accepted.NotificationID || replayed.IntakeStatus != "blocked" {
		t.Fatalf("blocked source replay=(%+v,%v,%v)", replayed, ok, err)
	}
	if _, _, err := store.FindSourceReplay(ctx, event.TenantID, "different-idempotency-key", strings.Repeat("4", 64), event.SourceDeploymentID, event.SourceEventID, event.ActionID); !isStoreError(err, StoreErrorConflict) {
		t.Fatalf("same source tuple with another digest error=%v, want conflict", err)
	}
	event.SourceEventID = "event-expired"
	event.ExpiresAt = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	expired, err := store.AcceptSourcePlan(ctx, event.TenantID, "source-expired-001", strings.Repeat("4", 64), SourceAcceptancePlan{Event: event})
	if err != nil || expired.IntakeStatus != "blocked" || expired.BlockedReason != "blocked_expired" || len(expired.DeliveryIDs) != 0 {
		t.Fatalf("expired source intake=(%+v,%v)", expired, err)
	}
}

func TestRetryRequiresExplicitRejectionAndExpiredLeaseBecomesUnknown(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-retry")
	request := fixtureSubmission(instanceID, "retry@example.test")
	accepted, err := store.AcceptSubmission(ctx, "tenant-retry", "retry-submit-0001", strings.Repeat("5", 64), request)
	if err != nil {
		t.Fatal("accept retry fixture")
	}
	for attemptNumber := 1; attemptNumber <= maxDeliveryAttempts; attemptNumber++ {
		if attemptNumber > 1 {
			if _, err := db.ExecContext(ctx, `UPDATE notification_deliveries SET next_attempt_at=NOW()-INTERVAL '1 second' WHERE tenant_id=$1 AND id=$2`, "tenant-retry", accepted.DeliveryIDs[0]); err != nil {
				t.Fatal("make scheduled retry due")
			}
		}
		claims, err := store.ClaimDeliveries(ctx, 1, time.Minute)
		if err != nil || len(claims) != 1 {
			t.Fatalf("claim attempt %d=(%d,%v)", attemptNumber, len(claims), err)
		}
		result := contract.PluginSendResultV1{DeliveryID: accepted.DeliveryIDs[0], Outcome: "rejected", Error: &contract.PluginDeliveryErrorV1{Code: "rate_limited", Retryable: true}}
		if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: "tenant-retry", DeliveryID: accepted.DeliveryIDs[0], AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken, Result: result, FinishedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("finish rejected attempt %d: %v", attemptNumber, err)
		}
	}
	view, err := store.GetDelivery(ctx, "tenant-retry", accepted.DeliveryIDs[0])
	if err != nil || view.DispatchStatus != contract.DispatchFailed || view.AttemptCount != maxDeliveryAttempts {
		t.Fatalf("third retryable rejection terminal state=(%+v,%v)", view, err)
	}

	unknownAccepted, err := store.AcceptSubmission(ctx, "tenant-retry", "unknown-submit-01", strings.Repeat("6", 64), fixtureSubmission(instanceID, "unknown@example.test"))
	if err != nil {
		t.Fatal("accept unknown fixture")
	}
	claims, err := store.ClaimDeliveries(ctx, 1, time.Minute)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim ambiguous send=(%d,%v)", len(claims), err)
	}
	unknown := contract.PluginSendResultV1{DeliveryID: unknownAccepted.DeliveryIDs[0], Outcome: "unknown"}
	if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: "tenant-retry", DeliveryID: unknownAccepted.DeliveryIDs[0], AttemptID: claims[0].AttemptID, LeaseToken: claims[0].LeaseToken, Result: unknown, FinishedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("finish ambiguous send: %v", err)
	}
	if claims, err := store.ClaimDeliveries(ctx, 10, time.Minute); err != nil || len(claims) != 0 {
		t.Fatalf("unknown outcome was automatically queued: (%d,%v)", len(claims), err)
	}
	unknownView, err := store.GetDelivery(ctx, "tenant-retry", unknownAccepted.DeliveryIDs[0])
	if err != nil || unknownView.DispatchStatus != contract.DispatchUnknown {
		t.Fatalf("unknown outcome status=(%+v,%v)", unknownView, err)
	}

	expiredAccepted, err := store.AcceptSubmission(ctx, "tenant-retry", "expired-submit-001", strings.Repeat("8", 64), fixtureSubmission(instanceID, "expired@example.test"))
	if err != nil {
		t.Fatal("accept expired-lease fixture")
	}
	expiredClaims, err := store.ClaimDeliveries(ctx, 1, time.Minute)
	if err != nil || len(expiredClaims) != 1 {
		t.Fatalf("claim expired-lease fixture=(%d,%v)", len(expiredClaims), err)
	}
	recovered, err := store.RecoverExpiredClaims(ctx, expiredClaims[0].ExpiresAt.Add(time.Second))
	if err != nil || recovered != 1 {
		t.Fatalf("recover expired attempt=(%d,%v)", recovered, err)
	}
	lateResult := contract.PluginSendResultV1{DeliveryID: expiredAccepted.DeliveryIDs[0], Outcome: "accepted"}
	if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: "tenant-retry", DeliveryID: expiredAccepted.DeliveryIDs[0], AttemptID: expiredClaims[0].AttemptID, LeaseToken: expiredClaims[0].LeaseToken, Result: lateResult, FinishedAt: time.Now().UTC()}); !isStoreError(err, StoreErrorStaleClaim) {
		t.Fatalf("late finish after lease recovery=%v, want stale claim", err)
	}
	expiredView, err := store.GetDelivery(ctx, "tenant-retry", expiredAccepted.DeliveryIDs[0])
	if err != nil || expiredView.DispatchStatus != contract.DispatchUnknown {
		t.Fatalf("expired in-flight send status=(%+v,%v)", expiredView, err)
	}
}

func TestClaimDecryptsOnlyWorkerPayloadAndRuntimeUsesPinnedRegistration(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-worker-payload")
	accepted, err := store.AcceptSubmission(ctx, "tenant-worker-payload", "worker-submit-0001", strings.Repeat("7", 64), fixtureSubmission(instanceID, "worker@example.test"))
	if err != nil {
		t.Fatal("accept worker fixture")
	}
	claims, err := store.ClaimDeliveries(ctx, 1, time.Minute)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim worker fixture=(%d,%v)", len(claims), err)
	}
	var config map[string]string
	if err := json.Unmarshal(claims[0].Request.Config, &config); err != nil || config["token"] != "fixture-instance-secret" {
		t.Fatalf("claim did not receive decrypted secret snapshot: (%+v,%v)", config, err)
	}
	runtime, err := store.ResolvePluginRuntime(ctx, "tenant-worker-payload", instanceID, claims[0].Request.ConfigVersion)
	if err != nil || runtime.RegistrationID == "" || runtime.RegistrationVersion != 1 || runtime.AuthSecret != "fixture-auth-value" || runtime.Origin != "https://fixture.example.test" {
		t.Fatalf("resolve pinned plugin runtime=(%+v,%v)", runtime, err)
	}
	if claims[0].Request.Recipient.Address != "worker@example.test" || claims[0].Request.Content.Text == nil || *claims[0].Request.Content.Text != "fixture notification body" {
		t.Fatalf("claim request missing protected payload: (%+v)", claims[0].Request)
	}
	_ = accepted
}
