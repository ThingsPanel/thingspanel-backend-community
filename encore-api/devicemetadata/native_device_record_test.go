package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

type nativeDeviceRecordTestRow struct {
	values []any
	err    error
}

func (r nativeDeviceRecordTestRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("unexpected native device scan arity")
	}
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(r.values[i]))
	}
	return nil
}

func completeNativeDeviceRecordTestRow() nativeDeviceRecordTestRow {
	now := time.Date(2026, time.October, 26, 8, 0, 0, 0, time.UTC)
	return nativeDeviceRecordTestRow{values: []any{
		"55555555-5555-4555-8555-555555555555",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		sql.NullTime{Time: now, Valid: true}, sql.NullTime{Time: now, Valid: true},
		sql.NullString{String: "actor-created", Valid: true},
		sql.NullString{String: "actor-updated", Valid: true},
		int64(7),
		sql.NullString{String: "Pump", Valid: true},
		sql.NullString{String: "pump-001", Valid: true},
		sql.NullString{String: "standalone", Valid: true},
		sql.NullString{}, sql.NullString{},
		[]byte(`{"zone":"north"}`),
		[]byte(nil),
		sql.NullString{String: "55555555-5555-4555-8555-555555555555", Valid: true},
		sql.NullString{}, sql.NullString{},
		sql.NullString{}, sql.NullString{}, sql.NullInt64{}, sql.NullString{}, sql.NullString{},
		sql.NullString{String: "enabled", Valid: true},
		sql.NullString{String: "inactive", Valid: true},
		sql.NullTime{}, sql.NullString{},
		sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
		[]byte(`[]`), []byte(`{"calibration":null}`), sql.NullString{},
	}}
}

func TestScanNativeDeviceRowEmitsCompleteOASDeviceShape(t *testing.T) {
	device, err := scanNativeDeviceRow(completeNativeDeviceRecordTestRow())
	if err != nil {
		t.Fatalf("scanNativeDeviceRow() error = %v", err)
	}
	body, err := json.Marshal(device)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	want := []string{
		"id", "tenantId", "createdAt", "updatedAt", "createdBy", "updatedBy", "resourceVersion",
		"name", "deviceNumber", "deviceKind", "productId", "description", "labels", "location",
		"templateId", "modelBinding", "administrativeState", "activationState", "activatedAt",
		"gatewayId", "runtimeRef", "tags", "customFields", "batchNumber",
	}
	if len(fields) != len(want) {
		t.Fatalf("response field count = %d, want %d (%s)", len(fields), len(want), body)
	}
	for _, key := range want {
		if _, ok := fields[key]; !ok {
			t.Errorf("required OAS field %q omitted from %s", key, body)
		}
	}
	for _, forbidden := range []string{"online", "isOnline", "voucher", "sn", "parentDeviceId"} {
		if _, ok := fields[forbidden]; ok {
			t.Errorf("legacy/runtime field %q leaked into response", forbidden)
		}
	}
	for _, key := range []string{"productId", "description", "location", "templateId", "modelBinding", "activatedAt", "gatewayId", "runtimeRef", "batchNumber"} {
		if string(fields[key]) != "null" {
			t.Errorf("nullable field %q = %s, want explicit null", key, fields[key])
		}
	}
	if string(fields["labels"]) != `{"zone":"north"}` || string(fields["tags"]) != `[]` ||
		string(fields["customFields"]) != `{"calibration":null}` || string(fields["location"]) != "null" {
		t.Fatalf("raw JSON fields were not preserved: %s", body)
	}
}

func TestScanNativeDeviceRowFailsClosedForMissingBinding(t *testing.T) {
	row := completeNativeDeviceRecordTestRow()
	row.values[14] = sql.NullString{}
	_, err := scanNativeDeviceRow(row)
	var apiErr *errs.Error
	if !errors.As(err, &apiErr) || apiErr.Code != errs.Unavailable {
		t.Fatalf("missing access binding error = %v, want Unavailable", err)
	}
}

func TestScanNativeDeviceRowNormalizesAuditAndActivationTimesToUTC(t *testing.T) {
	row := completeNativeDeviceRecordTestRow()
	localTime := time.Date(2026, time.October, 26, 16, 0, 0, 0, time.FixedZone("fixture", 8*60*60))
	row.values[2] = sql.NullTime{Time: localTime, Valid: true}
	row.values[3] = sql.NullTime{Time: localTime, Valid: true}
	row.values[23] = sql.NullString{String: "active", Valid: true}
	row.values[24] = sql.NullTime{Time: localTime, Valid: true}
	device, err := scanNativeDeviceRow(row)
	if err != nil {
		t.Fatalf("scanNativeDeviceRow() error = %v", err)
	}
	if device.CreatedAt.Location() != time.UTC || device.UpdatedAt.Location() != time.UTC ||
		device.ActivatedAt == nil || device.ActivatedAt.Location() != time.UTC {
		t.Fatalf("snapshot timestamps were not normalized to UTC: created=%s updated=%s activated=%v", device.CreatedAt, device.UpdatedAt, device.ActivatedAt)
	}
}

