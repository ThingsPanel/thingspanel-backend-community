package devicemetadata

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

func TestResolveAccessTemplateModelBindingTx(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	userID := "access-template-model-" + uuid.NewString()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	trustedTenantID, err := mustTenantID()
	if err != nil || trustedTenantID != tenantID {
		t.Fatalf("trusted tenant = %q, %v", trustedTenantID, err)
	}

	t.Run("null selection", func(t *testing.T) {
		tx, err := beginTenantScopedSQLTx(ctx, trustedTenantID, false)
		if err != nil {
			t.Fatal(err)
		}
		binding, err := resolveAccessTemplateModelBindingTx(ctx, tx, trustedTenantID, json.RawMessage(`null`))
		if err != nil || binding != nil {
			_ = tx.Rollback()
			t.Fatalf("null selection = %#v, %v", binding, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("fixed revision and separate digests survive later publication", func(t *testing.T) {
		tx, err := beginTenantScopedSQLTx(ctx, trustedTenantID, false)
		if err != nil {
			t.Fatal(err)
		}
		modelID, snapshotIDs := seedAccessTemplateModelFixture(t, ctx, tx, trustedTenantID, "PUBLISHED", true, []seededSnapshot{
			{version: 1, canonical: true, content: []byte(`{"semantic":1}`), checksum: strings.Repeat("a", 64), resources: `{"revision":1}`, resourcesChecksum: strings.Repeat("b", 64)},
			{version: 2, canonical: true, content: []byte(`{"semantic":2}`), checksum: strings.Repeat("c", 64), resources: `{"revision":2}`, resourcesChecksum: strings.Repeat("d", 64)},
		})
		var beforeTXID, afterTXID int64
		var beforeTenant, afterTenant string
		if err := tx.QueryRow(ctx, `SELECT txid_current(), current_setting('app.current_tenant_id')`).Scan(&beforeTXID, &beforeTenant); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		selection := json.RawMessage(`{"modelId":"` + modelID + `","publishedRevision":1}`)
		binding, err := resolveAccessTemplateModelBindingTx(ctx, tx, trustedTenantID, selection)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("resolve selected revision: %v", err)
		}
		if err := tx.QueryRow(ctx, `SELECT txid_current(), current_setting('app.current_tenant_id')`).Scan(&afterTXID, &afterTenant); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if beforeTXID != afterTXID || beforeTenant != trustedTenantID || afterTenant != trustedTenantID {
			_ = tx.Rollback()
			t.Fatalf("helper changed transaction context: txid %d/%d tenant %q/%q", beforeTXID, afterTXID, beforeTenant, afterTenant)
		}
		if binding.ModelID != modelID || binding.SnapshotID != snapshotIDs[1] || binding.PublishedRevision != 1 || binding.SnapshotDigest != strings.Repeat("a", 64) || binding.ResourcesDigest != strings.Repeat("b", 64) || binding.SnapshotDigest == binding.ResourcesDigest {
			_ = tx.Rollback()
			t.Fatalf("binding did not preserve fixed revision and distinct digests: %#v", binding)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM thing_models WHERE tenant_id=$1 AND id=$2`, trustedTenantID, modelID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("fixture survived caller rollback: count=%d err=%v", count, err)
		}
	})

	for _, tc := range []struct {
		name           string
		modelTenant    string
		status         string
		canonicalModel bool
		snapshots      []seededSnapshot
		want           errs.ErrCode
	}{
		{name: "cross-tenant model is not found", modelTenant: testTenantID(), status: "PUBLISHED", canonicalModel: true, snapshots: []seededSnapshot{{version: 1, canonical: true, content: []byte(`{"v":1}`), checksum: strings.Repeat("a", 64), resources: `{}`, resourcesChecksum: strings.Repeat("b", 64)}}, want: errs.NotFound},
		{name: "archived model", status: "ARCHIVED", canonicalModel: true, snapshots: []seededSnapshot{{version: 1, canonical: true, content: []byte(`{"v":1}`), checksum: strings.Repeat("a", 64), resources: `{}`, resourcesChecksum: strings.Repeat("b", 64)}}, want: errs.FailedPrecondition},
		{name: "legacy noncanonical model", status: "PUBLISHED", canonicalModel: false, snapshots: []seededSnapshot{{version: 1, canonical: true, content: []byte(`{"v":1}`), checksum: strings.Repeat("a", 64), resources: `{}`, resourcesChecksum: strings.Repeat("b", 64)}}, want: errs.FailedPrecondition},
		{name: "legacy noncanonical version", status: "PUBLISHED", canonicalModel: true, snapshots: []seededSnapshot{{version: 1, canonical: false}}, want: errs.FailedPrecondition},
		{name: "incomplete resources snapshot", status: "PUBLISHED", canonicalModel: true, snapshots: []seededSnapshot{{version: 1, canonical: true, content: []byte(`{"v":1}`), checksum: strings.Repeat("a", 64)}}, want: errs.FailedPrecondition},
		{name: "missing fixed revision", status: "PUBLISHED", canonicalModel: true, snapshots: []seededSnapshot{{version: 2, canonical: true, content: []byte(`{"v":2}`), checksum: strings.Repeat("c", 64), resources: `{}`, resourcesChecksum: strings.Repeat("d", 64)}}, want: errs.NotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := beginTenantScopedSQLTx(ctx, trustedTenantID, false)
			if err != nil {
				t.Fatal(err)
			}
			modelTenant := tc.modelTenant
			if modelTenant == "" {
				modelTenant = trustedTenantID
			}
			modelID, _ := seedAccessTemplateModelFixture(t, ctx, tx, modelTenant, tc.status, tc.canonicalModel, tc.snapshots)
			selection := json.RawMessage(`{"modelId":"` + modelID + `","publishedRevision":1}`)
			binding, err := resolveAccessTemplateModelBindingTx(ctx, tx, trustedTenantID, selection)
			if errs.Code(err) != tc.want || binding != nil {
				_ = tx.Rollback()
				t.Fatalf("binding=%#v err=%v code=%v want=%v", binding, err, errs.Code(err), tc.want)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type seededSnapshot struct {
	version           int
	canonical         bool
	content           []byte
	checksum          string
	resources         string
	resourcesChecksum string
}

func seedAccessTemplateModelFixture(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID, status string, canonicalModel bool, snapshots []seededSnapshot) (string, map[int]string) {
	t.Helper()
	modelID := uuid.NewString()
	var canonical any
	if canonicalModel {
		canonical = `{"name":"fixture","fields":[]}`
	}
	if len(snapshots) == 0 {
		t.Fatal("fixture requires snapshot")
	}
	maxVersion := 0
	for _, s := range snapshots {
		if s.version > maxVersion {
			maxVersion = s.version
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO thing_models(id,tenant_id,name,status,current_snapshot_id,canonical_model,draft_revision,published_revision) VALUES($1,$2,$3,$4,NULL,$5,1,$6)`, modelID, tenantID, "binding-fixture-"+modelID, status, canonical, maxVersion); err != nil {
		t.Fatalf("insert model fixture: %v", err)
	}
	ids := map[int]string{}
	for _, s := range snapshots {
		id := uuid.NewString()
		ids[s.version] = id
		var canonicalContent any = s.content
		var checksum any = s.checksum
		var resourcesContent, resourcesRevision, resourcesDigest any
		if s.resources != "" {
			resourcesContent = s.resources
			resourcesRevision = 0
		}
		if s.resourcesChecksum != "" {
			resourcesDigest = s.resourcesChecksum
		}
		if !s.canonical {
			canonicalContent = nil
			checksum = nil
			resourcesContent = nil
			resourcesRevision = nil
			resourcesDigest = nil
		}
		if _, err := tx.Exec(ctx, `INSERT INTO thing_model_snapshots(id,thing_model_id,tenant_id,version,content,published_by,canonical_snapshot,canonical_content,canonical_checksum,resources_content,resources_revision,resources_checksum) VALUES($1,$2,$3,$4,'{}',$5,$6,$7,$8,$9,$10,$11)`, id, modelID, tenantID, s.version, testActorID(), s.canonical, canonicalContent, checksum, resourcesContent, resourcesRevision, resourcesDigest); err != nil {
			t.Fatalf("insert snapshot fixture: %v", err)
		}
	}
	if maxVersion > 0 {
		if _, err := tx.Exec(ctx, `UPDATE thing_models SET current_snapshot_id=$1 WHERE tenant_id=$2 AND id=$3`, ids[maxVersion], tenantID, modelID); err != nil {
			t.Fatalf("set current snapshot: %v", err)
		}
	}
	return modelID, ids
}
