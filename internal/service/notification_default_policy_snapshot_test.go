package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/dal"
)

func TestDefaultPolicyReadinessRechecksPublishedCoreSnapshot(t *testing.T) {
	openSourceCompatPGFixture(t)
	const tenantID = "tenant-default-snapshot"
	const nativeID = "native-default-snapshot"
	key := seedDefaultSnapshotRoute(t, tenantID, nativeID, 7)
	if _, err := dal.SetTenantDefaultPolicy(context.Background(), key.DeploymentID, tenantID, nativeID, key.LegacyGroup, 0); err != nil {
		t.Fatalf("seed tenant default: %v", err)
	}

	groupEnabled, instanceEnabled, pluginEnabled := true, true, true
	var snapshotCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, sourceGroupSnapshotPath) {
			t.Errorf("unexpected Core fixture request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		snapshotCalls++
		writeNativePublishSnapshotFixtureWithTargets(w, r, groupEnabled, instanceEnabled, pluginEnabled)
	}))
	defer server.Close()
	bridge := testSourceBridge(t, server)
	defer bridge.Close()
	SetActiveSourceBridge(bridge)
	t.Cleanup(func() { SetActiveSourceBridge(nil) })

	ready, err := GetTenantDefaultPolicy(context.Background(), tenantID)
	if err != nil || ready.Selected == nil || !ready.Selected.Ready || len(ready.AvailablePolicies) != 1 || snapshotCalls < 2 {
		t.Fatalf("enabled exact Core snapshot should be ready and available: view=%+v calls=%d err=%v", ready, snapshotCalls, err)
	}

	for _, disabled := range []struct {
		name     string
		group    bool
		instance bool
		plugin   bool
	}{
		{name: "group", group: false, instance: true, plugin: true},
		{name: "instance", group: true, instance: false, plugin: true},
		{name: "plugin", group: true, instance: true, plugin: false},
	} {
		t.Run(disabled.name, func(t *testing.T) {
			groupEnabled, instanceEnabled, pluginEnabled = disabled.group, disabled.instance, disabled.plugin
			view, err := GetTenantDefaultPolicy(context.Background(), tenantID)
			if err != nil || view.Selected == nil || view.Selected.Ready || view.Selected.Status != "unavailable" || len(view.AvailablePolicies) != 0 {
				t.Fatalf("disabled Core %s remained ready: view=%+v err=%v", disabled.name, view, err)
			}
		})
	}
}

func TestSetDefaultPolicyRejectsCurrentlyDisabledCoreTarget(t *testing.T) {
	openSourceCompatPGFixture(t)
	const tenantID = "tenant-default-put-snapshot"
	const nativeID = "native-default-put-snapshot"
	key := seedDefaultSnapshotRoute(t, tenantID, nativeID, 3)

	instanceEnabled := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, sourceGroupSnapshotPath) {
			t.Errorf("unexpected Core fixture request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		writeNativePublishSnapshotFixtureWithTargets(w, r, true, instanceEnabled, true)
	}))
	defer server.Close()
	bridge := testSourceBridge(t, server)
	defer bridge.Close()
	SetActiveSourceBridge(bridge)
	t.Cleanup(func() { SetActiveSourceBridge(nil) })

	if _, err := SetTenantDefaultPolicy(context.Background(), tenantID, nativeID, 0); err != ErrDefaultPolicyInvalid {
		t.Fatalf("disabled instance target should be rejected before default pointer write, got %v", err)
	}
	selected, _, err := dal.GetTenantDefaultPolicy(context.Background(), key.DeploymentID, tenantID)
	if err != nil || selected.Version != 0 || selected.NativeGroupID != nil {
		t.Fatalf("failed target validation wrote default policy: record=%+v err=%v", selected, err)
	}

	instanceEnabled = true
	view, err := SetTenantDefaultPolicy(context.Background(), tenantID, nativeID, 0)
	if err != nil || view.Version != 1 || view.Selected == nil || !view.Selected.Ready {
		t.Fatalf("enabled Core target should permit default selection: view=%+v err=%v", view, err)
	}
}

func seedDefaultSnapshotRoute(t *testing.T, tenantID, nativeID string, revision int64) dal.SourceRouteKey {
	t.Helper()
	key := dal.SourceRouteKey{DeploymentID: "deployment-test", TenantID: tenantID,
		LegacyGroup: deterministicNativeAliasID("deployment-test", tenantID, nativeID)}
	if err := dal.EnsureNativePublishAlias(context.Background(), key, key.LegacyGroup, "default snapshot fixture"); err != nil {
		t.Fatalf("create native publish alias: %v", err)
	}
	if _, _, err := dal.ActivateNativePublishRoute(context.Background(), key, nativeID, "default snapshot fixture", revision, 0, "default-snapshot-fixture-projection"); err != nil {
		t.Fatalf("activate native publish route: %v", err)
	}
	return key
}
