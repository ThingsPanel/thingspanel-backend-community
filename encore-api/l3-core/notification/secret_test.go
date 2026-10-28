package notification

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivacyGuardRequiresOriginalMatchingInfraEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(path, []byte(`{"log_config":"trace"}`), 0600); err != nil {
		t.Fatal(err)
	}
	good := func() []byte {
		return []byte("ENCORE_INFRA_CONFIG_PATH=" + path + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + path + "\x00")
	}
	guard, err := newPrivacyGuard(func() ([]byte, error) { return good(), nil }, "linux")
	if err != nil {
		t.Fatalf("safe infra path rejected: %v", err)
	}
	if err := guard.Check(); err != nil {
		t.Fatalf("guard check failed: %v", err)
	}

	for name, env := range map[string][]byte{
		"mirror mismatch":  []byte("ENCORE_INFRA_CONFIG_PATH=" + path + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=/tmp/other\x00"),
		"runtime override": []byte("ENCORE_INFRA_CONFIG_PATH=" + path + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + path + "\x00ENCORE_RUNTIME_CONFIG=override\x00"),
		"duplicate infra":  []byte("ENCORE_INFRA_CONFIG_PATH=" + path + "\x00ENCORE_INFRA_CONFIG_PATH=" + path + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + path + "\x00"),
		"trace override":   []byte("ENCORE_INFRA_CONFIG_PATH=" + path + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + path + "\x00OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://trace.invalid\x00"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux"); err == nil {
				t.Fatal("unsafe environment was accepted")
			}
		})
	}
	if _, err := newPrivacyGuard(func() ([]byte, error) { return good(), nil }, "darwin"); err == nil {
		t.Fatal("non-Linux runtime was accepted")
	}
}

func TestPrivacyGuardRejectsTraceConfigAndChangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "infra.json")
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + path + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + path + "\x00")
	if err := os.WriteFile(path, []byte(`{"trace_endpoint":"https://trace.invalid","auth_keys":["fixture"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux"); err == nil {
		t.Fatal("trace-enabled infra config accepted")
	}
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	guard, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatalf("safe config rejected: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"updated":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(); err == nil {
		t.Fatal("changed infra config was accepted")
	}
}

func TestAESGCMProtectorBindsAADAndDoesNotStorePlaintext(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	protector, err := NewAESGCMProtector("fixture-key", map[string][]byte{"fixture-key": key})
	if err != nil {
		t.Fatal(err)
	}
	aad := SnapshotAAD{ScopeKind: "tenant", TenantID: "tenant-a", ResourceID: "instance-1", Version: 2, Purpose: "instance-secret", Slot: "smtpPassword"}
	plain := []byte("fixture-secret-sentinel")
	sealed, err := protector.Seal(context.Background(), aad, plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed.Ciphertext), string(plain)) {
		t.Fatal("ciphertext contains plaintext")
	}
	opened, err := protector.Open(context.Background(), aad, sealed)
	if err != nil || string(opened) != string(plain) {
		t.Fatal("snapshot did not open with original AAD")
	}
	changed := aad
	changed.Slot = "otherSecret"
	if _, err := protector.Open(context.Background(), changed, sealed); err == nil {
		t.Fatal("snapshot opened with a different slot")
	}
}
