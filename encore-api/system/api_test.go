package system

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"encore.app/internal/systemagent"
	"encore.app/l1-access/authn"

	"encore.dev/beta/errs"
)

const tok = "0123456789abcdef0123456789abcdef0123456789ab"

type call struct {
	path string
	body map[string]any
}

// fakeAgent answers fixed responses per path and records the calls it received.
func fakeAgent(t *testing.T, answers map[string]map[string]any) (*systemagent.Client, *[]call) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	l, err := net.Listen("unix", filepath.Join(dir, "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []call
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, call{strings.TrimPrefix(r.URL.Path, "/v1/system/"), body})
		ans, ok := answers[strings.TrimPrefix(r.URL.Path, "/v1/system/")]
		if !ok {
			ans = map[string]any{"ok": false, "error": "not enabled"}
		}
		_ = json.NewEncoder(w).Encode(ans)
	})}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	tf := filepath.Join(dir, "token")
	os.WriteFile(tf, []byte(tok), 0o600)
	return &systemagent.Client{Socket: filepath.Join(dir, "s.sock"), TokenFile: tf}, &calls
}

func code(err error) errs.ErrCode {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return errs.Unknown
}

func TestStatusRelaysHealthOperationAndUpgrade(t *testing.T) {
	c, _ := fakeAgent(t, map[string]map[string]any{"status": {"ok": true, "agent_version": "v0.3", "result": map[string]any{"ok": false, "components": []map[string]any{{"name": "backend", "group": "control", "state": "down"}}},
		"operation": map[string]any{"id": "1", "kind": "upgrade", "state": "running", "step": "snapshot", "percent": 20}, "upgrade": map[string]any{"phase": "stopping", "to": "v1.4.0"}}})
	got, err := status(context.Background(), c)
	if err != nil || got.OK || len(got.Components) != 1 || got.Operation.Step != "snapshot" || got.Upgrade.Phase != "stopping" || got.AgentVersion != "v0.3" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRestartValidatesScopeBeforeCallingTheAgent(t *testing.T) {
	c, calls := fakeAgent(t, map[string]map[string]any{"restart": {"ok": true, "operation": map[string]any{"id": "1", "kind": "restart", "state": "running"}}})
	for _, bad := range []string{"", "everything", "CONTROL", "control; rm"} {
		if _, err := restart(context.Background(), c, bad); code(err) != errs.InvalidArgument {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("invalid requests must not reach the Agent: %v", *calls)
	}
	for _, good := range []string{"control", "full"} {
		if r, err := restart(context.Background(), c, good); err != nil || r.Operation.Kind != "restart" {
			t.Fatalf("%s: %v", good, err)
		}
	}
	if (*calls)[0].body["scope"] != "control" || (*calls)[1].body["scope"] != "full" {
		t.Fatalf("%v", *calls)
	}
}

func TestBusyAndUnavailableAreStableErrors(t *testing.T) {
	c, _ := fakeAgent(t, map[string]map[string]any{"restart": {"ok": false, "error": "another system operation is running"}})
	if _, err := restart(context.Background(), c, "control"); code(err) != errs.FailedPrecondition {
		t.Fatalf("busy: %v", err)
	}
	if _, err := restart(context.Background(), &systemagent.Client{}, "control"); code(err) != errs.Unavailable {
		t.Fatalf("unavailable: %v", err)
	}
}

func TestUpgradeAndBackupNamesAreValidated(t *testing.T) {
	c, calls := fakeAgent(t, map[string]map[string]any{
		"upgrade":        {"ok": true, "operation": map[string]any{"id": "u", "kind": "upgrade", "state": "running"}},
		"backup/restore": {"ok": true, "operation": map[string]any{"id": "r", "kind": "restore", "state": "running"}},
		"backup/delete":  {"ok": true},
	})
	for _, bad := range []string{"1.4.0", "latest", "v1.4.0 && x", "../v1"} {
		if _, err := upgrade(context.Background(), c, bad); code(err) != errs.InvalidArgument {
			t.Errorf("version %q accepted", bad)
		}
	}
	for _, bad := range []string{"../../etc/passwd", "tp-backup-x.tar", "tp-backup-20261112T105328Z-v1-full.tar/../x", "other.tar"} {
		if _, err := backupAction(context.Background(), c, "backup/restore", bad); code(err) != errs.InvalidArgument {
			t.Errorf("backup %q accepted", bad)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("rejected input reached the Agent: %v", *calls)
	}
	if _, err := upgrade(context.Background(), c, "v1.4.0"); err != nil {
		t.Fatal(err)
	}
	name := "tp-backup-20261112T105328Z-v1.3.0-full.tar"
	if r, err := backupAction(context.Background(), c, "backup/restore", name); err != nil || r.Operation.Kind != "restore" {
		t.Fatal(err)
	}
	if _, err := backupAction(context.Background(), c, "backup/delete", name); err != nil {
		t.Fatal(err)
	}
}

func TestUpdatesListsPlatformAndAgentAndKeepsWorkingWhenTheCheckFails(t *testing.T) {
	c, _ := fakeAgent(t, map[string]map[string]any{
		"status":  {"ok": true, "agent_version": "v0.3.0"},
		"updates": {"ok": true, "updates": map[string]any{"current": "v1.3.0", "latest": "v1.4.0", "newer": true, "valid_until": "2027-01-01T00:00:00Z"}},
	})
	got, err := updates(context.Background(), c)
	if err != nil || len(got.Items) != 2 || !got.Items[0].CanUpgrade || got.Items[0].Latest != "v1.4.0" || got.Items[1].Kind != "agent" || got.Items[1].CanUpgrade || got.Items[1].Instructions == "" {
		t.Fatalf("%+v %v", got, err)
	}
	failing, _ := fakeAgent(t, map[string]map[string]any{
		"status":  {"ok": true, "agent_version": "v0.3.0"},
		"updates": {"ok": false, "error": "update check failed", "code": "updates_manifest_rejected"},
	})
	got, err = updates(context.Background(), failing)
	if err != nil || got.Error != "updates_manifest_rejected" || len(got.Items) != 1 || got.Items[0].Kind != "agent" {
		t.Fatalf("a failed release check must still show the rest: %+v %v", got, err)
	}
}

func TestBackupsEmptyListIsAnArrayAndCheckIsRelayed(t *testing.T) {
	c, _ := fakeAgent(t, map[string]map[string]any{
		"backup/list": {"ok": true},
		"check":       {"ok": true, "check": map[string]any{"ok": false, "service": "backend", "code": "config_invalid"}},
	})
	b, err := backups(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b)
	if !strings.Contains(string(raw), `"backups":[]`) {
		t.Fatalf("the UI needs [] not null: %s", raw)
	}
	ch, err := check(context.Background(), c)
	if err != nil || ch.OK || ch.Service != "backend" || ch.Code != "config_invalid" {
		t.Fatalf("%+v %v", ch, err)
	}
}

func TestEveryHandlerRefusesAnonymousAndNonAdmin(t *testing.T) {
	ctx := context.Background()
	defer func(old func() *authn.Data) { currentAuth = old }(currentAuth)
	for _, caller := range []*authn.Data{nil, {Roles: []string{"TENANT_ADMIN"}}, {Roles: []string{"TENANT_USER"}}, {}} {
		currentAuth = func() *authn.Data { return caller }
		refuseAll(t, ctx)
	}
	// a super administrator passes the check (and then reaches the unconfigured client: unavailable, not refused)
	currentAuth = func() *authn.Data { return &authn.Data{Roles: []string{"SYS_ADMIN"}} }
	var logged []string
	oldLog := logAction
	logAction = func(a string) { logged = append(logged, a) }
	defer func() { logAction = oldLog }()
	old := agent
	agent = &systemagent.Client{}
	defer func() { agent = old }()
	if _, err := Status(ctx); code(err) != errs.Unavailable {
		t.Fatalf("an administrator must get past the check: %v", err)
	}
	if len(logged) != 1 || logged[0] != "status" {
		t.Fatalf("the action must be logged once: %v", logged)
	}
}

func refuseAll(t *testing.T, ctx context.Context) {
	t.Helper()
	for name, fn := range map[string]func() error{
		"status":  func() error { _, e := Status(ctx); return e },
		"check":   func() error { _, e := CheckConfig(ctx); return e },
		"restart": func() error { _, e := Restart(ctx, &RestartRequest{Scope: "control"}); return e },
		"updates": func() error { _, e := Updates(ctx); return e },
		"upgrade": func() error { _, e := Upgrade(ctx, &UpgradeRequest{To: "v1.4.0"}); return e },
		"list":    func() error { _, e := ListBackups(ctx); return e },
		"create":  func() error { _, e := CreateBackup(ctx); return e },
		"restore": func() error { _, e := RestoreBackup(ctx, "tp-backup-20261112T105328Z-v1.3.0-full.tar"); return e },
		"delete":  func() error { _, e := DeleteBackup(ctx, "tp-backup-20261112T105328Z-v1.3.0-full.tar"); return e },
	} {
		if code(fn()) != errs.PermissionDenied {
			t.Errorf("%s: not refused", name)
		}
	}
}
