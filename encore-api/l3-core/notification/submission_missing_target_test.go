package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

func TestHTTPSubmitMissingSecondTargetRejectsWholeIntentWith422(t *testing.T) {
	store, db := newMigratedTestStore(t)
	tenant := "tenant-1"
	instance := createFixtureInstance(t, store, tenant)
	validator := &TargetValidator{Instances: store, Plugins: store}
	h := &NotificationHTTPHandler{Verifier: &coreVerifierFixture{user: coreTestPrincipal("notification:send", "notification:manage", "notification:send_test")}, Store: store, Ready: func() error { return nil }, Core: &Core{Store: store, ValidateTarget: validator.Validate}}
	intent := fixtureSubmission(instance, "fixture@example.test")
	invalid := intent.Deliveries[0]
	invalid.InstanceID = "missing-target-owned-fixture"
	intent.Deliveries = append(intent.Deliveries, invalid)
	before := map[string]int{}
	for _, table := range []string{"notification_requests", "notification_deliveries", "notification_idempotency"} {
		var n int
		if err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal("read fixture baseline count")
		}
		before[table] = n
	}
	wire, _ := json.Marshal(intent)
	request := httptest.NewRequest(http.MethodPost, "/api/v2/notifications", bytes.NewReader(wire))
	request.Header.Set("Idempotency-Key", "missing-second-target-key")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 422 {
		t.Fatalf("ordinary intent target rejection=%d, want 422", response.Code)
	}
	if strings.Contains(response.Body.String(), invalid.InstanceID) {
		t.Fatal("target rejection disclosed resource identity")
	}
	for _, table := range []string{"notification_requests", "notification_deliveries", "notification_idempotency"} {
		var n int
		if err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != before[table] {
			t.Fatalf("rejected whole intent left %s rows=%d err=%v", table, n, err)
		}
	}
	// The direct resource test endpoint retains masked not-found semantics.
	testWire, _ := json.Marshal(contract.TestSendRequest{Recipient: intent.Deliveries[0].Recipient, Content: intent.Deliveries[0].Content, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/notification-instances/"+invalid.InstanceID+"/test", bytes.NewReader(testWire))
	req.Header.Set("Idempotency-Key", "missing-direct-test-key")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 404 {
		t.Fatalf("direct instance test not-found=%d, want 404", out.Code)
	}
}
