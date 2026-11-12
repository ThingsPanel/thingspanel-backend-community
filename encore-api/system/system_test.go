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
