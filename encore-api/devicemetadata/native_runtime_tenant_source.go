package devicemetadata

import (
	"context"
	"os"
	"strings"
	"time"

	"encore.app/internal/runtimetenant"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

func runtimeTenantScope() runtimetenant.Scope {
	namespace := os.Getenv(runtimeNamespaceEnv)
	if namespace == "" {
		namespace = "community"
	}
	return runtimetenant.Scope{Environment: os.Getenv("TP_RUNTIME_TENANT_BINDING_ENVIRONMENT"), ProviderID: os.Getenv(runtimeProviderEnv), Namespace: namespace, ServiceInstanceID: os.Getenv(runtimeServiceInstanceEnv), CommunityOrigin: strings.TrimRight(strings.TrimSpace(os.Getenv("TP_COMMUNITY_BASE_URL")), "/")}
}
func currentRuntimeSource(ctx context.Context, tenantID string) (runtimetenant.Binding, error) {
	data := authn.CurrentData()
	if data == nil || data.UserID == "" || data.TenantID == "" {
		return runtimetenant.Binding{}, runtimeSourceUnavailable()
	}
	return verifiedRuntimeSource(ctx, runtimetenant.SQLReader{DB: db.Stdlib()}, tenantID, data.TenantID)
}
func verifiedRuntimeSource(ctx context.Context, reader runtimetenant.Queryer, tenantID, rawTenant string) (runtimetenant.Binding, error) {
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	binding, err := runtimetenant.Lookup(bounded, reader, tenantID, runtimeTenantScope())
	// Exact signed provenance is required in addition to the unique reviewed
	// native UUID mapping. A colliding normalization can never claim this source.
	if err != nil || rawTenant == "" || binding.RawTenantID != rawTenant {
		return runtimetenant.Binding{}, runtimeSourceUnavailable()
	}
	return binding, nil
}
func runtimeSourceUnavailable() error {
	return &errs.Error{Code: errs.Unavailable, Message: "runtime tenant source requires a reviewed binding"}
}

type runtimeBindingTxReader struct{ tx *sqldb.Tx }

func (r runtimeBindingTxReader) QueryRowContext(ctx context.Context, q string, args ...any) runtimetenant.RowScanner {
	return r.tx.QueryRow(ctx, q, args...)
}
