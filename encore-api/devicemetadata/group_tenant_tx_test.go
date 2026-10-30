package devicemetadata

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
)

func TestBeginTenantGroupTxSetsLocalTenant(t *testing.T) {
	tenantID := uuid.NewString()
	tx, err := beginTenantGroupTx(context.Background(), tenantID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var configured string
	if err := tx.QueryRowContext(context.Background(), `SELECT current_setting('app.current_tenant_id')`).Scan(&configured); err != nil {
		t.Fatal(err)
	}
	if configured != tenantID {
		t.Fatalf("tenant setting = %q, want %q", configured, tenantID)
	}
}

func TestBeginTenantGroupTxRejectsInvalidTenant(t *testing.T) {
	for _, tenantID := range []string{"", "tenant-target", uuid.Nil.String(), " " + uuid.NewString()} {
		t.Run(tenantID, func(t *testing.T) {
			tx, err := beginTenantGroupTx(context.Background(), tenantID, &sql.TxOptions{ReadOnly: true})
			if err == nil {
				_ = tx.Rollback()
				t.Fatalf("tenant %q unexpectedly opened a transaction", tenantID)
			}
		})
	}
}
