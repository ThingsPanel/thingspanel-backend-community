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

func TestDictionaryAuthorizationContractAndEcho(t *testing.T) {
	principal := app.Principal{SubjectID: "subject", TenantID: "tenant", Roles: []string{"platform_admin"}}
	token := strings.Repeat("fixture-dictionary-", 3)
	for _, operation := range []app.Operation{app.CreateColumn, app.CreateLanguage, app.ReadEnum, app.ReadPage, app.ReadLanguages, app.DeleteColumn, app.DeleteLanguage, app.ReadProtocols} {
		t.Run(string(operation), func(t *testing.T) {
			for _, change := range []string{"valid", "version", "subject", "tenant", "operation", "resource", "denied", "unavailable", "redirect", "malformed", "extra-field"} {
				t.Run(change, func(t *testing.T) {
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						if r.Method != http.MethodPost || r.URL.Path != Path || r.Header.Get("X-TP-Service-Token") != token || r.Header.Get("Authorization") != "" {
							t.Error("incorrect trusted transport")
						}
						var input Request
						if json.NewDecoder(r.Body).Decode(&input) != nil || input.ProtocolVersion != 1 || input.SubjectID != principal.SubjectID || input.TenantID != principal.TenantID || input.Operation != operation {
							t.Error("trusted identity/action changed")
						}
						result := Response{ProtocolVersion: 1, Authorized: true, TenantID: input.TenantID, SubjectID: input.SubjectID, Operation: input.Operation, Resource: Resource(operation)}
						switch change {
						case "version":
							result.ProtocolVersion = 2
						case "subject":
							result.SubjectID = "foreign"
						case "tenant":
							result.TenantID = "foreign"
						case "operation":
							result.Operation = "unregistered"
						case "resource":
							result.Resource = "api/v1/dict/blanket"
						case "denied":
							w.WriteHeader(403)
							return
						case "unavailable":
							w.WriteHeader(503)
							return
						case "redirect":
							w.Header().Set("Location", "/another-owner")
							w.WriteHeader(302)
							return
						case "malformed":
							_, _ = w.Write([]byte(`{"authorized":`))
							return
						case "extra-field":
							_, _ = w.Write([]byte(`{"protocolVersion":1,"authorized":true,"tenantId":"tenant","subjectId":"subject","operation":"read-enum","resource":"api/v1/dict/enum","role":"admin"}`))
							return
						}
						_ = json.NewEncoder(w).Encode(result)
					}))
					defer server.Close()
					client, err := New(server.URL, token)
					if err != nil {
						t.Fatal(err)
					}
					err = client.Authorize(context.Background(), principal, operation)
					if change == "valid" {
						if err != nil {
							t.Fatal(err)
						}
					} else {
						want := app.ErrUnauthorized
						if change == "unavailable" || change == "redirect" || change == "malformed" || change == "extra-field" {
							want = app.ErrUnavailable
						}
						if !errors.Is(err, want) {
							t.Fatalf("error=%v expected=%v", err, want)
						}
					}
					if calls.Load() != 1 {
						t.Fatalf("owner calls=%d; no redirect/retry permitted", calls.Load())
					}
				})
			}
		})
	}
}

func TestDictionaryAuthorizationConfigurationFailsClosed(t *testing.T) {
	for _, base := range []string{"", "file:///tmp/identity", "http://user:password@localhost", "http://localhost?override=1", "http://localhost/another-owner", "http://localhost/#fragment"} {
		if _, err := New(base, strings.Repeat("x", 32)); err == nil {
			t.Fatalf("accepted base %q", base)
		}
	}
	if _, err := New("http://127.0.0.1:1", "short"); err == nil {
		t.Fatal("accepted short service token")
	}
	client, err := New("http://127.0.0.1:1", strings.Repeat("x", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []app.Operation{"", "api/v1/dict", "unregistered"} {
		if err := client.Authorize(context.Background(), app.Principal{SubjectID: "s", TenantID: "t"}, operation); !errors.Is(err, app.ErrUnavailable) {
			t.Fatalf("unknown operation reached transport: %v", err)
		}
	}
}
