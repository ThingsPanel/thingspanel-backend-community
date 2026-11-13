package thingmodel

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"

	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
)

func TestPostgresOwnerCommandValidationUsesCommunityTenantMapping(t *testing.T) {
	const token = "test-owner-token-32-characters-long"
	t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", token)
	t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "deployment")
	rawTenant := "community-short-" + uuid.NewString()
	p := core.Principal{SubjectID: uuid.NewString(), TenantID: core.TenantID(canonicalIdentity("tenant", rawTenant)), Capabilities: []string{string(core.CapabilityRead), string(core.CapabilityWrite), string(core.CapabilityPublish)}}
	ctx := context.Background()
	svc := application()
	created, err := svc.Create(ctx, p, uuid.NewString(), core.Model{Name: "Owner command fixture", Fields: []core.Field{{Kind: core.FieldCommand, Identifier: "set", Name: core.I18nString{Default: "Set"}, Access: core.Access{Write: true}, Parameters: &core.Parameters{SchemaVersion: 1, Items: []core.Parameter{{Identifier: "value", Name: core.I18nString{Default: "Value"}, ValueType: core.ValueType{Kind: core.ScalarBool}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(ctx, p, uuid.NewString(), core.ModelID(created.ModelID), created.DraftRevision, "owner validation")
	if err != nil {
		t.Fatal(err)
	}
	version, err := svc.Version(ctx, p, core.ModelID(created.ModelID), *published.PublishedRevision)
	if err != nil {
		t.Fatal(err)
	}
	input := ownerCommandValidationRequest{DeviceID: uuid.NewString(), ConfigID: uuid.NewString(), ModelID: created.ModelID, SnapshotID: string(version.Snapshot.ID), Revision: int(*published.PublishedRevision), BindingRevision: 1, Identifier: "set", Params: json.RawMessage(`{"value":true}`)}
	for _, tc := range []struct {
		tenant string
		params json.RawMessage
		status int
	}{{rawTenant, json.RawMessage(`{"value":true}`), 200}, {rawTenant, json.RawMessage(`{}`), 400}, {"other-" + rawTenant, json.RawMessage(`{"value":true}`), 404}} {
		input.Params = tc.params
		body, _ := json.Marshal(input)
		r := httptest.NewRequest("POST", "/internal/thingmodel/validate-command", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Automation-Deployment", "deployment")
		r.Header.Set("X-Automation-Tenant", tc.tenant)
		w := httptest.NewRecorder()
		(&Service{}).OwnerValidateCommand(w, r)
		if w.Code != tc.status {
			t.Fatalf("status=%d wanted=%d body=%s", w.Code, tc.status, w.Body)
		}
	}
}

func TestOwnerCommandValidationIdentityPinsAndResultOnly(t *testing.T) {
	const token = "test-owner-token-32-characters-long"
	t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", token)
	t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "deployment")
	const body = `{"device_id":"device","config_id":"config","model_id":"model","snapshot_id":"snapshot","revision":7,"binding_revision":3,"identifier":"set","params":{"value":true}}`
	fixture := app.Publication{Snapshot: core.Snapshot{ID: "snapshot", ModelID: "model", TenantID: core.TenantID(canonicalIdentity("tenant", "tenant-a")), Revision: 7, Fields: []core.Field{{Kind: core.FieldCommand, Identifier: "set", Access: core.Access{Write: true}, Parameters: &core.Parameters{SchemaVersion: 1, Items: []core.Parameter{{Identifier: "value", ValueType: core.ValueType{Kind: core.ScalarBool}}}}}}}}
	for _, tc := range []struct {
		name, auth, tenant, deployment, body string
		want                                 int
	}{
		{"valid", "Bearer " + token, "tenant-a", "deployment", body, 200},
		{"anonymous", "", "tenant-a", "deployment", body, 401},
		{"user bearer", "Bearer user-session", "tenant-a", "deployment", body, 401},
		{"wrong deployment", "Bearer " + token, "tenant-a", "other", body, 401},
		{"missing tenant", "Bearer " + token, "", "deployment", body, 400},
		{"wrong tenant", "Bearer " + token, "tenant-b", "deployment", body, 404},
		{"wrong pin", "Bearer " + token, "tenant-a", "deployment", strings.Replace(body, `"snapshot"`, `"other"`, 1), 409},
		{"wrong value", "Bearer " + token, "tenant-a", "deployment", strings.Replace(body, `true`, `"true"`, 1), 400},
		{"trailing json", "Bearer " + token, "tenant-a", "deployment", body + ` {}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			read := func(ctx context.Context, tenant, model string, revision uint64) (app.Publication, error) {
				calls++
				if tenant != "tenant-a" {
					return app.Publication{}, app.ErrNotFound
				}
				if model != "model" || revision != 7 {
					t.Fatal("unpinned lookup")
				}
				return fixture, nil
			}
			r := httptest.NewRequest("POST", "/internal/thingmodel/validate-command", strings.NewReader(tc.body))
			r.Header.Set("Authorization", tc.auth)
			r.Header.Set("X-Automation-Deployment", tc.deployment)
			r.Header.Set("X-Automation-Tenant", tc.tenant)
			w := httptest.NewRecorder()
			handleOwnerCommandValidation(w, r, read)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tc.want == 401 && calls != 0 {
				t.Fatal("unauthorized caller read snapshot")
			}
			if tc.want == 200 {
				var out map[string]any
				if json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out) != 4 || out["valid"] != true || out["device_id"] != "device" || out["snapshot_id"] != "snapshot" || out["binding_revision"] != float64(3) {
					t.Fatalf("unexpected response %s", w.Body)
				}
			}
		})
	}
}
