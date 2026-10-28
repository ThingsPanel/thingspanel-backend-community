package notification

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"encore.app/l3-core/notification/contract"
)

type defaultSMTPPluginRepositoryFixture struct {
	items        []contract.PluginView
	registers    int
	registration contract.PluginRegistration
}

func (r *defaultSMTPPluginRepositoryFixture) RegisterPlatformPlugin(_ context.Context, _, _, _ string, request contract.PluginRegistration, manifest contract.PluginManifestV1) (contract.PluginView, error) {
	r.registers++
	r.registration = request
	view := contract.PluginView{PluginID: request.PluginID, PluginVersion: request.PluginVersion, Manifest: contract.PluginManifestSummary{PluginID: manifest.PluginID}}
	r.items = append(r.items, view)
	return view, nil
}
func (r *defaultSMTPPluginRepositoryFixture) FindPlatformPluginRegistrationReplay(context.Context, string, string, string) (contract.PluginView, bool, error) {
	return contract.PluginView{}, false, nil
}
func (r *defaultSMTPPluginRepositoryFixture) GetPlatformPlugin(context.Context, string) (contract.PluginView, error) {
	return contract.PluginView{}, ErrManagementNotFound
}
func (r *defaultSMTPPluginRepositoryFixture) ListPlatformPlugins(context.Context) ([]contract.PluginView, error) {
	return append([]contract.PluginView(nil), r.items...), nil
}
func (r *defaultSMTPPluginRepositoryFixture) UpdatePlatformPlugin(context.Context, string, string, string, string, contract.PluginUpdate) (contract.PluginView, error) {
	return contract.PluginView{}, ErrManagementInvalid
}

type defaultSMTPManifestFixture struct {
	fetches  int
	raw      []byte
	manifest contract.PluginManifestV1
}

func (f *defaultSMTPManifestFixture) FetchManifest(context.Context, string, string) ([]byte, contract.PluginManifestV1, error) {
	f.fetches++
	return append([]byte(nil), f.raw...), f.manifest, nil
}
func (*defaultSMTPManifestFixture) ValidateInstance(context.Context, PluginRuntime, contract.PluginValidateRequestV1) (contract.PluginValidateResultV1, error) {
	return contract.PluginValidateResultV1{Valid: true}, nil
}

func TestDefaultSMTPBootstrapIsOptInAndNeverReplacesExistingRegistration(t *testing.T) {
	config, err := defaultSMTPBootstrapConfigFromEnv(func(string) string { return "" })
	if err != nil || config.enabled {
		t.Fatalf("absent bootstrap configuration should be disabled: cfg=%+v err=%v", config, err)
	}
	for _, values := range []map[string]string{
		{"NOTIFICATION_DEFAULT_SMTP_BOOTSTRAP": "yes"},
		{"NOTIFICATION_DEFAULT_SMTP_BOOTSTRAP": "true", "NOTIFICATION_DEFAULT_SMTP_PLUGIN_ORIGIN": "http://smtp.example.test", "NOTIFICATION_DEFAULT_SMTP_PLUGIN_TOKEN": "fixture-token"},
		{"NOTIFICATION_DEFAULT_SMTP_BOOTSTRAP": "true", "NOTIFICATION_DEFAULT_SMTP_PLUGIN_ORIGIN": "https://smtp.example.test/path", "NOTIFICATION_DEFAULT_SMTP_PLUGIN_TOKEN": "fixture-token"},
	} {
		if _, err := defaultSMTPBootstrapConfigFromEnv(func(name string) string { return values[name] }); err == nil {
			t.Fatalf("unsafe bootstrap configuration was accepted: keys=%v", values)
		}
	}

	manifest := contract.PluginManifestV1{
		APIVersion: "1.0", PluginID: defaultSMTPPluginID, Name: "SMTP", PluginVersion: "1.0.0",
		Channels: []contract.Channel{contract.ChannelEmail}, ContentModes: []string{"text"},
		ConfigSchema: json.RawMessage(`{"type":"object","properties":{"from_password":{"type":"string"}}}`), RecipientSchema: json.RawMessage(`{"type":"object"}`), SecretFields: []string{"from_password"},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal("encode manifest fixture")
	}
	privacyPath := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(privacyPath, []byte(`{"log_config":"info"}`), 0600); err != nil {
		t.Fatal("write privacy fixture")
	}
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + privacyPath + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + privacyPath + "\x00")
	privacy, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatal("construct privacy fixture")
	}

	client := &defaultSMTPManifestFixture{raw: raw, manifest: manifest}
	registry := &defaultSMTPPluginRepositoryFixture{items: []contract.PluginView{{PluginID: defaultSMTPPluginID, PluginVersion: "0.9.0"}}}
	service := &ManagementService{platformPlugins: registry, pluginHTTP: client, privacy: privacy}
	if err := service.BootstrapDefaultSMTP(context.Background(), defaultSMTPBootstrapConfig{enabled: true, origin: "https://smtp.example.test", token: "new-secret"}); err != nil {
		t.Fatalf("existing SMTP registration should be preserved: %v", err)
	}
	if client.fetches != 0 || registry.registers != 0 {
		t.Fatalf("existing registration caused network or writes: fetches=%d registrations=%d", client.fetches, registry.registers)
	}

	registry.items = nil
	if err := service.BootstrapDefaultSMTP(context.Background(), defaultSMTPBootstrapConfig{enabled: true, origin: "https://smtp.example.test", token: "fixture-token"}); err != nil {
		t.Fatalf("bootstrap missing SMTP registration: %v", err)
	}
	if registry.registers != 1 || client.fetches != 1 || registry.registration.PluginID != defaultSMTPPluginID || registry.registration.AuthSecret != "fixture-token" {
		t.Fatalf("bootstrap did not perform exactly one fixed registration: writes=%d fetches=%d plugin=%q", registry.registers, client.fetches, registry.registration.PluginID)
	}
}

