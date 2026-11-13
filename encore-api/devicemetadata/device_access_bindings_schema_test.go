package devicemetadata

import (
	"context"
	"strings"
	"testing"
	"time"

	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

func TestDeviceAccessBindingsSchemaAndTenantReferences(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var columnCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='device_access_bindings'
	`).Scan(&columnCount); err != nil || columnCount != 14 {
		t.Fatalf("binding columns=%d err=%v, want 14 fields without copied ThingModel references", columnCount, err)
	}
	for _, constraint := range []struct{ name, reference string }{
		{"device_access_bindings_device_fk", "FOREIGN KEY (tenant_id, device_id) REFERENCES devices(tenant_id, id)"},
		{"device_access_bindings_template_fk", "FOREIGN KEY (tenant_id, template_id) REFERENCES device_access_templates(tenant_id, id)"},
		{"device_access_bindings_pending_operation_fk", "FOREIGN KEY (tenant_id, pending_operation_id) REFERENCES metadata_operations(tenant_id, id)"},
	} {
		var definition string
		if err := tx.QueryRow(ctx, `
			SELECT pg_get_constraintdef(oid)
			FROM pg_catalog.pg_constraint
			WHERE conrelid='device_access_bindings'::regclass AND conname=$1
		`, constraint.name).Scan(&definition); err != nil || !strings.Contains(definition, constraint.reference) {
			t.Fatalf("constraint %s definition=%q err=%v", constraint.name, definition, err)
		}
	}
	for _, index := range []string{
		"uq_device_access_bindings_runtime_instance_identity",
		"uq_device_access_bindings_runtime_no_instance_identity",
	} {
		var isUnique, hasPredicate bool
		if err := tx.QueryRow(ctx, `
			SELECT i.indisunique, i.indpred IS NOT NULL
			FROM pg_catalog.pg_index AS i
			WHERE i.indexrelid=to_regclass(current_schema() || '.' || $1)
		`, index).Scan(&isUnique, &hasPredicate); err != nil || !isUnique || !hasPredicate {
			t.Fatalf("runtime identity index %s unique=%v partial=%v err=%v", index, isUnique, hasPredicate, err)
		}
	}
	var identityTrigger bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_trigger AS tg
			WHERE tg.tgrelid='device_access_bindings'::regclass
			  AND tg.tgname='trg_device_access_bindings_identity_immutable'
			  AND NOT tg.tgisinternal
		)
	`).Scan(&identityTrigger); err != nil || !identityTrigger {
		t.Fatalf("binding identity immutability trigger missing: found=%v err=%v", identityTrigger, err)
	}

	tenantA, tenantB := testTenantID(), testTenantID()
	deviceA := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantA)
	deviceA2 := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantA)
	deviceA3 := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantA)
	deviceA4 := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantA)
	deviceA5 := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantA)
	deviceB := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantB)
	templateID := uuid.NewString()
	mustInsertRevisionSchemaTemplate(t, ctx, tx, revisionSchemaTemplate{
		id: templateID, tenantID: tenantA, name: "binding-schema-template-" + uuid.NewString(),
	})
	revision := templateID // Ordinary template identity, not a template revision.

	unconfigured := validDeviceAccessBindingSchemaRow(tenantA, deviceA)
	if err := insertDeviceAccessBindingSchemaRow(ctx, tx, unconfigured); err != nil {
		t.Fatalf("insert unconfigured device binding: %v", err)
	}
	var parameters, updatedBy string
	var generation int64
	var createdAt, updatedAt time.Time
	var configNull, runtimeNull bool
	if err := tx.QueryRow(ctx, `
		SELECT device_parameters::text, configuration_generation, updated_by,
		       created_at, updated_at,
		       template_id IS NULL AND direct_access IS NULL,
		       runtime_provider_id IS NULL AND service_instance_id IS NULL
		FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2
	`, tenantA, deviceA).Scan(&parameters, &generation, &updatedBy, &createdAt, &updatedAt, &configNull, &runtimeNull); err != nil {
		t.Fatal(err)
	}
	if parameters != `{}` || generation != 1 || updatedBy == "" || createdAt.IsZero() || updatedAt.IsZero() || !configNull || !runtimeNull {
		t.Fatalf("unconfigured binding defaults invalid: parameters=%s generation=%d actor=%q times=%v/%v nulls=%v/%v",
			parameters, generation, updatedBy, createdAt, updatedAt, configNull, runtimeNull)
	}
	var serverUpdatedAt time.Time
	if err := tx.QueryRow(ctx, `
		UPDATE device_access_bindings SET updated_at='2000-01-01T00:00:00Z'
		WHERE tenant_id=$1 AND device_id=$2 RETURNING updated_at
	`, tenantA, deviceA).Scan(&serverUpdatedAt); err != nil || !serverUpdatedAt.After(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("binding updated_at was not server controlled: value=%s err=%v", serverUpdatedAt, err)
	}
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_device_primary_key", "23505", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, validDeviceAccessBindingSchemaRow(tenantA, deviceA))
	})

	direct := validDeviceAccessBindingSchemaRow(tenantA, deviceA2)
	direct.directAccess = `{"access":{},"externalDeviceId":null}`
	direct.deviceParameters = `{"region":"west"}`
	if err := insertDeviceAccessBindingSchemaRow(ctx, tx, direct); err != nil {
		t.Fatalf("insert direct access binding: %v", err)
	}

	templateBinding := validDeviceAccessBindingSchemaRow(tenantA, deviceA3)
	templateBinding.templateID = revision
	templateBinding.deviceParameters = `{"interval":30}`
	if err := insertDeviceAccessBindingSchemaRow(ctx, tx, templateBinding); err != nil {
		t.Fatalf("insert template binding: %v", err)
	}
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_configuration_requires_generation", "23514", func() error {
		_, err := tx.Exec(ctx, `
			UPDATE device_access_bindings SET device_parameters='{"interval":60}'::jsonb
			WHERE tenant_id=$1 AND device_id=$2
		`, tenantA, deviceA3)
		return err
	})
	if _, err := tx.Exec(ctx, `
		UPDATE device_access_bindings
		SET device_parameters='{"interval":60}'::jsonb, configuration_generation=2
		WHERE tenant_id=$1 AND device_id=$2
	`, tenantA, deviceA3); err != nil {
		t.Fatalf("configuration update with newer generation: %v", err)
	}
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_generation_cannot_decrease", "23514", func() error {
		_, err := tx.Exec(ctx, `
			UPDATE device_access_bindings SET configuration_generation=1
			WHERE tenant_id=$1 AND device_id=$2
		`, tenantA, deviceA3)
		return err
	})
	operationID := insertMetadataOperationFixture(t, ctx, tx, tenantA, "device_activation")
	if _, err := tx.Exec(ctx, `
		UPDATE device_access_bindings SET pending_operation_id=$1
		WHERE tenant_id=$2 AND device_id=$3
	`, operationID, tenantA, deviceA3); err != nil {
		t.Fatalf("same-generation operation reference update: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE device_access_bindings
		SET runtime_provider_id=$1, runtime_namespace='generation-test', runtime_external_id=$2
		WHERE tenant_id=$3 AND device_id=$4
	`, uuid.NewString(), uuid.NewString(), tenantA, deviceA3); err != nil {
		t.Fatalf("same-generation runtime identity confirmation: %v", err)
	}

	crossTenantDevice := validDeviceAccessBindingSchemaRow(tenantB, deviceA)
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_cross_tenant_device_fk", "23503", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, crossTenantDevice)
	})
	crossTenantTemplate := validDeviceAccessBindingSchemaRow(tenantB, deviceB)
	crossTenantTemplate.templateID = revision
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_cross_tenant_template_fk", "23503", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, crossTenantTemplate)
	})
	missingDevice := validDeviceAccessBindingSchemaRow(tenantA, uuid.NewString())
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_missing_device_fk", "23503", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, missingDevice)
	})
	missingTemplate := validDeviceAccessBindingSchemaRow(tenantA, deviceA4)
	missingTemplate.templateID = uuid.NewString()
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_missing_template_fk", "23503", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, missingTemplate)
	})
	missingOperation := validDeviceAccessBindingSchemaRow(tenantA, deviceA5)
	missingOperation.pendingOperationID = uuid.NewString()
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_missing_operation_fk", "23503", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, missingOperation)
	})
	foreignTenantOperationID := insertMetadataOperationFixture(t, ctx, tx, tenantB, "device_activation")
	crossTenantOperation := validDeviceAccessBindingSchemaRow(tenantA, deviceA5)
	crossTenantOperation.pendingOperationID = foreignTenantOperationID
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_cross_tenant_operation_fk", "23503", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, crossTenantOperation)
	})

	expectDeviceIdentitySQLState(t, ctx, tx, "binding_tenant_immutable", "23514", func() error {
		_, err := tx.Exec(ctx, `UPDATE device_access_bindings SET tenant_id=$1 WHERE tenant_id=$2 AND device_id=$3`, tenantB, tenantA, deviceA)
		return err
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_device_immutable", "23514", func() error {
		_, err := tx.Exec(ctx, `UPDATE device_access_bindings SET device_id=$1 WHERE tenant_id=$2 AND device_id=$3`, deviceA2, tenantA, deviceA)
		return err
	})
}

func TestDeviceAccessTemplateCurrentModelSelectionUsesTenantSnapshot(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	tenantA, tenantB := testTenantID(), testTenantID()
	modelA, snapshotsA := seedRevisionSchemaModel(t, ctx, tx, tenantA, "binding-current-model-", "PUBLISHED", []revisionSchemaSnapshot{
		{version: 7, canonical: true, semanticDigest: strings.Repeat("a", 64), resourcesDigest: strings.Repeat("b", 64)},
		{version: 8, canonical: true, semanticDigest: strings.Repeat("c", 64), resourcesDigest: strings.Repeat("d", 64)},
	})
	modelB, _ := seedRevisionSchemaModel(t, ctx, tx, tenantB, "binding-foreign-model-", "PUBLISHED", []revisionSchemaSnapshot{
		{version: 1, canonical: true, semanticDigest: strings.Repeat("e", 64), resourcesDigest: strings.Repeat("f", 64)},
	})
	templateID := uuid.NewString()
	mustInsertRevisionSchemaTemplate(t, ctx, tx, revisionSchemaTemplate{id: templateID, tenantID: tenantA, name: "current-binding-" + templateID})
	if _, err := tx.Exec(ctx, `UPDATE device_access_templates SET configuration=jsonb_build_object('model',jsonb_build_object('modelId',$3::text,'publishedRevision',7)) WHERE tenant_id=$1 AND id=$2`, tenantA, templateID, modelA); err != nil {
		t.Fatalf("select current template ThingModel version 7: %v", err)
	}
	var selectedModel, selectedSnapshot string
	if err := tx.QueryRow(ctx, `SELECT thing_model_id::text,thing_model_snapshot_id::text FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, tenantA, templateID).Scan(&selectedModel, &selectedSnapshot); err != nil {
		t.Fatal(err)
	}
	if selectedModel != modelA || selectedSnapshot != snapshotsA[7] {
		t.Fatalf("template current model references=%s/%s, want %s/%s", selectedModel, selectedSnapshot, modelA, snapshotsA[7])
	}
	expectDeviceIdentitySQLState(t, ctx, tx, "template_cross_tenant_model_selection", "23503", func() error {
		_, err := tx.Exec(ctx, `UPDATE device_access_templates SET configuration=jsonb_build_object('model',jsonb_build_object('modelId',$3::text,'publishedRevision',1)) WHERE tenant_id=$1 AND id=$2`, tenantA, templateID, modelB)
		return err
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "template_missing_published_model_version", "23503", func() error {
		_, err := tx.Exec(ctx, `UPDATE device_access_templates SET configuration=jsonb_build_object('model',jsonb_build_object('modelId',$3::text,'publishedRevision',99)) WHERE tenant_id=$1 AND id=$2`, tenantA, templateID, modelA)
		return err
	})
	if _, err := tx.Exec(ctx, `UPDATE device_access_templates SET configuration='{"model":null}'::jsonb WHERE tenant_id=$1 AND id=$2`, tenantA, templateID); err != nil {
		t.Fatalf("clear current template ThingModel selection: %v", err)
	}
	var modelNull, snapshotNull bool
	if err := tx.QueryRow(ctx, `SELECT thing_model_id IS NULL,thing_model_snapshot_id IS NULL FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, tenantA, templateID).Scan(&modelNull, &snapshotNull); err != nil || !modelNull || !snapshotNull {
		t.Fatalf("null model selection did not clear template references: model=%v snapshot=%v err=%v", modelNull, snapshotNull, err)
	}
}

func TestDeviceAccessBindingsChecksAndGlobalRuntimeUniqueness(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	tenantA, tenantB := testTenantID(), testTenantID()
	deviceA1 := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantA)
	deviceA2 := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantA)
	deviceA3 := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantA)
	deviceA4 := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantA)
	deviceB1 := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantB)
	deviceB2 := insertDeviceAccessBindingSchemaDevice(t, ctx, tx, tenantB)
	templateID := uuid.NewString()
	mustInsertRevisionSchemaTemplate(t, ctx, tx, revisionSchemaTemplate{
		id: templateID, tenantID: tenantA, name: "binding-check-template-" + uuid.NewString(),
	})
	revision := templateID // Ordinary template identity, not a template revision.

	invalid := validDeviceAccessBindingSchemaRow(tenantA, deviceA1)
	invalid.directAccess = `[]`
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_direct_access_object", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})
	invalid = validDeviceAccessBindingSchemaRow(tenantA, deviceA1)
	invalid.directAccess = `{}`
	invalid.templateID = revision
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_config_exclusive", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})
	invalid = validDeviceAccessBindingSchemaRow(tenantA, deviceA1)
	invalid.deviceParameters = `[]`
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_parameters_object", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})
	invalid = validDeviceAccessBindingSchemaRow(tenantA, deviceA1)
	invalid.deviceParameters = deviceIdentityJSONMap(101)
	invalid.directAccess = `{}`
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_parameters_count", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})
	invalid = validDeviceAccessBindingSchemaRow(tenantA, deviceA1)
	invalid.deviceParameters = `{"x":"` + strings.Repeat("v", 65536) + `"}`
	invalid.directAccess = `{}`
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_parameters_size", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})
	invalid = validDeviceAccessBindingSchemaRow(tenantA, deviceA1)
	invalid.deviceParameters = `{"nonEmpty":true}`
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_unconfigured_parameters", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})
	invalid = validDeviceAccessBindingSchemaRow(tenantA, deviceA1)
	invalid.configurationGeneration = 0
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_generation_positive", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})
	invalid = validDeviceAccessBindingSchemaRow(tenantA, deviceA1)
	invalid.configurationGeneration = 9007199254740992
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_generation_safe_range", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})
	invalid = validDeviceAccessBindingSchemaRow(tenantA, deviceA1)
	invalid.updatedBy = " "
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_updated_by_nonempty", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})

	providerID := uuid.NewString()
	withInstance := validDeviceAccessBindingSchemaRow(tenantA, deviceA1)
	withInstance.directAccess = `{}`
	withInstance.runtimeProviderID = providerID
	withInstance.serviceInstanceID = "instance-1"
	withInstance.runtimeNamespace = "namespace-a"
	withInstance.runtimeExternalID = "device-shared"
	if err := insertDeviceAccessBindingSchemaRow(ctx, tx, withInstance); err != nil {
		t.Fatalf("insert complete confirmed runtime identity: %v", err)
	}
	otherInstance := validDeviceAccessBindingSchemaRow(tenantA, deviceA2)
	otherInstance.directAccess = `{}`
	otherInstance.runtimeProviderID = providerID
	otherInstance.serviceInstanceID = "instance-2"
	otherInstance.runtimeNamespace = "namespace-a"
	otherInstance.runtimeExternalID = "device-shared"
	if err := insertDeviceAccessBindingSchemaRow(ctx, tx, otherInstance); err != nil {
		t.Fatalf("same external ID in a distinct service instance should be allowed: %v", err)
	}
	duplicateCrossTenantInstance := validDeviceAccessBindingSchemaRow(tenantB, deviceB1)
	duplicateCrossTenantInstance.directAccess = `{}`
	duplicateCrossTenantInstance.runtimeProviderID = providerID
	duplicateCrossTenantInstance.serviceInstanceID = "instance-1"
	duplicateCrossTenantInstance.runtimeNamespace = "namespace-a"
	duplicateCrossTenantInstance.runtimeExternalID = "device-shared"
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_runtime_instance_global_unique", "23505", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, duplicateCrossTenantInstance)
	})

	withoutInstance := validDeviceAccessBindingSchemaRow(tenantA, deviceA3)
	withoutInstance.directAccess = `{}`
	withoutInstance.runtimeProviderID = providerID
	withoutInstance.runtimeNamespace = "namespace-b"
	withoutInstance.runtimeExternalID = "device-no-instance"
	if err := insertDeviceAccessBindingSchemaRow(ctx, tx, withoutInstance); err != nil {
		t.Fatalf("insert confirmed runtime identity without instance: %v", err)
	}
	duplicateCrossTenantNoInstance := validDeviceAccessBindingSchemaRow(tenantB, deviceB2)
	duplicateCrossTenantNoInstance.directAccess = `{}`
	duplicateCrossTenantNoInstance.runtimeProviderID = providerID
	duplicateCrossTenantNoInstance.runtimeNamespace = "namespace-b"
	duplicateCrossTenantNoInstance.runtimeExternalID = "device-no-instance"
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_runtime_no_instance_global_unique", "23505", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, duplicateCrossTenantNoInstance)
	})

	invalid = validDeviceAccessBindingSchemaRow(tenantA, deviceA4)
	invalid.directAccess = `{}`
	invalid.runtimeProviderID = providerID
	invalid.runtimeNamespace = "namespace-c"
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_runtime_partial_triplet", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})
	invalid = validDeviceAccessBindingSchemaRow(tenantA, deviceA4)
	invalid.directAccess = `{}`
	invalid.serviceInstanceID = "orphan-instance"
	expectDeviceIdentitySQLState(t, ctx, tx, "binding_service_instance_without_runtime", "23514", func() error {
		return insertDeviceAccessBindingSchemaRow(ctx, tx, invalid)
	})
}

type deviceAccessBindingSchemaRow struct {
	tenantID                string
	deviceID                string
	templateID              any
	directAccess            any
	deviceParameters        string
	configurationGeneration int64
	pendingOperationID      any
	runtimeProviderID       any
	serviceInstanceID       any
	runtimeNamespace        any
	runtimeExternalID       any
	updatedBy               string
}

func validDeviceAccessBindingSchemaRow(tenantID, deviceID string) deviceAccessBindingSchemaRow {
	return deviceAccessBindingSchemaRow{
		tenantID: tenantID, deviceID: deviceID, deviceParameters: `{}`,
		configurationGeneration: 1, updatedBy: "binding-schema-test-actor",
	}
}

func insertDeviceAccessBindingSchemaRow(ctx context.Context, tx *sqldb.Tx, row deviceAccessBindingSchemaRow) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO device_access_bindings(
			tenant_id, device_id, template_id, direct_access,
			device_parameters, configuration_generation, pending_operation_id,
			runtime_provider_id, service_instance_id, runtime_namespace,
			runtime_external_id, updated_by
		) VALUES(
			$1,$2,$3,$4::jsonb,$5::jsonb,$6,$7,$8,$9,$10,$11,$12
		)
	`, row.tenantID, row.deviceID, row.templateID, row.directAccess,
		row.deviceParameters, row.configurationGeneration, row.pendingOperationID,
		row.runtimeProviderID, row.serviceInstanceID, row.runtimeNamespace,
		row.runtimeExternalID, row.updatedBy)
	return err
}

func insertDeviceAccessBindingSchemaDevice(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID string) string {
	t.Helper()
	row := validDeviceIdentitySchemaRow(tenantID)
	row.deviceNumber = deviceIdentityString("binding-device-" + uuid.NewString())
	row.name = deviceIdentityString("binding device")
	row.deviceKind = deviceIdentityString("standalone")
	row.activationState = deviceIdentityString("inactive")
	row.activationTimeSource = deviceIdentityString("not_activated")
	if err := insertDeviceIdentitySchemaRow(ctx, tx, row); err != nil {
		t.Fatalf("insert binding device fixture: %v", err)
	}
	return row.id
}
