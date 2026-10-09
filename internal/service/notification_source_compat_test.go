package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"project/internal/model"
)

func emailSnapshotFixture() sourceGroupSnapshot {
	return sourceGroupSnapshot{
		SourceDeploymentID: "deployment-a",
		TenantID:           "tenant-a",
		Group: sourceSnapshotGroup{ID: "native-a", Enabled: true, Revision: 7, Bindings: []sourceSnapshotBinding{
			{BindingID: "binding-a", InstanceID: "instance-a", RecipientSource: json.RawMessage(`{"kind":"literal","recipient":{"kind":"email","address":"ops@example.test"}}`), ContentBinding: json.RawMessage(`{"kind":"text","title":"{{subject}}","text":"{{text}}\n\n---\nThis email was sent by ThingsPanel"}`)},
			{BindingID: "binding-b", InstanceID: "instance-a", RecipientSource: json.RawMessage(`{"kind":"literal","recipient":{"kind":"email","address":"user@example.test"}}`), ContentBinding: json.RawMessage(`{"kind":"text","title":"{{subject}}","text":"{{text}}\n\n---\nThis email was sent by ThingsPanel"}`)},
		}},
		Instances: []sourceSnapshotInstance{{ID: "instance-a", PluginRegistrationID: "plugin-reg-a", Channel: "email", Enabled: true, Config: map[string]json.RawMessage{
			"host": json.RawMessage(`"smtp.example.test"`), "port": json.RawMessage(`587`), "from_email": json.RawMessage(`"sender@example.test"`), "ssl": json.RawMessage(`false`),
		}, SecretState: map[string]bool{"from_password": true}, ProviderIdentity: map[string]string{}, ConfigVersion: 2}},
		Plugins: []sourceSnapshotPlugin{{ID: "plugin-reg-a", PluginID: sourceSMTPPluginID, PluginVersion: "1.0.0", Enabled: true}},
	}
}

func legacyEmailGroupFixture() *model.NotificationGroup {
	config := `{"EMAIL":" user@EXAMPLE.test,ops@example.test "}`
	return &model.NotificationGroup{ID: "legacy-a", TenantID: "tenant-a", Status: "OPEN", NotificationType: model.NoticeType_Email, NotificationConfig: &config}
}

func legacyEmailAccountFixture() model.EmailConfig {
	ssl := false
	return model.EmailConfig{Host: "smtp.example.test", Port: 587, FromEmail: "sender@example.test", FromPassword: "fixture-secret", SSL: &ssl}
}