func TestScanNativeDeviceRowProjectsTemplateCurrentModelSelection(t *testing.T) {
	row := completeNativeDeviceRecordTestRow()
	row.values[15] = sql.NullString{String: "66666666-6666-4666-8666-666666666666", Valid: true}
	row.values[16] = sql.NullString{String: "66666666-6666-4666-8666-666666666666", Valid: true}
	row.values[17] = sql.NullString{String: "77777777-7777-4777-8777-777777777777", Valid: true}
	row.values[18] = sql.NullString{String: "88888888-8888-4888-8888-888888888888", Valid: true}
	row.values[19] = sql.NullInt64{Int64: 3, Valid: true}
	row.values[20] = sql.NullString{String: strings.Repeat("a", 64), Valid: true}
	row.values[21] = sql.NullString{String: strings.Repeat("b", 64), Valid: true}
	row.values[25] = sql.NullString{String: "99999999-9999-4999-8999-999999999999", Valid: true}
	row.values[26] = sql.NullString{String: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Valid: true}
	row.values[27] = sql.NullString{String: "plant/line-1", Valid: true}
	row.values[28] = sql.NullString{String: "external-1", Valid: true}
	row.values[29] = sql.NullString{String: "instance-1", Valid: true}
	device, err := scanNativeDeviceRow(row)
	if err != nil {
		t.Fatalf("scanNativeDeviceRow() error = %v", err)
	}
	if device.TemplateID == nil || *device.TemplateID != "66666666-6666-4666-8666-666666666666" {
		t.Fatalf("templateId = %v", device.TemplateID)
	}
	if device.ModelBinding == nil || device.ModelBinding.ModelID != "77777777-7777-4777-8777-777777777777" ||
		device.ModelBinding.SnapshotID != "88888888-8888-4888-8888-888888888888" || device.ModelBinding.PublishedRevision != 3 {
		t.Fatalf("modelBinding was not projected from the template current ThingModel selection: %#v", device.ModelBinding)
	}
	if device.RuntimeRef == nil || device.RuntimeRef.ProviderID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" ||
		device.RuntimeRef.Namespace != "plant/line-1" || device.RuntimeRef.ExternalID != "external-1" ||
		device.RuntimeRef.ServiceInstanceID == nil || *device.RuntimeRef.ServiceInstanceID != "instance-1" {
		t.Fatalf("runtimeRef was not projected from binding columns: %#v", device.RuntimeRef)
	}
	if device.GatewayID == nil || *device.GatewayID != "99999999-9999-4999-8999-999999999999" {
		t.Fatalf("gatewayId was not projected from gateway membership: %v", device.GatewayID)
	}
}

func TestNativeDeviceProjectionUsesOnlyNativeOwners(t *testing.T) {
	for _, legacy := range []string{"d.sn", "d.parent_device_id", "d.voucher", "d.is_online", "latest", "ORDER BY revision", "device_access_template_revisions"} {
		if strings.Contains(nativeDeviceSelectFields+nativeDeviceFromSQL, legacy) {
			t.Errorf("native projection contains forbidden legacy/latest reference %q", legacy)
		}
	}
	for _, required := range []string{
		"LEFT JOIN device_access_bindings b", "b.tenant_id = d.tenant_id AND b.device_id = d.id",
		"t.thing_model_id", "t.thing_model_snapshot_id", "s.version", "s.canonical_checksum", "s.resources_checksum",
		"b.runtime_provider_id", "b.runtime_namespace", "b.runtime_external_id", "b.service_instance_id",
		"gm.gateway_device_id",
	} {
		if !strings.Contains(nativeDeviceSelectFields+nativeDeviceFromSQL, required) {
			t.Errorf("native projection missing required owner field %q", required)
		}
	}
}

func TestGetNativeDeviceRecordTxReadsOnlyItsTransactionAndTenant(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, otherTenantID, deviceID := testTenantID(), testTenantID(), testTenantID()
	tx, err := db.Stdlib().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO devices(
			id,tenant_id,name,device_number,device_kind,administrative_state,activation_state,
			activation_time_source,created_by,updated_by,labels,tags,custom_fields
		) VALUES($1,$2,$3,$4,'standalone','enabled','inactive','not_activated',$5,$5,'{"zone":"tx"}','[]','{"calibration":9007199254740993}')
	`, deviceID, tenantID, "tx snapshot device", "tx-"+uuid.NewString(), "projection-test"); err != nil {
		t.Fatalf("insert uncommitted device fixture: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO device_access_bindings(tenant_id,device_id,updated_by)
		VALUES($1,$2,$3)
	`, tenantID, deviceID, "projection-test"); err != nil {
		t.Fatalf("insert uncommitted binding fixture: %v", err)
	}
	device, err := getNativeDeviceRecordTx(ctx, tx, tenantID, deviceID)
	if err != nil || device.ID != deviceID {
		t.Fatalf("same-transaction device read = %#v, err=%v", device, err)
	}
	var labels map[string]string
	if err := json.Unmarshal(device.Labels, &labels); err != nil || labels["zone"] != "tx" {
		t.Fatalf("same-transaction labels = %v, err=%v", labels, err)
	}
	var customFields map[string]json.RawMessage
	if err := json.Unmarshal(device.CustomFields, &customFields); err != nil || strings.TrimSpace(string(customFields["calibration"])) != "9007199254740993" {
		t.Fatalf("same-transaction custom field lost exact numeric value: %s err=%v", device.CustomFields, err)
	}
	if _, err := getNativeDeviceRecordTx(ctx, tx, otherTenantID, deviceID); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant device read must be not found, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.Stdlib().QueryRowContext(ctx, `SELECT count(*) FROM devices WHERE tenant_id=$1 AND id=$2`, tenantID, deviceID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("transaction fixture remained after rollback: count=%d err=%v", remaining, err)
	}
}
