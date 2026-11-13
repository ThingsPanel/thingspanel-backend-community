package system

import (
	"testing"

	"encore.app/l1-access/authn"
)

func TestOnlySystemAdminPasses(t *testing.T) {
	cases := []struct {
		name string
		data *authn.Data
		want bool
	}{
		{"nil", nil, false},
		{"tenant admin", &authn.Data{Roles: []string{"TENANT_ADMIN"}}, false},
		{"no roles", &authn.Data{}, false},
		{"sys admin", &authn.Data{Roles: []string{"SYS_ADMIN"}}, true},
		{"sys admin among others", &authn.Data{Roles: []string{"TENANT_USER", "sys_admin"}}, true},
	}
	for _, c := range cases {
		if got := isSystemAdmin(c.data); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestPlatformVersionOverride(t *testing.T) {
	warned := 0
	old := warnInvalidOverride
	warnInvalidOverride = func(string) { warned++ }
	defer func() { warnInvalidOverride = old }()
	cases := []struct {
		name, override, platform, want string
		warn                           int
	}{
		{"unset", "", "", "dev", 0},
		{"platform only", "", "v1.4.0-beta.2", "v1.4.0-beta.2", 0},
		{"override wins", "v1.4.0", "v1.4.0-beta.2", "v1.4.0", 0},
		{"beta override", "v1.4.0-beta.3", "", "v1.4.0-beta.3", 0},
		{"invalid ignored", "latest", "v1.3.0", "v1.3.0", 1},
		{"invalid, nothing else", "v1.4", "", "dev", 1},
	}
	for _, c := range cases {
		warned = 0
		t.Setenv("TP_VERSION_OVERRIDE", c.override)
		t.Setenv("TP_PLATFORM_VERSION", c.platform)
		if got := platformVersion(); got != c.want || warned != c.warn {
			t.Errorf("%s: got %q warned=%d, want %q warned=%d", c.name, got, warned, c.want, c.warn)
		}
	}
}
