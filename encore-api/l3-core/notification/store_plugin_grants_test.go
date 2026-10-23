package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"encore.app/l3-core/notification/contract"
)

func TestPluginGrantInitialCreateSerializesConcurrentWriters(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	manifest := contract.PluginManifestV1{
		APIVersion: "1.0", PluginID: "grant-concurrency", Name: "Grant concurrency", PluginVersion: "1.0.0",
		Channels: []contract.Channel{contract.ChannelEmail}, ContentModes: []string{"text"},
		ConfigSchema: json.RawMessage(`{"type":"object"}`), RecipientSchema: json.RawMessage(`{"type":"object"}`), SecretFields: []string{},
	}
	plugin, err := store.RegisterPlugin(ctx, "platform-admin", "grant-concurrency-register", strings.Repeat("a", 64), contract.PluginRegistration{
		PluginID: manifest.PluginID, PluginVersion: manifest.PluginVersion, Origin: "https://grant-concurrency.example.test", AuthSecret: "fixture-auth", ManifestDigest: strings.Repeat("b", 64),
	}, manifest)
	if err != nil {
		t.Fatalf("register global plugin: %v", err)
	}

	const writers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	var success, versionConflict, otherErrors int
	var resultMu sync.Mutex
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			key := fmt.Sprintf("grant-concurrent-%02d", index)
			_, _, err := store.SetPluginGrant(ctx, "tenant-concurrent", plugin.ID, key, strings.Repeat(fmt.Sprintf("%x", index+1), 64), contract.PluginGrantUpdate{
				PluginRegistrationID: plugin.ID, TenantID: "tenant-concurrent", Enabled: true, ExpectedVersion: 0,
			})
			resultMu.Lock()
			defer resultMu.Unlock()
			switch {
			case err == nil:
				success++
			case isStoreError(err, StoreErrorVersionConflict):
				versionConflict++
			default:
				otherErrors++
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if success != 1 || versionConflict != writers-1 || otherErrors != 0 {
		t.Fatalf("concurrent first grant: success=%d versionConflict=%d other=%d", success, versionConflict, otherErrors)
	}
	var version int64
	var historyCount int
	if err := db.QueryRowContext(ctx, `SELECT version FROM notification_plugin_grants WHERE tenant_id=$1 AND registration_id=$2`, "tenant-concurrent", plugin.ID).Scan(&version); err != nil || version != 1 {
		t.Fatalf("current grant version=%d err=%v", version, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_plugin_grant_states WHERE tenant_id=$1 AND registration_id=$2`, "tenant-concurrent", plugin.ID).Scan(&historyCount); err != nil || historyCount != 1 {
		t.Fatalf("grant history rows=%d err=%v, want one immutable state", historyCount, err)
	}

	type replayResult struct {
		view    contract.PluginGrantView
		created bool
		err     error
	}
	results := make([]replayResult, writers)
	start = make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			results[index].view, results[index].created, results[index].err = store.SetPluginGrant(ctx, "tenant-same-key", plugin.ID, "grant-same-key", strings.Repeat("e", 64), contract.PluginGrantUpdate{
				PluginRegistrationID: plugin.ID, TenantID: "tenant-same-key", Enabled: true, ExpectedVersion: 0,
			})
		}(i)
	}
	close(start)
	wg.Wait()
	for index, result := range results {
		if result.err != nil || !result.created || result.view.Version != 1 || !result.view.Enabled || result.view.TenantID != "tenant-same-key" {
			t.Fatalf("concurrent same-key call %d did not replay create result: %+v", index, result)
		}
		if index > 0 && result.view != results[0].view {
			t.Fatalf("concurrent same-key response drifted: first=%+v got=%+v", results[0].view, result.view)
		}
	}
}
