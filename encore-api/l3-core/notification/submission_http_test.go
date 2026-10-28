package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

type coreVerifierFixture struct {
	user      UserPrincipal
	source    SourcePrincipal
	userError error
	calls     int
}

func (v *coreVerifierFixture) VerifyUser(context.Context, string) (UserPrincipal, error) {
	v.calls++
	return v.user, v.userError
}
func (v *coreVerifierFixture) VerifySource(context.Context, string) (SourcePrincipal, error) {
	v.calls++
	return v.source, nil
}
func (v *coreVerifierFixture) VerifySourceProjection(context.Context, string) (SourceProjectionPrincipal, error) {
	return SourceProjectionPrincipal{}, errors.New("unused")
}
func (v *coreVerifierFixture) VerifyReceipt(context.Context, string, string) (ReceiptPrincipal, error) {
	return ReceiptPrincipal{}, errors.New("unused")
}

func TestNotificationHTTPAuthorizesBeforeParsingAndReturnsDurable202(t *testing.T) {
	store := &coreStoreSpy{replay: true}
	verifier := &coreVerifierFixture{user: coreTestPrincipal("notification:send")}
	handler := &NotificationHTTPHandler{Verifier: verifier, Ready: func() error { return nil }, Core: &Core{Store: store, ValidateTarget: func(context.Context, string, contract.SubmitDelivery) error {
		t.Fatal("replay must not revalidate")
		return nil
	}, Now: func() time.Time { return coreTestTime("2031-01-01T00:00:00Z") }}}
	wire, _ := json.Marshal(coreTestRequest())
	run := func(body []byte) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v2/notifications", bytes.NewReader(body))
		request.Header.Set("Idempotency-Key", "stable-key-1")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	verifier.userError = errors.New("expired JWT")
	if got := run([]byte("not json")); got.Code != 401 {
		t.Fatalf("body parsed before auth: %d %s", got.Code, got.Body.String())
	}
	verifier.userError = nil
	verifier.user = coreTestPrincipal("notification:read")
	if got := run([]byte("not json")); got.Code != 403 {
		t.Fatalf("read scope could submit: %d", got.Code)
	}
	verifier.user = coreTestPrincipal("notification:send")
	got := run(wire)
	var response contract.Envelope[contract.AcceptedData]
	if got.Code != 202 || json.Unmarshal(got.Body.Bytes(), &response) != nil || response.Code != 200 || response.Data.NotificationID != "original" || response.RequestID == "" || store.accepted != 0 {
		t.Fatalf("durable replay response wrong: %d %s", got.Code, got.Body.String())
	}
	if got.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("sensitive result could be cached")
	}
}

func TestNotificationHTTPPrivacyGateDoesNotAuthenticateOrReadBody(t *testing.T) {
	verifier := &coreVerifierFixture{}
	handler := &NotificationHTTPHandler{Verifier: verifier, Core: &Core{}, Ready: func() error { return ErrPrivacyGuardClosed }}
	request := httptest.NewRequest(http.MethodPost, "/api/v2/notifications", bytes.NewBufferString("credential-bearing-body"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 503 || verifier.calls != 0 {
		t.Fatal("privacy gate bypassed")
	}
}

func TestNotificationHTTPSourceBlockedIs202AndSourceScopeCannotReplaceTenant(t *testing.T) {
	store := &coreStoreSpy{}
	verifier := &coreVerifierFixture{source: SourcePrincipal{DeploymentID: "deployment-1", TenantIDs: []string{"tenant-1"}}}
	handler := &NotificationHTTPHandler{Verifier: verifier, Ready: func() error { return nil }, Core: &Core{Store: store, Planner: corePlannerFunc(func(_ context.Context, _ string, event contract.SourceEventV1) (SourceAcceptancePlan, error) {
		return SourceAcceptancePlan{Event: event, BlockedReason: "group_revision_unavailable"}, nil
	})}}
	event := contract.SourceEventV1{SchemaVersion: "1.0", SourceDeploymentID: "deployment-1", SourceEventID: "event-1", ActionID: "action-1", TenantID: "tenant-1", NotificationGroupID: "group-1", GroupRevision: 1, OccurredAt: "2030-01-01T00:00:00Z", ExpiresAt: "2030-01-02T00:00:00Z", Payload: contract.SourceEventPayload{Subject: "alarm", Text: "temperature high", Variables: map[string]json.RawMessage{}}}
	run := func() *httptest.ResponseRecorder {
		wire, _ := json.Marshal(event)
		request := httptest.NewRequest(http.MethodPost, "/api/v2/notification-ingress/events", bytes.NewReader(wire))
		request.Header.Set("Idempotency-Key", "stable-source-key")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	got := run()
	var response contract.Envelope[contract.AcceptedData]
	if got.Code != 202 || json.Unmarshal(got.Body.Bytes(), &response) != nil || response.Data.IntakeStatus != "blocked" || store.sourceAccepted != 1 {
		t.Fatalf("blocked event not durably accepted: %d %s", got.Code, got.Body.String())
	}
	event.TenantID = "other"
	if got := run(); got.Code != 403 || store.sourceAccepted != 1 {
		t.Fatal("source credential escaped tenant allowlist")
	}
}
