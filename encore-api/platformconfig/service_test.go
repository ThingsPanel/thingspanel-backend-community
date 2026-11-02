package platformconfig

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
)

func TestDictionaryHostDefaultDisabledDoesNotReadConfiguration(t *testing.T) {
	t.Setenv("TP_PLATFORMCONFIG_CONFIG_FILE", "/does-not-exist/dictionary-config.json")
	t.Setenv("TP_PLATFORMCONFIG_AUTHORIZATION_TOKEN", "")
	t.Setenv("TP_AUTH_MODE", "passthrough")
	for _, owner := range []string{"", "disabled"} {
		t.Setenv("TP_PLATFORMCONFIG_OWNER", owner)
		service, err := initService()
		if err != nil || service == nil || service.database != nil || service.repository != nil || service.handler != nil {
			t.Fatalf("disabled service initialized a dependency: %#v %v", service, err)
		}
		for _, route := range []struct {
			method, path string
			handle       func(http.ResponseWriter, *http.Request)
		}{
			{http.MethodPost, "/api/v1/dict/column", service.CreateColumn},
			{http.MethodPost, "/api/v1/dict/language", service.CreateLanguage},
			{http.MethodGet, "/api/v1/dict/enum", service.Enum},
			{http.MethodGet, "/api/v1/dict", service.Page},
			{http.MethodGet, "/api/v1/dict/language/opaque", service.Languages},
			{http.MethodDelete, "/api/v1/dict/column/opaque", service.DeleteColumn},
			{http.MethodDelete, "/api/v1/dict/language/opaque", service.DeleteLanguage},
			{http.MethodGet, "/api/v1/dict/protocol/service", service.Protocols},
		} {
			w := httptest.NewRecorder()
			route.handle(w, httptest.NewRequest(route.method, route.path, nil))
			var envelope struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || w.Code != 503 || envelope.Code != 100001 {
				t.Fatalf("disabled route=%s status=%d body=%s", route.path, w.Code, w.Body.String())
			}
		}
		w := httptest.NewRecorder()
		service.Health(w, httptest.NewRequest(http.MethodGet, "/api/v1/platformconfig/health", nil))
		if w.Code != 503 || strings.Contains(w.Body.String(), "does-not-exist") {
			t.Fatal("disabled health response leaked configuration or reported ready")
		}
		service.Shutdown(nil)
	}
}

func TestDictionaryHostRejectsUnsafeOrAmbiguousConfiguration(t *testing.T) {
	t.Setenv("TP_PLATFORMCONFIG_OWNER", "encore")
	t.Setenv("TP_PLATFORMCONFIG_CONFIG_FILE", "/does-not-exist/dictionary-config.json")
	for _, mode := range []string{"", "dev", "passthrough", "unknown"} {
		t.Setenv("TP_AUTH_MODE", mode)
		if service, err := initService(); err == nil || service != nil {
			t.Fatal("unsafe authentication initialized the owner")
		}
	}
	t.Setenv("TP_PLATFORMCONFIG_OWNER", "legacy")
	if service, err := initService(); err == nil || service != nil {
		t.Fatal("unknown owner selection silently enabled a writer")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "owner.json")
	valid := `{"databaseURL":"postgres://fixture@127.0.0.1/existing","expectedDatabase":"existing","schema":"public","authorizationBaseURL":"http://127.0.0.1:9","messagesFile":"/existing/messages.yaml"}`
	for _, body := range []string{valid + `{}`, strings.Replace(valid, `"schema":"public"`, `"schema":"new_empty_schema"`, 1), strings.Replace(valid, `"messagesFile":"/existing/messages.yaml"`, `"messagesFile":"messages.yaml"`, 1), strings.TrimSuffix(valid, "}") + `,"fallbackDatabase":"empty"}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readConfig(path); err == nil {
			t.Fatal("accepted unsafe/ambiguous configuration")
		}
	}
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if config, err := readConfig(path); err != nil || config.ExpectedDatabase != "existing" || config.Schema != "public" {
		t.Fatalf("explicit existing database config: %#v %v", config, err)
	}
	if _, err := readConfig("relative.json"); err == nil {
		t.Fatal("accepted relative configuration path")
	}
}

func TestDictionaryHostPrincipalKeepsTrustedExactIdentity(t *testing.T) {
	for _, data := range []*authn.Data{nil, {}, {UserID: "subject"}, {TenantID: "tenant"}} {
		if _, err := principalForData(data); err == nil {
			t.Fatal("accepted absent identity")
		}
	}
	data := &authn.Data{UserID: "opaque-subject", TenantID: "opaque-tenant", Roles: []string{"platform_admin"}}
	principal, err := principalForData(data)
	if err != nil || principal.SubjectID != data.UserID || principal.TenantID != data.TenantID || len(principal.Roles) != 1 {
		t.Fatal("trusted identity was changed")
	}
	data.Roles[0] = "changed"
	if principal.Roles[0] != "platform_admin" {
		t.Fatal("principal retained mutable framework role storage")
	}
	for _, role := range []string{"SYS_ADMIN", "platform_admin"} {
		principal, err := principalForData(&authn.Data{UserID: "platform-user", Roles: []string{role}})
		if err != nil || principal.SubjectID != "platform-user" || principal.TenantID != "" {
			t.Fatal("existing global platform identity was rejected or rewritten")
		}
	}
	if _, err := principalForData(&authn.Data{UserID: "tenant-user", Roles: []string{"tenant_admin"}}); err == nil {
		t.Fatal("ordinary principal with absent tenant was accepted")
	}
}
