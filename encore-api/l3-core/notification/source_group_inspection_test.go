package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encore.app/l3-core/notification/contract"
)

func TestSourceGroupInspectionIsScopedAndOmitsSecrets(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	instanceID := createFixtureInstance(t, store, "tenant-inspection")
	group, err := store.CreateGroup(context.Background(), "tenant-inspection", "inspection-group-key", strings.Repeat("a", 64), contract.GroupCreate{Name: "fixture", Enabled: true, Bindings: []contract.GroupBinding{{BindingID: "binding", InstanceID: instanceID, RecipientSource: json.RawMessage(`{"kind":"literal","recipient":{"kind":"email","address":"fixture@example.test"}}`), ContentBinding: json.RawMessage(`{"kind":"text","title":"{{subject}}","text":"{{text}}"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	principal := SourceProjectionPrincipal{DeploymentID: "deployment-inspection", TenantIDs: []string{"tenant-inspection"}}
	handler := &NotificationHTTPHandler{Store: store, Verifier: groupHTTPTestVerifier{projectionPrincipal: principal}}
	run := func(query string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/notification-source/group-snapshots/"+group.ID+query, nil)
		if !handler.serveSourceGroupInspection(recorder, request) {
			t.Fatal("private route unhandled")
		}
		return recorder
	}
	response := run("?tenantId=tenant-inspection&groupRevision=1")
	var envelope contract.Envelope[sourceGroupInspection]
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || len(envelope.Data.Instances) != 1 || envelope.Data.SourceDeploymentID != principal.DeploymentID {
		t.Fatalf("inspection failed %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "fixture-instance-secret") || strings.Contains(response.Body.String(), "fixture-auth-value") || strings.Contains(response.Body.String(), "authSecret") {
		t.Fatal("private snapshot leaked credentials")
	}
	if response := run("?tenantId=other-tenant&groupRevision=1"); response.Code != 403 {
		t.Fatal("tenant scope not enforced", response.Code)
	}
	for _, query := range []string{"?tenantId=tenant-inspection&groupRevision=1&token=bad", "?tenantId=tenant-inspection&tenantId=other&groupRevision=1"} {
		if response := run(query); response.Code != 400 {
			t.Fatal("strict query failed", response.Code)
		}
	}
	handler.Verifier = groupHTTPTestVerifier{projectionErr: ErrIdentityUnauthorized}
	if response := run("?tenantId=tenant-inspection&groupRevision=1"); response.Code != 401 {
		t.Fatal("source purpose not enforced")
	}
}
