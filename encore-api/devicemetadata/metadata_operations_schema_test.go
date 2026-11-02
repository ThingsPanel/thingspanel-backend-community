package devicemetadata

import (
	"context"
	"errors"
	"strings"
	"testing"

	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMetadataOperationsSchemaConstraints(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var operationsExists, itemsExists, artifactsExists bool
	if err := tx.QueryRow(ctx, `
		SELECT to_regclass(current_schema() || '.metadata_operations') IS NOT NULL,
		       to_regclass(current_schema() || '.metadata_operation_items') IS NOT NULL,
		       to_regclass(current_schema() || '.metadata_artifacts') IS NOT NULL
	`).Scan(&operationsExists, &itemsExists, &artifactsExists); err != nil {
		t.Fatal(err)
	}
	if !operationsExists || !itemsExists || !artifactsExists {
		t.Fatalf("metadata operation tables missing: operations=%v items=%v artifacts=%v", operationsExists, itemsExists, artifactsExists)
	}

	tenantA, tenantB := testTenantID(), testTenantID()
	for _, kind := range []string{
		"device_activation", "device_deletion", "device_renumber", "administrative_state",
		"template_binding", "gateway_binding", "credential_change", "batch_create",
		"batch_template_binding", "device_import", "device_export", "template_import",
		"template_export", "gateway_binding_batch",
	} {
		insertMetadataOperationFixture(t, ctx, tx, tenantA, kind)
	}

	operationID := insertMetadataOperationFixture(t, ctx, tx, tenantA, "device_import")
	sameTenantOtherOperationID := insertMetadataOperationFixture(t, ctx, tx, tenantA, "device_export")
	otherTenantOperationID := insertMetadataOperationFixture(t, ctx, tx, tenantB, "device_export")
	artifactID := insertMetadataArtifactFixture(t, ctx, tx, tenantA, operationID, "import_source", strings.Repeat("b", 64))
	if _, err := tx.Exec(ctx, `UPDATE metadata_operations SET input_artifact_id=$1 WHERE tenant_id=$2 AND id=$3`, artifactID, tenantA, operationID); err != nil {
		t.Fatalf("attach same-tenant input artifact: %v", err)
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS metadata_operations_input_artifact_fk, metadata_artifacts_operation_fk IMMEDIATE`); err != nil {
		t.Fatalf("make circular artifact FKs immediate for negative checks: %v", err)
	}

	itemKey := "row-0001"
	if _, err := tx.Exec(ctx, `
		INSERT INTO metadata_operation_items(tenant_id,operation_id,item_key,input_digest,source_row)
		VALUES($1,$2,$3,$4,1)
	`, tenantA, operationID, itemKey, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("insert valid operation item: %v", err)
	}

	expectMetadataOperationSchemaError(t, ctx, tx, "sp_kind_reading_export", "23514", `
		INSERT INTO metadata_operations(id,tenant_id,created_by,updated_by,kind,originating_operation_name,originating_subject_id,request_digest)
		VALUES($1,$2,'schema-test','schema-test','reading_export','exportDeviceReadings','subject',$3)
	`, uuid.NewString(), tenantA, strings.Repeat("a", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_bad_state", "23514", `
		UPDATE metadata_operations SET state='queued' WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_bad_digest", "23514", `
		INSERT INTO metadata_operations(id,tenant_id,created_by,updated_by,kind,originating_operation_name,originating_subject_id,request_digest)
		VALUES($1,$2,'schema-test','schema-test','device_import','importDevice','subject',$3)
	`, uuid.NewString(), tenantA, strings.Repeat("A", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_resource_pair", "23514", `
		UPDATE metadata_operations SET resource_kind='device' WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_empty_resource_kind", "23514", `
		UPDATE metadata_operations SET resource_kind=' ',resource_id=$1 WHERE tenant_id=$2 AND id=$3
	`, uuid.NewString(), tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_completed_over_total", "23514", `
		UPDATE metadata_operations SET total_items=1,completed_items=2 WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_failed_over_completed", "23514", `
		UPDATE metadata_operations SET completed_items=1,failed_items=2 WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_lease_pair", "23514", `
		UPDATE metadata_operations SET lease_owner='worker-a' WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_failure_json_shape", "23514", `
		UPDATE metadata_operations SET failure='[]'::jsonb WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_failure_problem_required_fields", "23514", `
		UPDATE metadata_operations SET failure='{}'::jsonb WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_problem_errors_scalar", "23514", `
		UPDATE metadata_operations SET failure='{"type":"about:blank","title":"Failed","status":500,"detail":"failed","instance":"/operations/test","code":"FAILED","requestId":"schema-test","errors":"invalid"}'::jsonb
		WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_problem_conflicts_scalar", "23514", `
		UPDATE metadata_operations SET failure='{"type":"about:blank","title":"Failed","status":500,"detail":"failed","instance":"/operations/test","code":"FAILED","requestId":"schema-test","conflicts":{}}'::jsonb
		WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_result_json_shape", "23514", `
		UPDATE metadata_operations SET result='[]'::jsonb WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_result_unknown_field", "23514", `
		UPDATE metadata_operations SET result='{"secret":"not allowed"}'::jsonb WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	if _, err := tx.Exec(ctx, `
		UPDATE metadata_operations SET result='{"resourceIds":[],"downloadId":null,"checksum":null}'::jsonb
		WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID); err != nil {
		t.Fatalf("store valid non-secret Operation.result shape: %v", err)
	}
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_failed_requires_failure", "23514", `
		UPDATE metadata_operations SET state='failed',finished_at=NOW() WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_finished_required", "23514", `
		UPDATE metadata_operations SET state='cancelled' WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_success_requires_complete", "23514", `
		UPDATE metadata_operations SET total_items=1,state='succeeded',finished_at=NOW() WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_cross_tenant_input_artifact", "23503", `
		UPDATE metadata_operations SET input_artifact_id=$1 WHERE tenant_id=$2 AND id=$3
	`, artifactID, tenantB, otherTenantOperationID)

	failedOperationID := insertMetadataOperationFixture(t, ctx, tx, tenantA, "device_activation")
	if _, err := tx.Exec(ctx, `
		UPDATE metadata_operations SET state='failed',failure='{"type":"about:blank","title":"Failed","status":500,"detail":"failed","instance":"/operations/test","code":"FAILED","requestId":"schema-test"}'::jsonb,finished_at=NOW()
		WHERE tenant_id=$1 AND id=$2
	`, tenantA, failedOperationID); err != nil {
		t.Fatalf("enter failed terminal state: %v", err)
	}
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_operation_terminal_regression", "23514", `
		UPDATE metadata_operations SET state='running',finished_at=NULL WHERE tenant_id=$1 AND id=$2
	`, tenantA, failedOperationID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_operation_tenant_immutable", "23514", `
		UPDATE metadata_operations SET tenant_id=$1 WHERE tenant_id=$2 AND id=$3
	`, tenantB, tenantA, operationID)

	expectMetadataOperationSchemaError(t, ctx, tx, "sp_item_cross_tenant", "23503", `
		INSERT INTO metadata_operation_items(tenant_id,operation_id,item_key,input_digest)
		VALUES($1,$2,'cross-tenant',$3)
	`, tenantB, operationID, strings.Repeat("c", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_item_bad_row", "23514", `
		INSERT INTO metadata_operation_items(tenant_id,operation_id,item_key,input_digest,source_row)
		VALUES($1,$2,'bad-row',$3,0)
	`, tenantA, operationID, strings.Repeat("c", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_item_bad_state", "23514", `
		INSERT INTO metadata_operation_items(tenant_id,operation_id,item_key,input_digest,state)
		VALUES($1,$2,'bad-state',$3,'running')
	`, tenantA, operationID, strings.Repeat("c", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_item_bad_digest", "23514", `
		INSERT INTO metadata_operation_items(tenant_id,operation_id,item_key,input_digest)
		VALUES($1,$2,'bad-digest',$3)
	`, tenantA, operationID, strings.Repeat("C", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_item_failed_requires_error", "23514", `
		INSERT INTO metadata_operation_items(tenant_id,operation_id,item_key,input_digest,state)
		VALUES($1,$2,'failed-no-problem',$3,'failed')
	`, tenantA, operationID, strings.Repeat("c", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_item_error_problem_shape", "23514", `
		UPDATE metadata_operation_items SET error='{}'::jsonb WHERE tenant_id=$1 AND operation_id=$2 AND item_key=$3
	`, tenantA, operationID, itemKey)
	if _, err := tx.Exec(ctx, `
		UPDATE metadata_operation_items SET state='succeeded' WHERE tenant_id=$1 AND operation_id=$2 AND item_key=$3
	`, tenantA, operationID, itemKey); err != nil {
		t.Fatalf("enter succeeded item terminal state: %v", err)
	}
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_item_terminal_regression", "23514", `
		UPDATE metadata_operation_items SET state='pending' WHERE tenant_id=$1 AND operation_id=$2 AND item_key=$3
	`, tenantA, operationID, itemKey)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_item_tenant_immutable", "23514", `
		UPDATE metadata_operation_items SET tenant_id=$1 WHERE tenant_id=$2 AND operation_id=$3 AND item_key=$4
	`, tenantB, tenantA, operationID, itemKey)

	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_cross_tenant", "23503", `
		INSERT INTO metadata_artifacts(id,tenant_id,created_by,operation_id,purpose,file_owner,file_object_id,file_name,content_type,size_bytes,checksum)
		VALUES($1,$2,'schema-test',$3,'import_source','files','object-cross-tenant','input.json','application/json',0,$4)
	`, uuid.NewString(), tenantB, operationID, strings.Repeat("d", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_bad_purpose", "23514", `
		INSERT INTO metadata_artifacts(id,tenant_id,created_by,operation_id,purpose,file_owner,file_object_id,file_name,content_type,size_bytes,checksum)
		VALUES($1,$2,'schema-test',$3,'secret_export','files','object-bad-purpose','out.json','application/json',0,$4)
	`, uuid.NewString(), tenantA, operationID, strings.Repeat("d", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_bad_state", "23514", `
		INSERT INTO metadata_artifacts(id,tenant_id,created_by,operation_id,purpose,file_owner,file_object_id,file_name,content_type,size_bytes,checksum,state)
		VALUES($1,$2,'schema-test',$3,'report','files','object-bad-state','out.json','application/json',0,$4,'available')
	`, uuid.NewString(), tenantA, operationID, strings.Repeat("d", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_bad_size", "23514", `
		INSERT INTO metadata_artifacts(id,tenant_id,created_by,operation_id,purpose,file_owner,file_object_id,file_name,content_type,size_bytes,checksum)
		VALUES($1,$2,'schema-test',$3,'report','files','object-bad-size','out.json','application/json',-1,$4)
	`, uuid.NewString(), tenantA, operationID, strings.Repeat("d", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_bad_checksum", "23514", `
		INSERT INTO metadata_artifacts(id,tenant_id,created_by,operation_id,purpose,file_owner,file_object_id,file_name,content_type,size_bytes,checksum)
		VALUES($1,$2,'schema-test',$3,'report','files','object-bad-checksum','out.json','application/json',0,$4)
	`, uuid.NewString(), tenantA, operationID, strings.Repeat("D", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_path_name", "23514", `
		INSERT INTO metadata_artifacts(id,tenant_id,created_by,operation_id,purpose,file_owner,file_object_id,file_name,content_type,size_bytes,checksum)
		VALUES($1,$2,'schema-test',$3,'report','files','object-path-name','../out.json','application/json',0,$4)
	`, uuid.NewString(), tenantA, operationID, strings.Repeat("e", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_deleted_state_pair", "23514", `
		UPDATE metadata_artifacts SET state='deleted' WHERE tenant_id=$1 AND id=$2
	`, tenantA, artifactID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_tenant_immutable", "23514", `
		UPDATE metadata_artifacts SET tenant_id=$1 WHERE tenant_id=$2 AND id=$3
	`, tenantB, tenantA, artifactID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_object_id_immutable", "23514", `
		UPDATE metadata_artifacts SET file_object_id='replacement-object' WHERE tenant_id=$1 AND id=$2
	`, tenantA, artifactID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_checksum_immutable", "23514", `
		UPDATE metadata_artifacts SET checksum=$1 WHERE tenant_id=$2 AND id=$3
	`, strings.Repeat("f", 64), tenantA, artifactID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_operation_immutable", "23514", `
		UPDATE metadata_artifacts SET operation_id=$1 WHERE tenant_id=$2 AND id=$3
	`, sameTenantOtherOperationID, tenantA, artifactID)
	if _, err := tx.Exec(ctx, `
		UPDATE metadata_artifacts SET state='ready',expires_at=NOW()+INTERVAL '1 day'
		WHERE tenant_id=$1 AND id=$2
	`, tenantA, artifactID); err != nil {
		t.Fatalf("artifact staged-to-ready transition failed: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE metadata_artifacts SET state='expired',expires_at=NOW()
		WHERE tenant_id=$1 AND id=$2
	`, tenantA, artifactID); err != nil {
		t.Fatalf("artifact ready-to-expired transition failed: %v", err)
	}
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_expired_to_ready", "23514", `
		UPDATE metadata_artifacts SET state='ready' WHERE tenant_id=$1 AND id=$2
	`, tenantA, artifactID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_expired_to_staged", "23514", `
		UPDATE metadata_artifacts SET state='staged' WHERE tenant_id=$1 AND id=$2
	`, tenantA, artifactID)
	if _, err := tx.Exec(ctx, `
		UPDATE metadata_artifacts SET state='deleted',deleted_at=NOW()
		WHERE tenant_id=$1 AND id=$2
	`, tenantA, artifactID); err != nil {
		t.Fatalf("artifact expired-to-deleted transition failed: %v", err)
	}
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_deleted_to_ready", "23514", `
		UPDATE metadata_artifacts SET state='ready' WHERE tenant_id=$1 AND id=$2
	`, tenantA, artifactID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_deleted_to_staged", "23514", `
		UPDATE metadata_artifacts SET state='staged' WHERE tenant_id=$1 AND id=$2
	`, tenantA, artifactID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_dedupe_unique", "23505", `
		INSERT INTO metadata_artifacts(
			id,tenant_id,created_by,operation_id,purpose,file_owner,file_object_id,file_name,content_type,size_bytes,checksum
		)
		VALUES($1,$2,'schema-test',$3,'import_source','schema-file-owner','duplicate-object','duplicate.json','application/json',12,$4)
	`, uuid.NewString(), tenantA, operationID, strings.Repeat("b", 64))
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_artifact_delete_restrict", "23001", `
		DELETE FROM metadata_artifacts WHERE tenant_id=$1 AND id=$2
	`, tenantA, artifactID)
	expectMetadataOperationSchemaError(t, ctx, tx, "sp_operation_delete_restrict", "23001", `
		DELETE FROM metadata_operations WHERE tenant_id=$1 AND id=$2
	`, tenantA, operationID)
}

func insertMetadataOperationFixture(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID, kind string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO metadata_operations(
			id,tenant_id,created_by,updated_by,kind,originating_operation_name,originating_subject_id,request_digest
		)
		VALUES($1,$2,'schema-test','schema-test',$3,'schemaTestOperation','schema-test-subject',$4)
	`, id, tenantID, kind, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("insert metadata operation fixture kind=%q: %v", kind, err)
	}
	return id
}

func insertMetadataArtifactFixture(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID, operationID, purpose, checksum string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO metadata_artifacts(
			id,tenant_id,created_by,operation_id,purpose,file_owner,file_object_id,file_name,content_type,size_bytes,checksum
		)
		VALUES($1,$2,'schema-test',$3,$4,'schema-file-owner',$5,'input.json','application/json',12,$6)
	`, id, tenantID, operationID, purpose, "opaque-object-"+id, checksum); err != nil {
		t.Fatalf("insert metadata artifact fixture: %v", err)
	}
	return id
}

func expectMetadataOperationSchemaError(t *testing.T, ctx context.Context, tx *sqldb.Tx, savepoint, wantSQLState, query string, args ...any) {
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
		t.Fatalf("rollback %s: %v", savepoint, err)
	}
}
