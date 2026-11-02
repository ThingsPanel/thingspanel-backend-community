package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"project/internal/model"
)

// This is the Community consumer's HTTP contract test, not a ThingsVis database
// acceptance test. Every mismatched resolver echo must stop before import/query.
func TestMarketInstallThingsVisIdentityEchoContract(t *testing.T) {
	for _, execute := range []bool{false, true} {
		mode := "query"
		if execute {
			mode = "execute"
		}
		for _, changed := range []string{"", "externalTenantId", "externalSubjectId", "operationKey", "payloadHash", "missing_echoes", "missing_project_receipt"} {
			t.Run(mode+"/"+changed, func(t *testing.T) {
				req := model.MarketInstallStepRequest{
					ProtocolVersion:       1,
					MarketInstallIdentity: model.MarketInstallIdentity{TenantID: "external-tenant", SubjectID: "external-subject", AuthorizationRef: "grant"},
					OperationID:           "00000000-0000-0000-0000-000000000001", Generation: 1,
					StepID: "dashboard:import", IdempotencyKey: strings.Repeat("a", 64), PayloadHash: strings.Repeat("b", 64),
					Step: model.MarketInstallStep{ID: "dashboard:import", Owner: "thingsvis", Action: "import", ResourceKey: "dashboard", Payload: json.RawMessage(`{"dashboard":{"name":"fixture","schemaVersion":"thingsvis-1","canvasConfig":{},"nodes":[],"dataSources":[],"variables":[]},"deviceBindings":[],"deviceProofs":{}}`)},
				}
				var imports atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.URL.Path == "/api/internal/market-dashboards/import/v1/identity" {
						var got model.MarketInstallStepRequest
						if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got.TenantID != req.TenantID || got.SubjectID != req.SubjectID || got.IdempotencyKey != req.IdempotencyKey || got.PayloadHash != req.PayloadHash || r.Header.Get("X-TP-Service-Token") != strings.Repeat("i", 32) {
							t.Error("resolver did not receive the original authorized step")
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						identity := map[string]string{"protocolVersion": "thingsvis-import-receipt-v1", "state": "confirmed", "tenantId": "local-tenant", "userId": "local-user", "externalTenantId": req.TenantID, "externalSubjectId": req.SubjectID, "operationKey": req.IdempotencyKey, "payloadHash": req.PayloadHash}
						if changed == "missing_echoes" {
							for _, key := range []string{"externalTenantId", "externalSubjectId", "operationKey", "payloadHash"} {
								delete(identity, key)
							}
						} else if changed != "" && changed != "missing_project_receipt" {
							identity[changed] = "wrong-original-scope"
						}
						_ = json.NewEncoder(w).Encode(identity)
						return
					}
					if r.URL.Path != "/api/internal/market-dashboards/import/v1" {
						t.Error("unexpected provider endpoint")
						w.WriteHeader(http.StatusNotFound)
						return
					}
					imports.Add(1)
					if r.Header.Get("X-Tenant-ID") != "local-tenant" || r.Header.Get("X-User-ID") != "local-user" || r.Header.Get("X-Internal-Token") != strings.Repeat("m", 32) {
						t.Error("local identity headers were not mapped exactly")
					}
					if execute {
						var input struct {
							OperationKey string `json:"operationKey"`
							PayloadHash  string `json:"payloadHash"`
							InstanceKey  string `json:"instanceKey"`
						}
						if r.Method != http.MethodPost {
							t.Error("execute did not use POST")
						}
						if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.OperationKey != req.IdempotencyKey || input.PayloadHash != req.PayloadHash || input.InstanceKey != req.OperationID {
							t.Error("execute changed original operation identity")
						}
						w.WriteHeader(http.StatusCreated)
					} else {
						if r.Method != http.MethodGet || r.URL.Query().Get("operationKey") != req.IdempotencyKey || r.URL.Query().Get("payloadHash") != req.PayloadHash {
							t.Error("query did not preserve original key/hash")
						}
					}
					projectID := "original-project"
					if changed == "missing_project_receipt" {
						projectID = ""
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"protocolVersion": "thingsvis-import-receipt-v1", "state": "confirmed", "operationKey": req.IdempotencyKey, "payloadHash": req.PayloadHash, "instanceKey": req.OperationID, "resourceKey": req.Step.ResourceKey, "receiptId": "original-receipt", "dashboardId": "original-dashboard", "projectId": projectID})
				}))
				t.Cleanup(server.Close)
				t.Setenv("TP_MARKETINSTALL_THINGSVIS_BASE_URL", server.URL)
				t.Setenv("TP_MARKETINSTALL_THINGSVIS_IDENTITY_TOKEN", strings.Repeat("i", 32))
				t.Setenv("TP_MARKETINSTALL_THINGSVIS_TOKEN", strings.Repeat("m", 32))
				observed, err := NewMarketInstallProvider().dashboardStep(context.Background(), req, execute)
				if err != nil {
					t.Fatal(err)
				}
				if changed == "" {
					if observed.State != "confirmed" || observed.Receipt == nil || observed.Receipt.ResourceID != "original-dashboard" || imports.Load() != 1 {
						t.Fatalf("valid identity failed: %+v calls=%d", observed, imports.Load())
					}
				} else if changed == "missing_project_receipt" {
					if observed.State != "unknown" || observed.ErrorCode != "THINGSVIS_RECEIPT_INVALID" || observed.Receipt != nil || imports.Load() != 1 {
						t.Fatalf("incomplete receipt accepted: %+v", observed)
					}
				} else if observed.State != "unknown" || observed.ErrorCode != "THINGSVIS_IDENTITY_INVALID" || observed.Receipt != nil || imports.Load() != 0 {
					t.Fatalf("mismatched identity reached importer: %+v calls=%d", observed, imports.Load())
				}
			})
		}
	}
}
