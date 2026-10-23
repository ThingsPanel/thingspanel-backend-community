package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"encore.app/l3-core/notification/contract"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A rolled-back serialization failure must stay retryable at the intake layer.
// Reporting 409 here would cause the legacy outbox to permanently discard it.
func TestSubmissionStorageFailureReturns503AndSameKeyCanRetry(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instance := createFixtureInstance(t, store, "tenant-1")
	intent := fixtureSubmission(instance, "fixture@example.test")
	wire, _ := json.Marshal(intent)
	h := &NotificationHTTPHandler{Verifier: &coreVerifierFixture{user: coreTestPrincipal("notification:send")}, Store: store, Ready: func() error { return nil }, Core: &Core{Store: store, ValidateTarget: func(context.Context, string, contract.SubmitDelivery) error { return nil }}}
	if _, err := db.ExecContext(ctx, `CREATE FUNCTION fail_fixture_request() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture serialization' USING ERRCODE='40001'; END $$; CREATE TRIGGER fail_fixture_request BEFORE INSERT ON notification_requests FOR EACH ROW EXECUTE FUNCTION fail_fixture_request()`); err != nil {
		t.Fatal(err)
	}
	run := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/notifications", bytes.NewReader(wire))
		req.Header.Set("Idempotency-Key", "same-stable-intent-key")
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		return out
	}
	if out := run(); out.Code != 503 {
		t.Fatalf("transient failure classified permanently: %d %s", out.Code, out.Body.String())
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_requests`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed intake acknowledged or persisted")
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER fail_fixture_request ON notification_requests`); err != nil {
		t.Fatal(err)
	}
	first := run()
	second := run()
	var a, b contract.Envelope[contract.AcceptedData]
	if first.Code != 202 || second.Code != 202 || json.Unmarshal(first.Body.Bytes(), &a) != nil || json.Unmarshal(second.Body.Bytes(), &b) != nil || a.Data.NotificationID == "" || a.Data.NotificationID != b.Data.NotificationID {
		t.Fatalf("stable retry did not resolve to one intent: %d/%d", first.Code, second.Code)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_requests`).Scan(&count); err != nil || count != 1 {
		t.Fatal("retry created duplicate intent")
	}
}
