package main

import (
	"strings"
	"testing"
)

func TestNotificationWriteUsesHeaderAndResourceSpecificKey(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	file := writeTemp(t, `{"expectedVersion":1,"name":"example"}`)
	for _, id := range []string{"group-a", "group-b"} {
		code, _, stderr := runCLI("--base-url", srv.URL, "notifications", "group-update", id, "--file", file)
		if code != 0 {
			t.Fatalf("update %s: %d %s", id, code, stderr)
		}
	}
	reqs := cap.all()
	if len(reqs) != 2 || reqs[0].Idem == "" || reqs[0].Idem == reqs[1].Idem || strings.Contains(reqs[0].Body, "request_id") {
		t.Fatalf("unexpected requests: %+v", reqs)
	}
}

func TestNotificationRejectsPathInjection(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	code, _, _ := runCLI("--base-url", srv.URL, "notifications", "get", "id?other=1")
	if code != exitUsage || len(cap.all()) != 0 {
		t.Fatalf("code=%d requests=%+v", code, cap.all())
	}
}

func TestNotificationInstanceCreateUsesSelectedTenant(t *testing.T) {
	srv, cap := newServer(t, 201, `{}`)
	file := writeTemp(t, `{"name":"account"}`)
	code, _, stderr := runCLI("--base-url", srv.URL, "notifications", "instance-create", "tenant-1", "--file", file)
	if code != 0 {
		t.Fatalf("create: %d %s", code, stderr)
	}
	if got := cap.last(); got.Path != "/api/v2/platform/notification-tenants/tenant-1/notification-instances" || got.Idem == "" || strings.Contains(got.Body, "request_id") {
		t.Fatalf("request: %+v", got)
	}
}

func TestNotificationCommandRoutesAndRetrySafety(t *testing.T) {
	commands := notificationCommands()
	want := map[string]struct {
		method, path string
		write        bool
	}{
		"notifications.list":               {"GET", "/api/v2/notifications", false},
		"notifications.get":                {"GET", "/api/v2/notifications/:id", false},
		"notifications.send":               {"POST", "/api/v2/notifications", true},
		"notifications.deliveries":         {"GET", "/api/v2/notification-deliveries", false},
		"notifications.delivery":           {"GET", "/api/v2/notification-deliveries/:id", false},
		"notifications.plugins":            {"GET", "/api/v2/notification-plugins", false},
		"notifications.plugin":             {"GET", "/api/v2/notification-plugins/:id", false},
		"notifications.instances":          {"GET", "/api/v2/notification-instances", false},
		"notifications.instance":           {"GET", "/api/v2/notification-instances/:id", false},
		"notifications.tenant-plugins":     {"GET", "/api/v2/platform/notification-tenants/:tenantId/notification-plugins", false},
		"notifications.tenant-instances":   {"GET", "/api/v2/platform/notification-tenants/:tenantId/notification-instances", false},
		"notifications.tenant-instance":    {"GET", "/api/v2/platform/notification-tenants/:tenantId/notification-instances/:id", false},
		"notifications.test":               {"POST", "/api/v2/notification-instances/:id/test", true},
		"notifications.groups":             {"GET", "/api/v2/notification-groups", false},
		"notifications.group":              {"GET", "/api/v2/notification-groups/:id", false},
		"notifications.group-create":       {"POST", "/api/v2/notification-groups", true},
		"notifications.group-update":       {"PUT", "/api/v2/notification-groups/:id", true},
		"notifications.instance-validate":  {"POST", "/api/v2/platform/notification-tenants/:tenantId/notification-instances/validate", false},
		"notifications.instance-create":    {"POST", "/api/v2/platform/notification-tenants/:tenantId/notification-instances", true},
		"notifications.instance-update":    {"PUT", "/api/v2/platform/notification-tenants/:tenantId/notification-instances/:id", true},
		"notifications.plugin-register":    {"POST", "/api/v2/notification-plugins", true},
		"notifications.plugin-update":      {"PUT", "/api/v2/notification-plugins/:id", true},
		"notifications.grants":             {"GET", "/api/v2/notification-plugin-grants", false},
		"notifications.grant-set":          {"PUT", "/api/v2/notification-plugin-grants", true},
		"notifications.receipt-credential": {"POST", "/api/v2/platform/notification-tenants/:tenantId/notification-instances/:id/receipt-credential", true},
	}
	if len(commands) != len(want) {
		t.Fatalf("command count=%d want=%d", len(commands), len(want))
	}
	for name, spec := range want {
		var got *command
		for _, candidate := range commands {
			if candidate.name == name {
				got = candidate
				break
			}
		}
		if got == nil {
			t.Fatalf("missing command %q", name)
		}
		if got.method != spec.method || got.path != spec.path {
			t.Errorf("%s route=%s %s, want %s %s", name, got.method, got.path, spec.method, spec.path)
		}
		if got.sideEffect != spec.write {
			t.Errorf("%s sideEffect=%v want %v", name, got.sideEffect, spec.write)
		}
		if got.sideEffect && !got.headerOnlyKey {
			t.Errorf("%s must send idempotency key in header without injecting unrelated request_id", name)
		}
	}
}

func TestNotificationQueryBuildUsesFrozenCamelCaseNames(t *testing.T) {
	var list *command
	for _, c := range notificationCommands() {
		if c.name == "notifications.list" {
			list = c
			break
		}
	}
	if list == nil {
		t.Fatal("notifications.list missing")
	}
	in := &invocation{values: map[string][]string{
		"page": {"2"}, "pageSize": {"10"}, "sourceType": {"alarm"}, "intakeStatus": {"blocked"},
	}}
	_, query, err := list.build(in)
	if err != nil {
		t.Fatal(err)
	}
	if query["page"][0] != "2" || query["pageSize"][0] != "10" || query["sourceType"][0] != "alarm" || query["intakeStatus"][0] != "blocked" {
		t.Fatalf("query=%v", query)
	}
}
