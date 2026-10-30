package devicemetadata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDeviceIdentitySchemaMigrationPreservesS3LegacyRowsAndTenantFence(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	columns := map[string]struct {
		nullable     string
		defaultValue string
	}{
		"device_number":          {"YES", ""},
		"name":                   {"YES", ""},
		"device_kind":            {"YES", ""},
		"description":            {"YES", ""},
		"labels":                 {"NO", "'{}'::jsonb"},
		"tags":                   {"NO", "'[]'::jsonb"},
		"custom_fields":          {"NO", "'{}'::jsonb"},
		"location":               {"YES", ""},
		"batch_number":           {"YES", ""},
		"administrative_state":   {"NO", "'enabled'::character varying"},
		"activation_state":       {"YES", ""},
		"activation_time_source": {"YES", ""},
		"created_by":             {"YES", ""},
		"updated_by":             {"YES", ""},
		"deleted_by":             {"YES", ""},
		"resource_version":       {"NO", "1"},
		"updated_at":             {"NO", "now()"},
		"device_template_id":     {"YES", ""},
		"sn":                     {"YES", ""},
		"voucher":                {"YES", ""},
		"is_online":              {"NO", "0"},
		"parent_device_id":       {"YES", ""},
	}
	rows, err := tx.Query(ctx, `
		SELECT column_name, is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='devices'
		  AND column_name = ANY($1)
	`, stringKeys(columns))
	if err != nil {
		t.Fatalf("read device identity columns: %v", err)
	}
	seen := make(map[string]bool, len(columns))
	for rows.Next() {
		var name, nullable, defaultValue string
		if err := rows.Scan(&name, &nullable, &defaultValue); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		want := columns[name]
		if nullable != want.nullable || normalizeDeviceColumnDefault(defaultValue) != normalizeDeviceColumnDefault(want.defaultValue) {
			rows.Close()
			t.Errorf("column %s nullable/default=%q/%q want %q/%q", name, nullable, defaultValue, want.nullable, want.defaultValue)
		}
		seen[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	for name := range columns {
		if !seen[name] {
			t.Errorf("device schema column %q is missing", name)
		}
	}

	var deviceNumberCollation string
	if err := tx.QueryRow(ctx, `
		SELECT c.collname
		FROM pg_catalog.pg_attribute AS a
		JOIN pg_catalog.pg_class AS r ON r.oid=a.attrelid
		JOIN pg_catalog.pg_namespace AS n ON n.oid=r.relnamespace
		JOIN pg_catalog.pg_collation AS c ON c.oid=a.attcollation
		WHERE n.nspname=current_schema() AND r.relname='devices' AND a.attname='device_number'
	`).Scan(&deviceNumberCollation); err != nil || deviceNumberCollation != "C" {
		t.Fatalf("device_number collation=%q err=%v, want C", deviceNumberCollation, err)
	}
	var fullNumberUnique bool
	if err := tx.QueryRow(ctx, `
		SELECT i.indisunique AND i.indpred IS NULL
		       AND pg_get_indexdef(i.indexrelid) LIKE '%(tenant_id, device_number)%'
		FROM pg_catalog.pg_index AS i
		WHERE i.indexrelid=to_regclass(current_schema() || '.uq_devices_tenant_device_number')
	`).Scan(&fullNumberUnique); err != nil || !fullNumberUnique {
		t.Fatalf("tenant device_number index is not unique over tombstones: unique=%v err=%v", fullNumberUnique, err)
	}
	var immutableTrigger bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_trigger AS tg
			WHERE tg.tgrelid='devices'::regclass
			  AND tg.tgname='trg_tenant_id_immutable'
			  AND NOT tg.tgisinternal
		)
	`).Scan(&immutableTrigger); err != nil || !immutableTrigger {
		t.Fatalf("devices tenant immutability trigger missing: found=%v err=%v", immutableTrigger, err)
	}

	tenantA := testTenantID()
	legacyID := uuid.NewString()
	legacyActivatedAt := time.Date(2022, time.January, 2, 3, 4, 5, 0, time.UTC)
	if _, err := tx.Exec(ctx, `
		INSERT INTO devices (id, tenant_id, sn, activated_at)
		VALUES ($1, $2, $3, $4)
	`, legacyID, tenantA, "legacy-sn-"+uuid.NewString(), legacyActivatedAt); err != nil {
		t.Fatalf("insert S3 legacy device with retained old activated_at and no inferred target values: %v", err)
	}
	var gotLegacySN string
	var numberNull, nameNull, kindNull, stateNull, sourceNull, actorNull, templateNull bool
	var gotLegacyActivatedAt time.Time
	var resourceVersion int64
	var administrativeState, labels, tags, customFields string
	if err := tx.QueryRow(ctx, `
		SELECT sn, device_number IS NULL, name IS NULL, device_kind IS NULL,
		       activation_state IS NULL, activation_time_source IS NULL,
		       created_by IS NULL AND updated_by IS NULL, device_template_id IS NULL,
		       activated_at, resource_version, administrative_state,
		       labels::text, tags::text, custom_fields::text
		FROM devices WHERE tenant_id=$1 AND id=$2
	`, tenantA, legacyID).Scan(&gotLegacySN, &numberNull, &nameNull, &kindNull, &stateNull, &sourceNull, &actorNull, &templateNull,
		&gotLegacyActivatedAt, &resourceVersion, &administrativeState, &labels, &tags, &customFields); err != nil {
		t.Fatal(err)
	}
	if gotLegacySN == "" || !numberNull || !nameNull || !kindNull || !stateNull || !sourceNull || !actorNull || !templateNull ||
		!gotLegacyActivatedAt.Equal(legacyActivatedAt) || resourceVersion != 1 || administrativeState != "enabled" ||
		labels != `{}` || tags != `[]` || customFields != `{}` {
		t.Fatalf("S3 legacy/default row mismatch: sn=%q nulls=%v/%v/%v/%v/%v/%v/%v activatedAt=%s rv=%d admin=%q JSON=%s/%s/%s",
			gotLegacySN, numberNull, nameNull, kindNull, stateNull, sourceNull, actorNull, templateNull,
			gotLegacyActivatedAt, resourceVersion, administrativeState, labels, tags, customFields)
	}

	otherTenant := testTenantID()
	expectDeviceIdentitySQLState(t, ctx, tx, "tenant_immutable", "23514", func() error {
		_, err := tx.Exec(ctx, `UPDATE devices SET tenant_id=$1 WHERE tenant_id=$2 AND id=$3`, otherTenant, tenantA, legacyID)
		return err
	})
}

func TestDeviceIdentityTargetConstraintsAndS3Nullability(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	tenantA, tenantB := testTenantID(), testTenantID()
	base := validDeviceIdentitySchemaRow(tenantA)
	base.deviceNumber = deviceIdentityString("Case-Sensitive-01")
	base.name = deviceIdentityString("Device A")
	base.deviceKind = deviceIdentityString("standalone")
	base.activationState = deviceIdentityString("active")
	base.activatedAt = time.Date(2026, time.October, 26, 1, 2, 3, 0, time.UTC)
	base.activationTimeSource = deviceIdentityString("runtime_confirmation")
	base.createdBy = deviceIdentityString("synthetic-actor")
	base.updatedBy = deviceIdentityString("synthetic-actor")
	base.labels = `{"region":"east","site_1":"alpha"}`
	base.tags = `["edge","gateway"]`
	base.customFields = `{"enabled":true,"threshold":12.500000000000000001,"unknown":null}`
	base.location = deviceIdentityString(`{"latitude":31.2304,"longitude":121.4737,"address":"Shanghai"}`)
	base.batchNumber = deviceIdentityString("batch-01")
	if err := insertDeviceIdentitySchemaRow(ctx, tx, base); err != nil {
		t.Fatalf("insert valid target device: %v", err)
	}
	var targetLegacyColumnsNull bool
	if err := tx.QueryRow(ctx, `SELECT sn IS NULL AND device_template_id IS NULL FROM devices WHERE tenant_id=$1 AND id=$2`, tenantA, base.id).Scan(&targetLegacyColumnsNull); err != nil || !targetLegacyColumnsNull {
		t.Fatalf("target-only device required legacy sn/template data: null=%v err=%v", targetLegacyColumnsNull, err)
	}

	otherTenantNumber := validDeviceIdentitySchemaRow(tenantB)
	otherTenantNumber.deviceNumber = deviceIdentityString("Case-Sensitive-01")
	if err := insertDeviceIdentitySchemaRow(ctx, tx, otherTenantNumber); err != nil {
		t.Fatalf("same device number should be allowed in another tenant: %v", err)
	}
	caseVariant := validDeviceIdentitySchemaRow(tenantA)
	caseVariant.deviceNumber = deviceIdentityString("case-sensitive-01")
	if err := insertDeviceIdentitySchemaRow(ctx, tx, caseVariant); err != nil {
		t.Fatalf("C-collated business number must remain case-sensitive: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantA, base.id); err != nil {
		t.Fatalf("tombstone device for number-retention check: %v", err)
	}
	duplicateTombstoneNumber := validDeviceIdentitySchemaRow(tenantA)
	duplicateTombstoneNumber.deviceNumber = deviceIdentityString("Case-Sensitive-01")
	expectDeviceIdentitySQLState(t, ctx, tx, "number_unique_tombstone", "23505", func() error {
		return insertDeviceIdentitySchemaRow(ctx, tx, duplicateTombstoneNumber)
	})

	invalidNumbers := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"leading_space", " leading"},
		{"trailing_space", "trailing "},
		{"tab", "leading\t"},
		{"ascii_control", "mid\x01dle"},
		{"c1_control", "mid" + string(rune(0x85)) + "dle"},
		{"unicode_edge_space", string(rune(0x3000)) + "edge"},
		{"too_long", strings.Repeat("n", 129)},
	}
	for _, tc := range invalidNumbers {
		t.Run("device_number/"+tc.name, func(t *testing.T) {
			row := validDeviceIdentitySchemaRow(tenantA)
			row.deviceNumber = deviceIdentityString(tc.value)
			wantState := "23514"
			if tc.name == "too_long" {
				wantState = "22001"
			}
			expectDeviceIdentitySQLState(t, ctx, tx, "number_"+tc.name, wantState, func() error {
				return insertDeviceIdentitySchemaRow(ctx, tx, row)
			})
		})
	}

	invalidNames := validDeviceIdentitySchemaRow(tenantA)
	invalidNames.name = deviceIdentityString("")
	expectDeviceIdentitySQLState(t, ctx, tx, "name_empty", "23514", func() error { return insertDeviceIdentitySchemaRow(ctx, tx, invalidNames) })
	invalidKind := validDeviceIdentitySchemaRow(tenantA)
	invalidKind.deviceKind = deviceIdentityString("sensor")
	expectDeviceIdentitySQLState(t, ctx, tx, "kind_enum", "23514", func() error { return insertDeviceIdentitySchemaRow(ctx, tx, invalidKind) })

	activationCases := []struct {
		name          string
		state, source *string
		activatedAt   any
		valid         bool
	}{
		{name: "all_unset_s3", valid: true},
		{name: "inactive_complete", state: deviceIdentityString("inactive"), source: deviceIdentityString("not_activated"), valid: true},
		{name: "active_runtime_timestamp", state: deviceIdentityString("active"), source: deviceIdentityString("runtime_confirmation"), activatedAt: time.Now().UTC(), valid: true},
		{name: "active_source_record_timestamp", state: deviceIdentityString("active"), source: deviceIdentityString("source_record"), activatedAt: time.Now().UTC(), valid: true},
		{name: "active_unknown_time", state: deviceIdentityString("active"), source: deviceIdentityString("source_unknown"), valid: true},
		{name: "missing_source", state: deviceIdentityString("active"), valid: false},
		{name: "missing_state", source: deviceIdentityString("not_activated"), valid: false},
		{name: "inactive_timestamp", state: deviceIdentityString("inactive"), source: deviceIdentityString("not_activated"), activatedAt: time.Now().UTC(), valid: false},
		{name: "active_source_without_timestamp", state: deviceIdentityString("active"), source: deviceIdentityString("source_record"), valid: false},
		{name: "unknown_source_with_timestamp", state: deviceIdentityString("active"), source: deviceIdentityString("source_unknown"), activatedAt: time.Now().UTC(), valid: false},
		{name: "unknown_enum", state: deviceIdentityString("pending"), source: deviceIdentityString("not_activated"), valid: false},
	}
	for _, tc := range activationCases {
		t.Run("activation/"+tc.name, func(t *testing.T) {
			row := validDeviceIdentitySchemaRow(tenantA)
			row.activationState, row.activationTimeSource, row.activatedAt = tc.state, tc.source, tc.activatedAt
			if tc.valid {
				if err := insertDeviceIdentitySchemaRow(ctx, tx, row); err != nil {
					t.Fatalf("valid activation tuple rejected: %v", err)
				}
				return
			}
			expectDeviceIdentitySQLState(t, ctx, tx, "activation_"+tc.name, "23514", func() error { return insertDeviceIdentitySchemaRow(ctx, tx, row) })
		})
	}

	validJSON := validDeviceIdentitySchemaRow(tenantA)
	validJSON.labels = `{"a":"","key_2":"value"}`
	validJSON.tags = `["a","b"]`
	validJSON.customFields = `{"s":"text","n":1,"b":false,"null":null}`
	validJSON.location = deviceIdentityString(`{"latitude":null,"longitude":null,"address":"Only address"}`)
	if err := insertDeviceIdentitySchemaRow(ctx, tx, validJSON); err != nil {
		t.Fatalf("valid empty-value labels/scalar custom fields/address-only location rejected: %v", err)
	}

	invalidRows := []struct {
		name   string
		mutate func(*deviceIdentitySchemaRow)
	}{
		{"labels_array", func(r *deviceIdentitySchemaRow) { r.labels = `[]` }},
		{"labels_key_pattern", func(r *deviceIdentitySchemaRow) { r.labels = `{"bad key":"x"}` }},
		{"labels_value_type", func(r *deviceIdentitySchemaRow) { r.labels = `{"key":1}` }},
		{"labels_value_length", func(r *deviceIdentitySchemaRow) { r.labels = `{"key":"` + strings.Repeat("v", 257) + `"}` }},
		{"labels_count", func(r *deviceIdentitySchemaRow) { r.labels = deviceIdentityJSONMap(33) }},
		{"tags_object", func(r *deviceIdentitySchemaRow) { r.tags = `{}` }},
		{"tags_empty_item", func(r *deviceIdentitySchemaRow) { r.tags = `[""]` }},
		{"tags_long_item", func(r *deviceIdentitySchemaRow) { r.tags = `["` + strings.Repeat("t", 129) + `"]` }},
		{"tags_duplicate", func(r *deviceIdentitySchemaRow) { r.tags = `["same","same"]` }},
		{"tags_count", func(r *deviceIdentitySchemaRow) { r.tags = deviceIdentityJSONArray(101) }},
		{"custom_fields_array", func(r *deviceIdentitySchemaRow) { r.customFields = `[]` }},
		{"custom_fields_nested_object", func(r *deviceIdentitySchemaRow) { r.customFields = `{"x":{"nested":true}}` }},
		{"custom_fields_nested_array", func(r *deviceIdentitySchemaRow) { r.customFields = `{"x":[1]}` }},
		{"custom_fields_count", func(r *deviceIdentitySchemaRow) { r.customFields = deviceIdentityJSONMap(101) }},
		{"location_missing_key", func(r *deviceIdentitySchemaRow) {
			r.location = deviceIdentityString(`{"latitude":null,"longitude":null}`)
		}},
		{"location_extra_key", func(r *deviceIdentitySchemaRow) {
			r.location = deviceIdentityString(`{"latitude":null,"longitude":null,"address":null,"altitude":2}`)
		}},
		{"location_one_coordinate", func(r *deviceIdentitySchemaRow) {
			r.location = deviceIdentityString(`{"latitude":1,"longitude":null,"address":null}`)
		}},
		{"location_latitude_range", func(r *deviceIdentitySchemaRow) {
			r.location = deviceIdentityString(`{"latitude":90.0001,"longitude":0,"address":null}`)
		}},
		{"location_longitude_range", func(r *deviceIdentitySchemaRow) {
			r.location = deviceIdentityString(`{"latitude":0,"longitude":-180.0001,"address":null}`)
		}},
		{"location_address_type", func(r *deviceIdentitySchemaRow) {
			r.location = deviceIdentityString(`{"latitude":null,"longitude":null,"address":1}`)
		}},
		{"location_address_length", func(r *deviceIdentitySchemaRow) {
			r.location = deviceIdentityString(`{"latitude":null,"longitude":null,"address":"` + strings.Repeat("a", 513) + `"}`)
		}},
	}
	for _, tc := range invalidRows {
		t.Run("json/"+tc.name, func(t *testing.T) {
			row := validDeviceIdentitySchemaRow(tenantA)
			tc.mutate(&row)
			expectDeviceIdentitySQLState(t, ctx, tx, "json_"+tc.name, "23514", func() error { return insertDeviceIdentitySchemaRow(ctx, tx, row) })
		})
	}

	invalidAdministrative := validDeviceIdentitySchemaRow(tenantA)
	invalidAdministrative.administrativeState = "paused"
	expectDeviceIdentitySQLState(t, ctx, tx, "administrative_enum", "23514", func() error { return insertDeviceIdentitySchemaRow(ctx, tx, invalidAdministrative) })
	invalidVersion := validDeviceIdentitySchemaRow(tenantA)
	invalidVersion.resourceVersion = 9007199254740992
	expectDeviceIdentitySQLState(t, ctx, tx, "resource_version_safe_range", "23514", func() error { return insertDeviceIdentitySchemaRow(ctx, tx, invalidVersion) })
	invalidActor := validDeviceIdentitySchemaRow(tenantA)
	invalidActor.createdBy = deviceIdentityString(" ")
	expectDeviceIdentitySQLState(t, ctx, tx, "created_actor_nonempty", "23514", func() error { return insertDeviceIdentitySchemaRow(ctx, tx, invalidActor) })
}

type deviceIdentitySchemaRow struct {
	id                   string
	tenantID             string
	deviceNumber         *string
	name                 *string
	deviceKind           *string
	activationState      *string
	activatedAt          any
	activationTimeSource *string
	labels               string
	tags                 string
	customFields         string
	location             *string
	batchNumber          *string
	administrativeState  string
	resourceVersion      int64
	createdBy            *string
	updatedBy            *string
	deletedBy            *string
}

func validDeviceIdentitySchemaRow(tenantID string) deviceIdentitySchemaRow {
	return deviceIdentitySchemaRow{
		id: uuid.NewString(), tenantID: tenantID,
		labels: `{}`, tags: `[]`, customFields: `{}`, administrativeState: "enabled", resourceVersion: 1,
	}
}

func insertDeviceIdentitySchemaRow(ctx context.Context, tx *sqldb.Tx, row deviceIdentitySchemaRow) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO devices (
			id, tenant_id, device_number, name, device_kind,
			activation_state, activated_at, activation_time_source,
			labels, tags, custom_fields, location, batch_number,
			administrative_state, resource_version, created_by, updated_by, deleted_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9::jsonb, $10::jsonb, $11::jsonb, $12::jsonb, $13,
			$14, $15, $16, $17, $18
		)
	`, row.id, row.tenantID, row.deviceNumber, row.name, row.deviceKind,
		row.activationState, row.activatedAt, row.activationTimeSource,
		row.labels, row.tags, row.customFields, row.location, row.batchNumber,
		row.administrativeState, row.resourceVersion, row.createdBy, row.updatedBy, row.deletedBy)
	return err
}

