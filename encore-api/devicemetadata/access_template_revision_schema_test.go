package devicemetadata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAccessTemplateCurrentConfigurationSchemaKeepsThingModelVersions(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var templateOwner, revisionOwner, configurationColumn, draftColumn bool
	err = tx.QueryRow(ctx, `SELECT
 to_regclass('device_access_templates') IS NOT NULL,
 to_regclass('device_access_template_revisions') IS NOT NULL,
 EXISTS(SELECT FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='device_access_templates' AND column_name='configuration'),
 EXISTS(SELECT FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='device_access_templates' AND column_name='draft')`).Scan(&templateOwner, &revisionOwner, &configurationColumn, &draftColumn)
	if err != nil || !templateOwner || revisionOwner || !configurationColumn || draftColumn {
		t.Fatalf("single configuration schema: template=%v revisions=%v configuration=%v draft=%v err=%v", templateOwner, revisionOwner, configurationColumn, draftColumn, err)
	}
	tenantID := testTenantID()
	modelID, snapshots := seedRevisionSchemaModel(t, ctx, tx, tenantID, "current-template-model-", "PUBLISHED", []revisionSchemaSnapshot{
		{version: 1, canonical: true, semanticDigest: strings.Repeat("a", 64), resourcesDigest: strings.Repeat("b", 64)},
		{version: 2, canonical: true, semanticDigest: strings.Repeat("c", 64), resourcesDigest: strings.Repeat("d", 64)},
	})
	for _, revision := range []int{1, 2} {
		selection := []byte(fmt.Sprintf(`{"modelId":%q,"publishedRevision":%d}`, modelID, revision))
		binding, err := resolveAccessTemplateModelBindingTx(ctx, tx, tenantID, selection)
		if err != nil || binding == nil || binding.SnapshotID != snapshots[revision] || binding.PublishedRevision != uint64(revision) {
			t.Fatalf("ThingModel version %d no longer resolves: binding=%+v err=%v", revision, binding, err)
		}
	}
	expectRevisionSchemaError(t, ctx, tx, "model_snapshot_still_immutable", "P0001", `UPDATE thing_model_snapshots SET content='{}' WHERE tenant_id=$1 AND id=$2`, tenantID, snapshots[1])
}

type revisionSchemaSnapshot struct {
	version           int
	canonical         bool
	canonicalContent  []byte
	semanticDigest    string
	resourcesDigest   string
	resourcesContent  string
	resourcesRevision *int64
}

