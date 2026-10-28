package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

func TestNotificationListFindsBlockedRequestsAndKeepsTenantScope(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	ctx := context.Background()
	for i, tenant := range []string{"tenant-query", "other-tenant"} {
		event := contract.SourceEventV1{SchemaVersion: "1.0", SourceDeploymentID: "deployment-query", SourceEventID: "blocked-event", ActionID: "action", TenantID: tenant, NotificationGroupID: "missing", GroupRevision: 1, OccurredAt: time.Now().UTC().Format(time.RFC3339), ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), Payload: contract.SourceEventPayload{Subject: "fixture", Text: "private fixture body", Variables: map[string]json.RawMessage{}}}
		digest, _ := NotificationIntentDigest(event)
		if _, err := store.AcceptSourcePlan(ctx, tenant, "blocked-event-key", digest, SourceAcceptancePlan{Event: event, BlockedReason: "group_revision_unavailable"}); err != nil {
			t.Fatalf("fixture %d: %v", i, err)
		}
	}
	instance := createFixtureInstance(t, store, "tenant-query")
	intent := fixtureSubmission(instance, "private@example.test")
	digest, _ := NotificationIntentDigest(intent)
	if _, err := store.AcceptSubmission(ctx, "tenant-query", "ready-event-key", digest, intent); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListNotifications(ctx, "tenant-query", NotificationListFilter{Page: 1, PageSize: 20, IntakeStatus: "blocked"})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].IntakeStatus != "blocked" || page.Items[0].Deliveries == nil || len(page.Items[0].Deliveries) != 0 {
		t.Fatalf("zero-target request missing: %+v %v", page, err)
	}
	handler := &NotificationHTTPHandler{Core: &Core{}, Store: store, Verifier: &coreVerifierFixture{user: UserPrincipal{UserID: "query-reader", TenantID: "tenant-query", Scopes: []string{"notification:read"}}}, Ready: func() error { return nil }}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/notifications?intakeStatus=blocked&sourceType=alarm&page=1&pageSize=20", nil))
	var envelope contract.Envelope[contract.NotificationListData]
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Data.Total != 1 {
		t.Fatalf("filtered HTTP list rejected: %d %s", response.Code, response.Body.String())
	}
	for _, query := range []string{"sourceType=bogus", "sourceType=", "intakeStatus=unknown", "intakeStatus="} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/notifications?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("invalid request-list enum %q returned %d: %s", query, response.Code, response.Body.String())
		}
	}
	deliveryHandler := &NotificationHTTPHandler{Core: &Core{}, Store: store, Verifier: &coreVerifierFixture{user: UserPrincipal{UserID: "query-reader", TenantID: "tenant-query", Scopes: []string{"notification:read"}}}, Ready: func() error { return nil }}
	for _, query := range []string{"sourceType=bogus", "sourceType=", "dispatchStatus=unknownish", "dispatchStatus=", "deliveryStatus=sent", "deliveryStatus="} {
		response := httptest.NewRecorder()
		deliveryHandler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/notification-deliveries?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("invalid delivery-list enum %q returned %d: %s", query, response.Code, response.Body.String())
		}
	}
	all, err := store.ListNotifications(ctx, "tenant-query", NotificationListFilter{Page: 1, PageSize: 20})
	if err != nil || all.Total != 2 || len(all.Items) != 2 {
		t.Fatalf("tenant count/page escaped: %+v %v", all, err)
	}
	wire, _ := json.Marshal(all)
	if strings.Contains(string(wire), "private@example.test") || strings.Contains(string(wire), "private fixture body") {
		t.Fatal("query exposed private payload")
	}
	next, err := store.ListNotifications(ctx, "tenant-query", NotificationListFilter{Page: 3, PageSize: 1})
	if err != nil || next.Total != 2 || next.Items == nil || len(next.Items) != 0 {
		t.Fatal("empty page or count inconsistent")
	}
}

func TestDeliveryHTTPPaginationAllowsFrozenFilters(t *testing.T) {
	filter, err := notificationDeliveryFilter(httptest.NewRequest(http.MethodGet, "/api/v2/notification-deliveries?instanceId=instance-query&sourceType=alarm&dispatchStatus=unknown&deliveryStatus=unknown&page=2&pageSize=10", nil))
	if err != nil || filter.Page != 2 || filter.PageSize != 10 || filter.InstanceID != "instance-query" || filter.DispatchStatus != "unknown" {
		t.Fatal("valid filters rejected", err)
	}
	for _, query := range []string{"tenantId=other", "page=1&page=2", "sourceType=alarm&sourceType=test", "sourceType=bogus", "sourceType=", "dispatchStatus=unknownish", "dispatchStatus=", "deliveryStatus=sent", "deliveryStatus="} {
		if _, err := notificationDeliveryFilter(httptest.NewRequest(http.MethodGet, "/?"+query, nil)); err == nil {
			t.Fatal("invalid filter admitted", query)
		}
	}
}