func expectDeviceIdentitySQLState(t *testing.T, ctx context.Context, tx *sqldb.Tx, name, wantSQLState string, operation func() error) {
	t.Helper()
	savepoint := "device_identity_" + strings.ReplaceAll(name, "/", "_")
	if _, err := tx.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("create savepoint %s: %v", savepoint, err)
	}
	err := operation()
	if err == nil {
		_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("operation unexpectedly succeeded; wanted SQLSTATE %s", wantSQLState)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != wantSQLState {
		_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("operation error=%v, want SQLSTATE %s", err, wantSQLState)
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("rollback savepoint %s: %v", savepoint, err)
	}
	if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("release savepoint %s: %v", savepoint, err)
	}
}

func deviceIdentityString(value string) *string { return &value }

func deviceIdentityJSONMap(count int) string {
	entries := make([]string, count)
	for i := range entries {
		entries[i] = fmt.Sprintf(`"key_%d":"value"`, i)
	}
	return "{" + strings.Join(entries, ",") + "}"
}

func deviceIdentityJSONArray(count int) string {
	entries := make([]string, count)
	for i := range entries {
		entries[i] = fmt.Sprintf(`"tag-%d"`, i)
	}
	return "[" + strings.Join(entries, ",") + "]"
}

func stringKeys(values map[string]struct {
	nullable     string
	defaultValue string
}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func normalizeDeviceColumnDefault(value string) string {
	return strings.NewReplacer("::character varying", "", "::jsonb", "", "(", "", ")", "").Replace(strings.ToLower(strings.TrimSpace(value)))
}