func seedRevisionSchemaModel(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID, namePrefix, modelStatus string, snapshots []revisionSchemaSnapshot) (string, map[int]string) {
	t.Helper()
	modelID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO thing_models(id,tenant_id,name,status,canonical_model,draft_revision,published_revision)
		VALUES($1,$2,$3,$4,'{"fixture":true}'::jsonb,1,$5)
	`, modelID, tenantID, namePrefix+modelID, modelStatus, maxSnapshotVersion(snapshots)); err != nil {
		t.Fatalf("insert revision schema model fixture: %v", err)
	}
	ids := make(map[int]string, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotID := uuid.NewString()
		ids[snapshot.version] = snapshotID
		var canonicalContent, canonicalChecksum any
		var resourcesContent, resourcesRevision, resourcesChecksum any
		if snapshot.canonical {
			if snapshot.canonicalContent == nil {
				canonicalContent = []byte(`{"canonical":true}`)
			} else {
				canonicalContent = snapshot.canonicalContent
			}
			canonicalChecksum = snapshot.semanticDigest
			if snapshot.resourcesDigest != "" {
				resourcesContent = snapshot.resourcesContent
				if resourcesContent == "" {
					resourcesContent = `{"profile":{}}`
				}
				revision := int64(0)
				if snapshot.resourcesRevision != nil {
					revision = *snapshot.resourcesRevision
				}
				resourcesRevision = revision
				resourcesChecksum = snapshot.resourcesDigest
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO thing_model_snapshots(
				id,thing_model_id,tenant_id,version,content,published_by,
				canonical_snapshot,canonical_content,canonical_checksum,
				resources_content,resources_revision,resources_checksum
			)
			VALUES($1,$2,$3,$4,'{}'::jsonb,$5,$6,$7,$8,$9,$10,$11)
		`, snapshotID, modelID, tenantID, snapshot.version, uuid.NewString(), snapshot.canonical,
			canonicalContent, canonicalChecksum, resourcesContent, resourcesRevision, resourcesChecksum); err != nil {
			t.Fatalf("insert revision schema snapshot fixture v%d: %v", snapshot.version, err)
		}
	}
	latest := maxSnapshotVersion(snapshots)
	if latest > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE thing_models SET current_snapshot_id=$1
			WHERE tenant_id=$2 AND id=$3
		`, ids[latest], tenantID, modelID); err != nil {
			t.Fatalf("set current revision schema snapshot: %v", err)
		}
	}
	return modelID, ids
}

func maxSnapshotVersion(snapshots []revisionSchemaSnapshot) int {
	max := 0
	for _, snapshot := range snapshots {
		if snapshot.version > max {
			max = snapshot.version
		}
	}
	return max
}

type revisionSchemaTemplate struct {
	id, tenantID, name                        string
	modelID, snapshotID                       string
	status                                    string
	protocolType, protocolConfig, voucherType string
	connectionType                            string
	autoRegister                              bool
	onlineConfig, tags, extras                string
	descriptionI18n, imageURL                 string
}

func mustInsertRevisionSchemaTemplate(t *testing.T, ctx context.Context, tx *sqldb.Tx, row revisionSchemaTemplate) {
	t.Helper()
	if row.status == "" {
		row.status = "DRAFT"
	}
	if row.protocolType == "" {
		row.protocolType = "MQTT"
	}
	if row.connectionType == "" {
		row.connectionType = "DIRECT"
	}
	if row.onlineConfig == "" {
		row.onlineConfig = `{}`
	}
	if row.tags == "" {
		row.tags = `[]`
	}
	if row.extras == "" {
		row.extras = `{}`
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO device_access_templates(
			id,tenant_id,name,thing_model_id,thing_model_snapshot_id,
			protocol_type,protocol_config,voucher_type,connection_type,auto_register,
			online_config,tags,extras,description_i18n,image_url,status
		)
		VALUES($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,NULLIF($7,'')::jsonb,$8,$9,$10,
		       $11::jsonb,$12::jsonb,$13::jsonb,NULLIF($14,'')::jsonb,NULLIF($15,''),$16)
	`, row.id, row.tenantID, row.name, row.modelID, row.snapshotID, row.protocolType, row.protocolConfig,
		row.voucherType, row.connectionType, row.autoRegister, row.onlineConfig, row.tags, row.extras,
		row.descriptionI18n, row.imageURL, row.status)
	if err != nil {
		t.Fatalf("insert template schema fixture: %v", err)
	}
}

func expectRevisionSchemaError(t *testing.T, ctx context.Context, tx *sqldb.Tx, savepoint, wantSQLState, query string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("create %s: %v", savepoint, err)
	}
	_, execErr := tx.Exec(ctx, query, args...)
	if execErr == nil {
		_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("statement unexpectedly succeeded; wanted SQLSTATE %s", wantSQLState)
	}
	var pgErr *pgconn.PgError
	if !errors.As(execErr, &pgErr) {
		_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("statement error %T is not a PostgreSQL error: %v", execErr, execErr)
	}
	if pgErr.Code != wantSQLState {
		_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("statement SQLSTATE=%s, want %s; message=%s", pgErr.Code, wantSQLState, pgErr.Message)
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("rollback %s after expected constraint error: %v", savepoint, err)
	}
	if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("release %s: %v", savepoint, err)
	}
}
