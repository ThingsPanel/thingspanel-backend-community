package pluginmgr

import (
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"github.com/stretchr/testify/require"
)

func TestProviderPublicationRequiresVerifiedPlatformAuthority(t *testing.T) {
	for _, tc := range []struct {
		name string
		data *authn.Data
		want errs.ErrCode
	}{
		{"no identity", nil, errs.Unauthenticated},
		{"no subject", &authn.Data{Roles: []string{"SYS_ADMIN"}}, errs.Unauthenticated},
		{"verified platform administrator", &authn.Data{UserID: "publisher", Roles: []string{"SYS_ADMIN"}}, errs.OK},
		{"tenant cannot publish globally", &authn.Data{UserID: "user", Roles: []string{"TENANT_ADMIN"}, Scopes: []string{"*", "provider:publish"}}, errs.PermissionDenied},
		{"legacy lowercase cannot grant", &authn.Data{UserID: "user", Roles: []string{"platform_admin"}}, errs.PermissionDenied},
		{"scope cannot grant", &authn.Data{UserID: "user", Scopes: []string{"provider:publish"}}, errs.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, errs.Code(authorizeProviderPublication(tc.data))) })
	}
}
