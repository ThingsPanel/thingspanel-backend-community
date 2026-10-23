package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"encore.app/l3-core/notification/contract"
)

func TestPluginAndInstanceManagementPersistenceAndReplay(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	manifest := contract.PluginManifestV1{
		APIVersion:      "1.0",
		PluginID:        "smtp-fixture",
		Name:            "SMTP fixture",
		PluginVersion:   "1.0.0",
		IdentityFields:  []string{"account"},
		Channels:        []contract.Channel{contract.ChannelEmail},
		ContentModes:    []string{"text"},
		ConfigSchema:    json.RawMessage(`{"type":"object"}`),
		RecipientSchema: json.RawMessage(`{"type":"object"}`),
		SecretFields:    []string{"password"},
	}
	registration := contract.PluginRegistration{
		PluginID:       manifest.PluginID,
		PluginVersion:  manifest.PluginVersion,
		Origin:         "https://plugin.example.test",
		AuthSecret:     "fixture-plugin-auth-sentinel",
		ManifestDigest: strings.Repeat("a", 64),
	}

	plugin, err := store.RegisterPlugin(ctx, "tenant-a", "register-key-001", strings.Repeat("1", 64), registration, manifest)
	if err != nil {
		t.Fatalf("register plugin: %v", err)
	}
	grantFixturePlugin(t, store, "tenant-a", plugin.ID)
	replayedPlugin, err := store.RegisterPlugin(ctx, "tenant-a", "register-key-001", strings.Repeat("1", 64), registration, manifest)
	if err != nil || replayedPlugin.ID != plugin.ID || replayedPlugin.Version != plugin.Version {
		t.Fatalf("same registration replay = (%+v, %v), want original id/version", replayedPlugin, err)
	}
	if _, err := store.GetPlugin(ctx, "tenant-b", plugin.ID); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("cross-tenant plugin read error = %v, want not found", err)
	}
	otherTenantPlugins, err := store.ListPlugins(ctx, "tenant-b")
	if err != nil || len(otherTenantPlugins) != 0 {
		t.Fatalf("cross-tenant plugin list = (%d, %v), want empty", len(otherTenantPlugins), err)
	}

	updated, err := store.UpdatePlugin(ctx, "tenant-a", plugin.ID, "plugin-disable-001", strings.Repeat("2", 64), contract.PluginUpdate{ExpectedVersion: 1, Enabled: false})
	if err != nil || updated.Enabled || updated.Version != 2 {
		t.Fatalf("disable plugin = (%+v, %v), want disabled version 2", updated, err)
	}
	if _, err := store.UpdatePlugin(ctx, "tenant-a", plugin.ID, "plugin-enable-001", strings.Repeat("3", 64), contract.PluginUpdate{ExpectedVersion: 2, Enabled: true}); err != nil {
		t.Fatalf("re-enable plugin: %v", err)
	}
	updatedReplay, err := store.UpdatePlugin(ctx, "tenant-a", plugin.ID, "plugin-disable-001", strings.Repeat("2", 64), contract.PluginUpdate{ExpectedVersion: 1, Enabled: false})
	if err != nil || updatedReplay.Version != 2 || updatedReplay.Enabled {
		t.Fatalf("old plugin update replay = (%+v, %v), want original version 2 disabled", updatedReplay, err)
	}
	registrationReplay, replayed, err := store.FindPluginRegistrationReplay(ctx, "tenant-a", "register-key-001", strings.Repeat("1", 64))
	if err != nil || !replayed || registrationReplay.Version != 1 || !registrationReplay.Enabled {
		t.Fatalf("plugin registration replay drifted to current state: (%+v,%v,%v)", registrationReplay, replayed, err)
	}

	instanceRequest := contract.InstanceCreate{
		PluginRegistrationID: plugin.ID,
		Name:                 "SMTP account",
		Channel:              contract.ChannelEmail,
		Config:               json.RawMessage(`{"host":"smtp.example.test","port":587}`),
		ProviderIdentity:     map[string]string{"account": "fixture-account"},
	}
	instance, err := store.CreateInstance(ctx, "tenant-a", "instance-create-001", strings.Repeat("4", 64), instanceRequest, map[string]string{"password": "fixture-instance-password-sentinel"})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	instanceReplay, err := store.CreateInstance(ctx, "tenant-a", "instance-create-001", strings.Repeat("4", 64), instanceRequest, map[string]string{"password": "fixture-instance-password-sentinel"})
	if err != nil || instanceReplay.ID != instance.ID || instanceReplay.ConfigVersion != instance.ConfigVersion {
		t.Fatalf("same instance create replay = (%+v, %v), want original id/config version", instanceReplay, err)
	}
	if !instance.SecretState["password"] || instance.Enabled {
		t.Fatalf("create view leaked or lost state: enabled=%v secretState=%v", instance.Enabled, instance.SecretState)
	}
	if _, hasSecret := instance.Config["password"]; hasSecret {
		t.Fatal("instance view returned a secret config field")
	}
	if _, err := store.CreateInstance(ctx, "tenant-b", "instance-create-002", strings.Repeat("5", 64), instanceRequest, nil); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("cross-tenant instance create error = %v, want missing grant", err)
	}

	patch := json.RawMessage(`{"host":"smtp-next.example.test"}`)
	update := contract.InstanceUpdate{
		ExpectedVersion: 1,
		ConfigPatch:     patch,
		Secrets:         &contract.SecretPatch{Set: map[string]string{"password": "rotated-password-sentinel"}},
		Enabled:         boolPtr(true),
	}
	instanceV2, err := store.UpdateInstance(ctx, "tenant-a", instance.ID, "instance-update-001", strings.Repeat("6", 64), update)
	if err != nil || instanceV2.Version != 2 || instanceV2.ConfigVersion != 2 || !instanceV2.Enabled {
		t.Fatalf("update instance = (%+v, %v), want enabled version 2", instanceV2, err)
	}
	instanceV2Replay, err := store.UpdateInstance(ctx, "tenant-a", instance.ID, "instance-update-001", strings.Repeat("6", 64), update)
	if err != nil || instanceV2Replay.Version != 2 || string(instanceV2Replay.Config["host"]) != `"smtp-next.example.test"` {
		t.Fatalf("instance update replay = (%+v, %v), want original immutable snapshot", instanceV2Replay, err)
	}
	if _, err := store.UpdateInstance(ctx, "tenant-a", instance.ID, "instance-update-conflict", strings.Repeat("7", 64), contract.InstanceUpdate{ExpectedVersion: 1, Enabled: boolPtr(false)}); !isStoreError(err, StoreErrorVersionConflict) {
		t.Fatalf("stale expectedVersion error = %v, want version conflict", err)
	}
	createdReplay, replayed, err := store.FindInstanceReplay(ctx, "tenant-a", instance.ID, "instance.create", "instance-create-001", strings.Repeat("4", 64))
	if err != nil || !replayed || createdReplay.Version != 1 || createdReplay.Enabled {
		t.Fatalf("instance create replay drifted to current state: (%+v,%v,%v)", createdReplay, replayed, err)
	}

	var authCipher, secretCipher []byte
	if err := db.QueryRowContext(ctx, `SELECT auth_ciphertext FROM notification_plugin_registrations WHERE id = $1`, plugin.ID).Scan(&authCipher); err != nil {
		t.Fatal("read protected plugin credential")
	}
	if err := db.QueryRowContext(ctx, `SELECT ciphertext FROM notification_instance_secrets WHERE tenant_id = $1 AND instance_id = $2 AND config_version = 1 AND slot = 'password'`, "tenant-a", instance.ID).Scan(&secretCipher); err != nil {
		t.Fatal("read protected instance credential")
	}
	if bytes.Contains(authCipher, []byte(registration.AuthSecret)) || bytes.Contains(secretCipher, []byte("fixture-instance-password-sentinel")) {
		t.Fatal("plaintext credential was stored in PostgreSQL")
	}
}

func boolPtr(value bool) *bool { return &value }

func isStoreError(err error, kind StoreErrorKind) bool {
	var storeError *StoreError
	return errors.As(err, &storeError) && storeError.Kind == kind
}
