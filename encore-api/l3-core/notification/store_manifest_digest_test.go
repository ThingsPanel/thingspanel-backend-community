package notification

import (
	"context"
	"encoding/json"
	"encore.app/l3-core/notification/contract"
	"strings"
	"testing"
)

func TestFormalManifestDigestStoresHashAndReplaysPrefixedWire(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	manifest := contract.PluginManifestV1{APIVersion: "1.0", PluginID: "digest-fixture", PluginVersion: "1.0.0", Name: "Digest fixture", Channels: []contract.Channel{"email"}, ContentModes: []string{"text"}, ConfigSchema: json.RawMessage(`{"type":"object"}`), RecipientSchema: json.RawMessage(`{"type":"object"}`), SecretFields: []string{}}
	request := contract.PluginRegistration{PluginID: manifest.PluginID, PluginVersion: manifest.PluginVersion, Origin: "https://fixture.example.test", AuthSecret: "fixture-token", ManifestDigest: "sha256:" + strings.Repeat("a", 64)}
	first, err := store.RegisterPlugin(ctx, "tenant-digest", "formal-digest-key", strings.Repeat("b", 64), request, manifest)
	if err != nil || first.ManifestDigest != request.ManifestDigest {
		t.Fatalf("formal digest failed native-compatible storage: %+v %v", first, err)
	}
	var stored string
	if err := db.QueryRowContext(ctx, `SELECT manifest_digest FROM notification_plugin_registrations WHERE id=$1`, first.ID).Scan(&stored); err != nil || stored != strings.Repeat("a", 64) {
		t.Fatal("digest storage mismatch")
	}
	repeat, err := store.RegisterPlugin(ctx, "tenant-digest", "formal-digest-key", strings.Repeat("b", 64), request, manifest)
	if err != nil || repeat.ID != first.ID || repeat.ManifestDigest != request.ManifestDigest {
		t.Fatal("formal digest replay mismatch")
	}
}
