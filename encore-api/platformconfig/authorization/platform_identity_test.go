package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"encore.app/platformconfig/app"
)

func TestDictionaryAuthorizationEmptyPlatformTenantRequiresExplicitEcho(t *testing.T) {
	principal := app.Principal{SubjectID: "platform-subject", Roles: []string{"platform_admin"}}
	token := strings.Repeat("dictionary-token-", 3)
	for _, change := range []string{"valid", "missing", "null", "foreign", "revoked"} {
		t.Run(change, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request Request
				if r.Header.Get("X-TP-Service-Token") != token || json.NewDecoder(r.Body).Decode(&request) != nil || request.SubjectID != principal.SubjectID || request.TenantID != "" || request.Operation != app.CreateColumn {
					t.Error("platform identity was changed")
				}
				if change == "revoked" {
					w.WriteHeader(403)
					return
				}
				response := map[string]any{"protocolVersion": 1, "authorized": true, "subjectId": principal.SubjectID, "tenantId": "", "operation": app.CreateColumn, "resource": Resource(app.CreateColumn)}
				switch change {
				case "missing":
					delete(response, "tenantId")
				case "null":
					response["tenantId"] = nil
				case "foreign":
					response["tenantId"] = "tenant"
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			client, err := New(server.URL, token)
			if err != nil {
				t.Fatal(err)
			}
			err = client.Authorize(context.Background(), principal, app.CreateColumn)
			if change == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				want := app.ErrUnauthorized
				if change == "missing" || change == "null" {
					want = app.ErrUnavailable
				}
				if !errors.Is(err, want) {
					t.Fatalf("error=%v expected=%v", err, want)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("authorization retried")
			}
			if err := client.Authorize(context.Background(), app.Principal{SubjectID: principal.SubjectID, Roles: []string{"TENANT_ADMIN"}}, app.ReadEnum); !errors.Is(err, app.ErrUnavailable) || calls.Load() != 1 {
				t.Fatal("empty tenant ordinary principal reached owner")
			}
		})
	}
}
