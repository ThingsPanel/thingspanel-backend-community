package authz

import (
	"sync"

	"encore.app/l1-access/authn"
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

var (
	once     sync.Once
	enforcer *casbin.Enforcer
	initErr  error
)

// Action names are shared by every resource so the permission matrix stays
// readable: one resource, one set of verbs.
const (
	ActionView   = "view"
	ActionManage = "manage"
	ActionReplay = "replay"
)

func getEnforcer() (*casbin.Enforcer, error) {
	once.Do(func() {
		mconf := `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && (p.obj == "*" || r.obj == p.obj) && (p.act == "*" || r.act == p.act)
`
		m, err := model.NewModelFromString(mconf)
		if err != nil {
			initErr = err
			return
		}
		e, err := casbin.NewEnforcer(m)
		if err != nil {
			initErr = err
			return
		}
		// Role -> permission mappings.
		// sub(role), obj(resource), act(action)
		_, _ = e.AddPolicy("platform_admin", "*", "*")
		_, _ = e.AddPolicy("tenant_admin", "device", "view")
		_, _ = e.AddPolicy("tenant_admin", "device", "control")
		_, _ = e.AddPolicy("tenant_admin", "device", "manage")
		_, _ = e.AddPolicy("tenant_admin", "plugin", "manage")
		// Native device-metadata group access is granted only to the verified
		// Community TENANT_ADMIN identity. Scopes are not consulted here: the
		// current Community user-token issuer does not mint delegated scopes.
		_, _ = e.AddPolicy("TENANT_ADMIN", "group", "read")
		_, _ = e.AddPolicy("TENANT_ADMIN", "group", "write")
		_, _ = e.AddPolicy("TENANT_ADMIN", "group", "delete")
		_, _ = e.AddPolicy("TENANT_ADMIN", "device", "read")
		// Sending commands and opening debug sessions are tenant-administrator
		// actions on the native API; without these rows the command, desired
		// message and debug tabs answered 403 for a real TENANT_ADMIN.
		_, _ = e.AddPolicy("TENANT_ADMIN", "device", "control")
		_, _ = e.AddPolicy("TENANT_ADMIN", "device", "view")
		_, _ = e.AddPolicy("TENANT_ADMIN", "device", "write")
		_, _ = e.AddPolicy("TENANT_ADMIN", "device", "credentials:read")
		_, _ = e.AddPolicy("TENANT_ADMIN", "device", "credentials:write")
		_, _ = e.AddPolicy("TENANT_ADMIN", "device", "simulate")
		_, _ = e.AddPolicy("TENANT_ADMIN", "device", "diagnose")
		_, _ = e.AddPolicy("TENANT_ADMIN", "device", "data:export")
		_, _ = e.AddPolicy("TENANT_ADMIN", "template", "read")
		_, _ = e.AddPolicy("TENANT_ADMIN", "template", "write")
		_, _ = e.AddPolicy("TENANT_ADMIN", "template", "delete")
		_, _ = e.AddPolicy("TENANT_ADMIN", "template", "diagnose")
		// Global immutable release publication is a private platform operation;
		// this grants no tenant resource access or delegated tenant identity.
		_, _ = e.AddPolicy("SYS_ADMIN", "provider", "publish")
		_, _ = e.AddPolicy("tenant_operator", "device", "view")
		_, _ = e.AddPolicy("tenant_operator", "device", "control")
		_, _ = e.AddPolicy("tenant_viewer", "device", "view")
		// Northbound (data forwarding). Reading configuration and delivery
		// history is a view action; changing connections/pipelines or sending a
		// test record is manage; replaying a failed delivery is replay. There is
		// no separate northbound identity or permission store.
		_, _ = e.AddPolicy("tenant_admin", "northbound", ActionView)
		_, _ = e.AddPolicy("tenant_admin", "northbound", ActionManage)
		_, _ = e.AddPolicy("tenant_admin", "northbound", ActionReplay)
		_, _ = e.AddPolicy("tenant_operator", "northbound", ActionView)
		_, _ = e.AddPolicy("tenant_viewer", "northbound", ActionView)
		enforcer = e
	})
	return enforcer, initErr
}

func Can(data *authn.Data, resource, action string) bool {
	if data == nil || len(data.Roles) == 0 {
		return false
	}
	e, err := getEnforcer()
	if err != nil {
		return false
	}
	for _, role := range data.Roles {
		ok, _ := e.Enforce(role, resource, action)
		if ok {
			return true
		}
	}
	return false
}

func CanDevice(data *authn.Data, action, deviceID string) bool {
	if !Can(data, "device", action) {
		return false
	}
	if hasRole(data.Roles, "platform_admin") {
		return true
	}
	// Empty binding list means tenant-level role scope.
	if len(data.AuthorizedDeviceIDs) == 0 {
		return true
	}
	for _, d := range data.AuthorizedDeviceIDs {
		if d == deviceID {
			return true
		}
	}
	return false
}

func hasRole(roles []string, target string) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}
