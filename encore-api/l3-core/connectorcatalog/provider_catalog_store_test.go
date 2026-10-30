package connectorcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"

	"encore.app/l3-core/connectorcatalog/providercatalog"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestProviderCatalogStorePersistsImmutableReleases(t *testing.T) {
	ctx := context.Background()
	providerID := uuid.NewString()
	firstManifest := mustProviderCatalogTestManifest(t, providerID, "opaque-v1", `{"type":"object", "properties":{"host":{"type":"string"}}}`)
	first, err := publishProviderRelease(ctx, firstManifest, "platform-test-publisher", "publish-test-request")
	if err != nil {
		t.Fatalf("publish first version: %v", err)
	}

	replayed, err := publishProviderRelease(ctx, firstManifest, "platform-test-publisher", "publish-test-request")
	if err != nil {
		t.Fatalf("replay identical version: %v", err)
	}
	if !sameProviderRelease(first, replayed) || !first.PublishedAt.Equal(replayed.PublishedAt) {
		t.Fatal("identical replay did not return the persisted release")
	}

	changedManifest := mustProviderCatalogTestManifest(t, providerID, "opaque-v1", `{"type":"object", "properties":{"host":{"type":"string","minLength":1}}}`)
	if _, err := publishProviderRelease(ctx, changedManifest, "platform-test-publisher", "changed-request"); !errors.Is(err, ErrProviderReleaseConflict) {
		t.Fatalf("changed content for existing version error=%v, want conflict", err)
	}

	for _, version := range []string{"opaque-v2", "opaque-v10"} {
		if _, err := publishProviderRelease(ctx, mustProviderCatalogTestManifest(t, providerID, version, `{"type":"object"}`), "platform-test-publisher", "version-request"); err != nil {
			t.Fatalf("publish version %q: %v", version, err)
		}
	}

	tx, err := providerCatalogDB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var auditCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM provider_publication_events WHERE provider_id=$1`, providerID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 3 {
		t.Fatalf("publication audits=%d, want 3 successful distinct versions", auditCount)
	}
	var actorID string
	if err := tx.QueryRow(ctx, `SELECT actor_id FROM provider_publication_events WHERE provider_id=$1 AND version=$2`, providerID, "opaque-v1").Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if actorID != "platform-test-publisher" {
		t.Fatal("publication actor was not preserved")
	}
	loaded, err := loadProviderReleaseTx(ctx, tx, providerID, "opaque-v1")
	if err != nil {
		t.Fatalf("load persisted release: %v", err)
	}
	if !sameProviderRelease(first, loaded) || string(loaded.OriginalConfigurationSchema) != `{"type":"object", "properties":{"host":{"type":"string"}}}` {
		t.Fatal("loaded release differs from the published identity or raw schema bytes")
	}
	if _, err := loadProviderReleaseTx(ctx, tx, providerID, "missing"); !errors.Is(err, ErrProviderReleaseNotFound) {
		t.Fatalf("missing release error=%v, want not found", err)
	}

	var ownVersions []string
	var total int64
	for page := 1; ; page++ {
		items, pageTotal, err := listProviderReleasesTx(ctx, tx, page, 2)
		if err != nil {
			t.Fatalf("list page %d: %v", page, err)
		}
		if page == 1 {
			total = pageTotal
		} else if pageTotal != total {
			t.Fatalf("list total changed between pages: %d then %d", total, pageTotal)
		}
		for _, item := range items {
			if item.ProviderID == providerID {
				ownVersions = append(ownVersions, item.Version)
			}
		}
		if len(items) < 2 || int64(page*2) >= total {
			break
		}
	}
	sort.Strings(ownVersions)
	wantVersions := []string{"opaque-v1", "opaque-v10", "opaque-v2"}
	if fmt.Sprint(ownVersions) != fmt.Sprint(wantVersions) {
		t.Fatalf("listed versions=%v, want %v", ownVersions, wantVersions)
	}

	for name, statement := range map[string]string{
		"update":   `UPDATE provider_releases SET name='changed' WHERE provider_id=$1 AND version=$2`,
		"delete":   `DELETE FROM provider_releases WHERE provider_id=$1 AND version=$2`,
		"truncate": `TRUNCATE provider_releases`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tx.Exec(ctx, `SAVEPOINT provider_release_immutable_check`); err != nil {
				t.Fatal(err)
			}
			var err error
			if name == "truncate" {
				_, err = tx.Exec(ctx, statement)
			} else {
				_, err = tx.Exec(ctx, statement, providerID, "opaque-v1")
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("mutation error=%v, want PostgreSQL constraint/privilege error", err)
			}
			if name != "truncate" && pgErr.Code != "23514" {
				t.Fatalf("%s SQLSTATE=%s, want immutable constraint 23514", name, pgErr.Code)
			}
			if name == "truncate" && pgErr.Code != "23514" && pgErr.Code != "42501" {
				t.Fatalf("truncate SQLSTATE=%s, want immutable constraint 23514 or least-privilege denial 42501", pgErr.Code)
			}
			if _, rollbackErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT provider_release_immutable_check`); rollbackErr != nil {
				t.Fatalf("rollback after rejected mutation: %v", rollbackErr)
			}
			if name == "truncate" && pgErr.Code == "42501" {
				var noTruncatePrivilege, enabledTrigger bool
				if err := tx.QueryRow(ctx, `
					SELECT NOT has_table_privilege(current_user, 'provider_releases', 'TRUNCATE'),
					       EXISTS (
					           SELECT 1 FROM pg_catalog.pg_trigger
					            WHERE tgrelid='provider_releases'::regclass
					              AND tgname='provider_releases_reject_truncate'
					              AND tgenabled='O'
					       )
				`).Scan(&noTruncatePrivilege, &enabledTrigger); err != nil {
					t.Fatalf("inspect truncate privilege and trigger: %v", err)
				}
				if !noTruncatePrivilege || !enabledTrigger {
					t.Fatalf("truncate boundary: noPrivilege=%v enabledTrigger=%v", noTruncatePrivilege, enabledTrigger)
				}
			}
			if _, releaseErr := tx.Exec(ctx, `RELEASE SAVEPOINT provider_release_immutable_check`); releaseErr != nil {
				t.Fatalf("release savepoint: %v", releaseErr)
			}
		})
	}
}

func mustProviderCatalogTestManifest(t *testing.T, providerID, version, configuration string) *providercatalog.ValidatedManifest {
	t.Helper()
	input := providerReleaseManifestWire{
		FormatVersion:         providercatalog.FormatVersion,
		ProviderID:            providerID,
		Name:                  "isolated catalog test",
		Version:               version,
		ArtifactDigest:        "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Protocols:             []string{"test-protocol"},
		DeviceKinds:           []string{"standalone"},
		ConfigurationSchema:   json.RawMessage(configuration),
		DeviceParameterSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
	}
	encoded, err := marshalProviderReleaseManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := providercatalog.ValidateManifest(encoded)
	if err != nil {
		t.Fatalf("validate fixture manifest: %v", err)
	}
	return manifest
}
