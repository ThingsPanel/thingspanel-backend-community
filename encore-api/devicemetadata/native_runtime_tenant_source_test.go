package devicemetadata

import (
	"context"
	"database/sql"
	"testing"

	"encore.app/internal/runtimetenant"
)

type sourceFixtureRow struct{ raw string }

func (r sourceFixtureRow) Scan(dest ...any) error {
	*dest[0].(*string) = "00000000-0000-0000-0000-000000000001"
	*dest[1].(*string) = "1234abcd-0000-0000-0000-000000000000"
	*dest[2].(*string) = r.raw
	*dest[3].(*int64) = 1
	return nil
}

type sourceFixtureReader struct{}

func (sourceFixtureReader) QueryRowContext(context.Context, string, ...any) runtimetenant.RowScanner {
	return sourceFixtureRow{raw: "1234abcd"}
}
func TestRuntimeSourceRequiresExactSignedRawProvenance(t *testing.T) {
	t.Setenv("TP_RUNTIME_TENANT_BINDING_ENVIRONMENT", "fixture")
	t.Setenv(runtimeProviderEnv, "fixture-provider")
	t.Setenv(runtimeNamespaceEnv, "community")
	t.Setenv(runtimeServiceInstanceEnv, "fixture-instance")
	t.Setenv("TP_COMMUNITY_BASE_URL", "https://community.example.invalid")
	native := "1234abcd-0000-0000-0000-000000000000"
	for _, raw := range []string{"1234-abcd", native, "foreign", ""} {
		if _, e := verifiedRuntimeSource(context.Background(), sourceFixtureReader{}, native, raw); e == nil {
			t.Fatalf("normalization collision or missing raw proof admitted: %q", raw)
		}
	}
	binding, e := verifiedRuntimeSource(context.Background(), sourceFixtureReader{}, native, "1234abcd")
	if e != nil || binding.RawTenantID != "1234abcd" {
		t.Fatal("reviewed exact raw provenance refused")
	}
}
func TestLegacyRuntimeOutboxWithoutSourceProofNeverDispatches(t *testing.T) {
	// No DB, credentials, or HTTP server is involved. Missing immutable intent
	// provenance must stop before any native lookup or Community side effect.
	item := runtimeOutboxItem{Kind: operationKindDeviceDeletion, Desired: []byte(`{}`), SourceBindingID: sql.NullString{}, SourceBindingGeneration: sql.NullInt64{}}
	outcome := (communityRuntimeApplier{}).Apply(context.Background(), item)
	if !outcome.Skip || outcome.Applied {
		t.Fatal("unproven historical outbox dispatched")
	}
}