func TestEmailSourceCompatibilityFetchesExactTenantRevisionAndMatchesRecipientSet(t *testing.T) {
	snapshot := emailSnapshotFixture()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != sourceGroupSnapshotPath+"native-a" {
			t.Errorf("unexpected private snapshot request target: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("tenantId") != "tenant-a" || r.URL.Query().Get("groupRevision") != "7" {
			t.Errorf("snapshot request omitted exact scope/revision: %q", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer projection-fixture-token" {
			t.Error("snapshot request did not use projection-purpose credential")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sourceGroupSnapshotEnvelope{Code: 200, Message: "ok", RequestID: "request-a", Data: snapshot})
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	checker := &EmailSourceCompatibilityChecker{
		fetchSnapshot: func(ctx context.Context, request SourceGroupProjectionRequest) (sourceGroupSnapshot, error) {
			return fetchSourceGroupSnapshot(ctx, client, base, "projection-fixture-token", request)
		},
		legacyConfig: func(context.Context) (model.EmailConfig, bool, error) { return legacyEmailAccountFixture(), true, nil },
	}
	request := SourceGroupProjectionRequest{SourceDeploymentID: "deployment-a", TenantID: "tenant-a", LegacyGroupID: "legacy-a", NotificationGroupID: "native-a", GroupRevision: 7}
	if err := checker.CheckEncoreCompatibility(context.Background(), legacyEmailGroupFixture(), request); err != nil {
		t.Fatalf("complete email projection rejected: %v", err)
	}
}

func TestEmailSourceCompatibilityRejectsWholeGroupOnAnyMismatch(t *testing.T) {
	baseSnapshot := emailSnapshotFixture()
	group := legacyEmailGroupFixture()
	request := SourceGroupProjectionRequest{SourceDeploymentID: "deployment-a", TenantID: "tenant-a", LegacyGroupID: "legacy-a", NotificationGroupID: "native-a", GroupRevision: 7}
	tests := []struct {
		name     string
		group    *model.NotificationGroup
		request  SourceGroupProjectionRequest
		snapshot sourceGroupSnapshot
	}{
		{name: "member group stays legacy", group: func() *model.NotificationGroup {
			g := legacyEmailGroupFixture()
			g.NotificationType = model.NoticeType_Member
			return g
		}(), request: request, snapshot: baseSnapshot},
		{name: "duplicate legacy recipient stays legacy", group: func() *model.NotificationGroup {
			g := legacyEmailGroupFixture()
			config := `{"EMAIL":"ops@example.test,ops@EXAMPLE.test"}`
			g.NotificationConfig = &config
			return g
		}(), request: request, snapshot: baseSnapshot},
		{name: "wrong revision", group: group, request: func() SourceGroupProjectionRequest { r := request; r.GroupRevision = 8; return r }(), snapshot: baseSnapshot},
		{name: "missing recipient", group: group, request: request, snapshot: func() sourceGroupSnapshot { s := baseSnapshot; s.Group.Bindings = s.Group.Bindings[:1]; return s }()},
		{name: "extra target", group: group, request: request, snapshot: func() sourceGroupSnapshot {
			s := baseSnapshot
			s.Instances = append(s.Instances, sourceSnapshotInstance{ID: "instance-extra", PluginRegistrationID: "plugin-extra", Channel: "email", Enabled: true})
			s.Plugins = append(s.Plugins, sourceSnapshotPlugin{ID: "plugin-extra", PluginID: sourceSMTPPluginID, PluginVersion: "1.0.0", Enabled: true})
			return s
		}()},
		{name: "content mismatch", group: group, request: request, snapshot: func() sourceGroupSnapshot {
			s := baseSnapshot
			s.Group.Bindings[0].ContentBinding = json.RawMessage(`{"kind":"text","title":"wrong","text":"{{text}}\n\n---\nThis email was sent by ThingsPanel"}`)
			return s
		}()},
		{name: "instance secret absent", group: group, request: request, snapshot: func() sourceGroupSnapshot {
			s := baseSnapshot
			s.Instances[0].SecretState["from_password"] = false
			return s
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fetchCalls := 0
			checker := &EmailSourceCompatibilityChecker{
				fetchSnapshot: func(context.Context, SourceGroupProjectionRequest) (sourceGroupSnapshot, error) {
					fetchCalls++
					return test.snapshot, nil
				},
				legacyConfig: func(context.Context) (model.EmailConfig, bool, error) { return legacyEmailAccountFixture(), true, nil },
			}
			err := checker.CheckEncoreCompatibility(context.Background(), test.group, test.request)
			if err == nil {
				t.Fatal("incompatible projection accepted")
			}
			if (test.name == "member group stays legacy" || test.name == "duplicate legacy recipient stays legacy") && fetchCalls != 0 {
				t.Fatal("ineligible legacy group triggered private projection lookup")
			}
		})
	}
}

func TestEmailSourceSnapshotRejectsRedirectAndOversizedBody(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://other.example.test/", http.StatusFound)
		}))
		defer server.Close()
		client := server.Client()
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		base, _ := url.Parse(server.URL)
		_, err := fetchSourceGroupSnapshot(context.Background(), client, base, "fixture-token", SourceGroupProjectionRequest{TenantID: "tenant-a", NotificationGroupID: "native-a", GroupRevision: 1})
		if err == nil {
			t.Fatal("redirect response accepted")
		}
	})
	t.Run("oversized", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, strings.Repeat("x", sourceSnapshotMaxBody+1))
		}))
		defer server.Close()
		client := server.Client()
		base, _ := url.Parse(server.URL)
		_, err := fetchSourceGroupSnapshot(context.Background(), client, base, "fixture-token", SourceGroupProjectionRequest{TenantID: "tenant-a", NotificationGroupID: "native-a", GroupRevision: 1})
		if err == nil {
			t.Fatal("oversized snapshot accepted")
		}
	})
}

func TestPlanLegacyEmailImportProducesDisabledSecretFreeBody(t *testing.T) {
	plan, err := PlanLegacyEmailGroupImport(legacyEmailGroupFixture(), "tenant-a", "Imported EMAIL", VerifiedSMTPImportTarget{ID: "instance-a", TenantID: "tenant-a", PluginID: sourceSMTPPluginID, Channel: "email", Enabled: true})
	if err != nil {
		t.Fatalf("plan import: %v", err)
	}
	if plan.Enabled || len(plan.Bindings) != 2 || plan.Bindings[0].ContentBinding["title"] != "{{subject}}" || plan.Bindings[0].ContentBinding["text"] != legacyEmailTextTemplate {
		t.Fatal("import plan did not preserve the closed, exact email mapping")
	}
	const oldSendBody = "Alarm body\n\n---\nThis email was sent by ThingsPanel"
	gotRenderedBody := strings.ReplaceAll(plan.Bindings[0].ContentBinding["text"], "{{text}}", "Alarm body")
	if gotRenderedBody != oldSendBody {
		t.Fatalf("planned body differs from old EMAIL output: got %q want %q", gotRenderedBody, oldSendBody)
	}
	encoded, err := json.Marshal(plan)
	if err != nil || strings.Contains(string(encoded), "fixture-secret") || strings.Contains(string(encoded), "from_password") {
		t.Fatal("import plan exposed credential material")
	}
	if _, err := PlanLegacyEmailGroupImport(legacyEmailGroupFixture(), "tenant-b", "Bad tenant", VerifiedSMTPImportTarget{ID: "instance-a", TenantID: "tenant-a", PluginID: sourceSMTPPluginID, Channel: "email", Enabled: true}); err == nil {
		t.Fatal("cross-tenant import target accepted")
	}
}
