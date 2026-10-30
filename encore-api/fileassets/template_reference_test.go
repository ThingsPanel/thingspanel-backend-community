package fileassets

import (
	"context"
	"encoding/json"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestValidateTemplateImageIsMetadataOnlyTenantScopedAndRoleChecked(t *testing.T) {
	ctx := context.Background()
	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	assetID := uuid.NewString()
	insertCommittedTestAsset(t, ctx, tenantA, assetID, "tenant/"+tenantA+"/"+assetID)
	t.Cleanup(func() { deleteTestAsset(t, ctx, tenantA, assetID) })

	service := &Service{} // no object store or secret box: validation must only read the catalog.
	userID := "template-reference-" + uuid.NewString()
	setPrincipal := func(tenant string, roles ...string) {
		et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: tenant, Roles: roles})
	}
	defer et.OverrideAuthInfo("", nil)

	setPrincipal(tenantA, "TENANT_ADMIN")
	response, err := service.ValidateTemplateImage(ctx, &ValidateTemplateImageRequest{AssetID: assetID})
	require.NoError(t, err)
	require.Equal(t, ValidateTemplateImageResponse{AssetID: assetID}, *response)
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	require.Equal(t, `{"assetId":"`+assetID+`"}`, string(encoded))
	for _, forbidden := range []string{"downloadUrl", "expiresAt", "objectKey", "signature"} {
		require.NotContains(t, string(encoded), forbidden)
	}

	tx, err := BeginTenantScopedTx(ctx, tenantA)
	require.NoError(t, err)
	var leases int
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM file_asset_url_leases WHERE tenant_id=$1::uuid AND asset_id=$2::uuid`, tenantA, assetID).Scan(&leases))
	require.NoError(t, tx.Commit())
	require.Zero(t, leases, "reference validation must not create a URL lease")

	setPrincipal(tenantB, "TENANT_ADMIN")
	_, err = service.ValidateTemplateImage(ctx, &ValidateTemplateImageRequest{AssetID: assetID})
	require.Equal(t, errs.NotFound, errs.Code(err), "cross-tenant asset must be indistinguishable from missing")

	setPrincipal(tenantA, "SYS_ADMIN")
	_, err = service.ValidateTemplateImage(ctx, &ValidateTemplateImageRequest{AssetID: assetID})
	require.Equal(t, errs.PermissionDenied, errs.Code(err))

	setPrincipal(tenantA, "TENANT_ADMIN")
	_, err = service.ValidateTemplateImage(ctx, nil)
	require.Equal(t, errs.InvalidArgument, errs.Code(err))
}
