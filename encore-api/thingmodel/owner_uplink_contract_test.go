package thingmodel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOwnerUplinkContractOnlyPinnedPublication(t *testing.T) {
	const token = "fixture-owner-token-at-least-32-characters"
	t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", token)
	t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "deployment")
	const body = `{"model_id":"m","snapshot_id":"s","revision":7}`
	fixture := app.Publication{Snapshot: core.Snapshot{ID: "s", ModelID: "m", TenantID: core.TenantID(canonicalIdentity("tenant", "a")), Revision: 7, Name: "published", Fields: []core.Field{{Kind: core.FieldTelemetry, Identifier: "temperature", Name: core.I18nString{Default: "T"}, Access: core.Access{Read: true}, ValueType: &core.ValueType{Kind: core.ScalarNumber}}}}}
	for _, tc := range []struct {
		name, auth, tenant, deployment, body string
		status                               int
	}{
		{"valid", "Bearer " + token, "a", "deployment", body, 200},
		{"anonymous", "", "a", "deployment", body, 401},
		{"browser", "Bearer user-token", "a", "deployment", body, 401},
		{"deployment", "Bearer " + token, "a", "wrong", body, 401},
		{"foreign", "Bearer " + token, "b", "deployment", body, 404},
		{"snapshot", "Bearer " + token, "a", "deployment", strings.Replace(body, `"s"`, `"other"`, 1), 409},
		{"draft_field", "Bearer " + token, "a", "deployment", strings.TrimSuffix(body, "}") + `,"draft":true}`, 400},
		{"trailing", "Bearer " + token, "a", "deployment", body + `{}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			read := func(ctx context.Context, tenant, model string, revision uint64) (app.Publication, error) {
				reads++
				if tenant != "a" {
					return app.Publication{}, app.ErrNotFound
				}
				if model != "m" || revision != 7 {
					t.Fatal("unpinned lookup")
				}
				return fixture, nil
			}
			r := httptest.NewRequest("POST", "/internal/thingmodel/uplink-contract", strings.NewReader(tc.body))
			r.Header.Set("Authorization", tc.auth)
			r.Header.Set("X-Automation-Tenant", tc.tenant)
			r.Header.Set("X-Automation-Deployment", tc.deployment)
			w := httptest.NewRecorder()
			handleOwnerUplinkContract(w, r, read)
			if w.Code != tc.status {
				t.Fatalf("status=%d want%d body=%s", w.Code, tc.status, w.Body)
			}
			if tc.status == 401 && reads != 0 {
				t.Fatal("unauthorized publication read")
			}
			if tc.status == 200 {
				var out map[string]json.RawMessage
				if json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out) != 7 {
					t.Fatal("unexpected response fields")
				}
				if out["resources"] != nil || out["draft"] != nil || out["device_id"] != nil {
					t.Fatal("mutable/private owner data exposed")
				}
				h := sha256.Sum256(out["model"])
				var checksum string
				_ = json.Unmarshal(out["contract_sha256"], &checksum)
				if checksum != hex.EncodeToString(h[:]) || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("invalid immutable contract checksum")
				}
			}
		})
	}
}

func TestPostgresOwnerUplinkContractUsesPublishedSnapshotAndTenant(t *testing.T) {
	const token = "fixture-owner-token-at-least-32-characters"
	t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", token)
	t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "deployment")
	tenant := "q04-uplink-" + uuid.NewString()
	p := core.Principal{SubjectID: uuid.NewString(), TenantID: core.TenantID(canonicalIdentity("tenant", tenant)), Capabilities: []string{string(core.CapabilityRead), string(core.CapabilityWrite), string(core.CapabilityPublish)}}
	svc := application()
	ctx := context.Background()
	created, err := svc.Create(ctx, p, uuid.NewString(), core.Model{Name: "Uplink contract fixture", Fields: []core.Field{{Kind: core.FieldTelemetry, Identifier: "  Vendor.Temp-Celsius/°C  ", Name: core.I18nString{Default: "T"}, Access: core.Access{Read: true}, ValueType: &core.ValueType{Kind: core.ScalarNumber}}, {Kind: core.FieldCommand, Identifier: "  Vendor.Temp-Celsius/°C  ", Name: core.I18nString{Default: "Set"}, Access: core.Access{Write: true}, Parameters: &core.Parameters{SchemaVersion: 1, Items: []core.Parameter{}}}}})
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(ctx, p, uuid.NewString(), core.ModelID(created.ModelID), created.DraftRevision, "uplink fixture")
	if err != nil {
		t.Fatal(err)
	}
	version, err := svc.Version(ctx, p, core.ModelID(created.ModelID), *published.PublishedRevision)
	if err != nil {
		t.Fatal(err)
	}
	input := ownerUplinkContractRequest{created.ModelID, string(version.Snapshot.ID), version.Snapshot.Revision}
	raw, _ := json.Marshal(input)
	for _, current := range []string{tenant, "other-" + tenant} {
		r := httptest.NewRequest("POST", "/internal/thingmodel/uplink-contract", strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Automation-Deployment", "deployment")
		r.Header.Set("X-Automation-Tenant", current)
		w := httptest.NewRecorder()
		(&Service{}).OwnerUplinkContract(w, r)
		want := 200
		if current != tenant {
			want = 404
		}
		if w.Code != want {
			t.Fatalf("status%d want%d", w.Code, want)
		}
		if want == 200 {
			var out struct {
				Model ModelDraft `json:"model"`
			}
			if json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Model.Fields) != 2 || out.Model.Fields[0].Identifier != out.Model.Fields[1].Identifier || out.Model.Fields[0].Kind == out.Model.Fields[1].Kind {
				t.Fatal("published family-scoped identifiers lost")
			}
		}
	}
}