func TestDefaultSMTPBootstrapPersistsOnceAndPreservesCredential(t *testing.T) {
	store, db := newMigratedTestStore(t)
	manifest := contract.PluginManifestV1{
		APIVersion: "1.0", PluginID: defaultSMTPPluginID, Name: "SMTP", PluginVersion: "1.0.1",
		Channels: []contract.Channel{contract.ChannelEmail}, ContentModes: []string{"text"},
		ConfigSchema:    json.RawMessage(`{"type":"object","properties":{"from_password":{"type":"string"}}}`),
		RecipientSchema: json.RawMessage(`{"type":"object"}`), SecretFields: []string{"from_password"},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	infraPath := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(infraPath, []byte(`{"log_config":"info"}`), 0600); err != nil {
		t.Fatal(err)
	}
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00")
	privacy, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatal(err)
	}
	client := &defaultSMTPManifestFixture{raw: raw, manifest: manifest}
	service := &ManagementService{platformPlugins: store, pluginHTTP: client, privacy: privacy}
	cfg := defaultSMTPBootstrapConfig{enabled: true, origin: "https://smtp.example.test", token: "fixture-bootstrap-credential"}
	if err := service.BootstrapDefaultSMTP(context.Background(), cfg); err != nil {
		t.Fatalf("persist bootstrap: %v", err)
	}
	items, err := store.ListPlatformPlugins(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one persisted plugin: count=%d err=%v", len(items), err)
	}
	var ciphertext []byte
	if err := db.QueryRow(`SELECT auth_ciphertext FROM notification_plugin_registrations WHERE id = $1`, items[0].ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	cfg.token = "fixture-replacement-must-not-be-written"
	if err := service.BootstrapDefaultSMTP(context.Background(), cfg); err != nil {
		t.Fatalf("repeat bootstrap: %v", err)
	}
	var after []byte
	if err := db.QueryRow(`SELECT auth_ciphertext FROM notification_plugin_registrations WHERE id = $1`, items[0].ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if string(ciphertext) != string(after) || client.fetches != 1 {
		t.Fatal("repeat bootstrap replaced credential or refetched manifest")
	}
	for _, table := range []string{"notification_plugin_registrations", "notification_plugin_grants", "notification_instances", "notification_requests", "notification_deliveries", "notification_delivery_attempts"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		want := 0
		if table == "notification_plugin_registrations" {
			want = 1
		}
		if count != want {
			t.Fatalf("bootstrap object count for %s: got %d want %d", table, count, want)
		}
	}
}
