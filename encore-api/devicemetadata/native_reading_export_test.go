package devicemetadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

type fakeHistory struct {
	rows  map[string][]exportSample
	calls int
	err   error
}

func (f *fakeHistory) Page(_ context.Context, _, _, identifier string, fromMs, toMs int64, page, pageSize int) ([]exportSample, int64, error) {
	f.calls++
	if f.err != nil {
		return nil, 0, f.err
	}
	var inRange []exportSample
	for _, row := range f.rows[identifier] {
		if ms := row.At.UnixMilli(); ms >= fromMs && ms <= toMs {
			inRange = append(inRange, row)
		}
	}
	start := (page - 1) * pageSize
	if start >= len(inRange) {
		return nil, int64(len(inRange)), nil
	}
	end := start + pageSize
	if end > len(inRange) {
		end = len(inRange)
	}
	return inRange[start:end], int64(len(inRange)), nil
}

func exportBody(identifiers string, from, to, format string) []byte {
	return []byte(`{"kind":"telemetry","identifiers":` + identifiers + `,"from":"` + from + `","to":"` + to + `","format":"` + format + `","aggregation":"raw","window":null}`)
}

func TestDecodeReadingExport(t *testing.T) {
	good := string(exportBody(`["temp"]`, "2026-10-29T00:00:00Z", "2026-10-29T06:00:00Z", "csv"))
	if plan, err := decodeReadingExport([]byte(good)); err != nil || plan.format != "csv" || len(plan.identifiers) != 1 {
		t.Fatalf("valid: %+v %v", plan, err)
	}
	if plan, err := decodeReadingExport(exportBody(`["temp"]`, "2026-10-29T00:00:00Z", "2026-10-30T00:00:01Z", "csv")); err != nil || !plan.async {
		t.Fatalf("a raw export over 24h must be accepted as a long export: %+v %v", plan, err)
	}
	if plan, err := decodeReadingExport([]byte(good)); err != nil || plan.async {
		t.Fatalf("a short export must stay synchronous: %+v %v", plan, err)
	}
	for name, body := range map[string]string{
		"attribute":             strings.Replace(good, `"telemetry"`, `"attribute"`, 1),
		"aggregated no window":  strings.Replace(good, `"aggregation":"raw"`, `"aggregation":"avg"`, 1),
		"unknown aggregation":   strings.Replace(good, `"aggregation":"raw"`, `"aggregation":"median"`, 1),
		"aggregated bad window": strings.Replace(strings.Replace(good, `"aggregation":"raw"`, `"aggregation":"avg"`, 1), `"window":null`, `"window":"45m"`, 1),
		"window set":            strings.Replace(good, `"window":null`, `"window":"1h"`, 1),
		"window missing":        strings.Replace(good, `,"window":null`, ``, 1),
		"bad format":            strings.Replace(good, `"csv"`, `"xlsx"`, 1),
		"dup identifiers":       strings.Replace(good, `["temp"]`, `["a","a"]`, 1),
		"no identifiers":        strings.Replace(good, `["temp"]`, `[]`, 1),
		"over 31 days":          string(exportBody(`["temp"]`, "2026-09-28T00:00:00Z", "2026-10-30T00:00:01Z", "csv")),
		"from after to":         string(exportBody(`["temp"]`, "2026-10-29T06:00:00Z", "2026-10-29T00:00:00Z", "csv")),
		"unknown field":         strings.Replace(good, `"kind"`, `"server":"x","kind"`, 1),
	} {
		if _, err := decodeReadingExport([]byte(body)); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestExportReadingsStoresADownloadOnlyItsOwnerCanFetch(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "exporter-"+uuid.NewString()[:8]
	deviceID := uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "EXP-"+deviceID[:8], "export device", "{}", true); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM metadata_downloads WHERE tenant_id=$1`,
			`DELETE FROM metadata_idempotency WHERE tenant_id=$1`,
			`DELETE FROM metadata_audit_events WHERE tenant_id=$1`,
			`DELETE FROM metadata_operations WHERE tenant_id=$1`,
			`DELETE FROM device_access_bindings WHERE tenant_id=$1`,
			`DELETE FROM devices WHERE tenant_id=$1`,
		} {
			if _, err := db.Exec(ctx, q, tenantID); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
	})
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	identity, err := currentNativeDeviceWriteIdentity(false)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)
	history := &fakeHistory{rows: map[string][]exportSample{
		"temp": {
			{Identifier: "temp", At: base.Add(2 * time.Minute), Value: json.RawMessage(`22.5`)},
			{Identifier: "temp", At: base.Add(1 * time.Minute), Value: json.RawMessage(`21`)},
			{Identifier: "temp", At: base.Add(-2 * time.Hour), Value: json.RawMessage(`99`)}, // outside the range
		},
		"on": {{Identifier: "on", At: base, Value: json.RawMessage(`true`)}},
	}}
	previous := activeHistorySource
	activeHistorySource = history
	defer func() { activeHistorySource = previous }()
	code := func(err error) errs.ErrCode {
		var e *errs.Error
		if errors.As(err, &e) {
			return e.Code
		}
		return errs.Unknown
	}
	key := func(l string) string { return "exp-" + l + "-" + strings.ReplaceAll(uuid.NewString(), "-", "") }
	body := exportBody(`["temp","on"]`, "2026-10-29T00:00:00Z", "2026-10-29T06:00:00Z", "csv")

	first, err := exportReadings(ctx, identity, deviceID, key("a"), body, "r1")
	if err != nil || first.Status != 202 {
		t.Fatalf("export: %+v %v", first, err)
	}
	var op struct{ Data NativeOperation }
	if err := json.Unmarshal(first.Body, &op); err != nil || op.Data.State != "succeeded" {
		t.Fatalf("operation: %s", first.Body)
	}
	var result struct{ DownloadID, Checksum string }
	var raw map[string]string
	if err := json.Unmarshal(op.Data.Result, &raw); err != nil {
		t.Fatalf("result: %s", op.Data.Result)
	}
	result.DownloadID, result.Checksum = raw["downloadId"], raw["checksum"]

	download, err := readMetadataDownload(ctx, tenantID, subjectID, true, result.DownloadID)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	sum := sha256.Sum256(download.Content)
	if hex.EncodeToString(sum[:]) != result.Checksum || download.Checksum != result.Checksum {
		t.Fatalf("checksum mismatch")
	}
	want := "identifier,observedAt,value\non,2026-10-29T01:00:00.000Z,true\ntemp,2026-10-29T01:02:00.000Z,22.5\ntemp,2026-10-29T01:01:00.000Z,21\n"
	if string(download.Content) != want {
		t.Fatalf("csv content:\n%s\nwant:\n%s", download.Content, want)
	}

	// An aggregated export carries window bounds and real sample counts, and may span more than 24 hours.
	aggBody := []byte(`{"kind":"telemetry","identifiers":["temp"],"from":"2026-10-29T00:00:00Z","to":"2026-11-05T00:00:00Z","format":"csv","aggregation":"avg","window":"1h"}`)
	agg, err := exportReadings(ctx, identity, deviceID, key("agg"), aggBody, "r1b")
	if err != nil {
		t.Fatalf("aggregated export: %v", err)
	}
	var aggOp struct{ Data NativeOperation }
	_ = json.Unmarshal(agg.Body, &aggOp)
	var aggResult map[string]string
	_ = json.Unmarshal(aggOp.Data.Result, &aggResult)
	aggFile, err := readMetadataDownload(ctx, tenantID, subjectID, true, aggResult["downloadId"])
	if err != nil {
		t.Fatal(err)
	}
	wantAgg := "identifier,windowStart,windowEnd,aggregation,value,sampleCount\ntemp,2026-10-29T01:00:00.000Z,2026-10-29T02:00:00.000Z,avg,21.75,2\n"
	if string(aggFile.Content) != wantAgg {
		t.Fatalf("aggregated csv:\n%s\nwant:\n%s", aggFile.Content, wantAgg)
	}

	// Replaying the same key returns the stored answer without asking the runtime again.
	calls := history.calls
	sameKey := key("b")
	one, err := exportReadings(ctx, identity, deviceID, sameKey, body, "r2")
	if err != nil {
		t.Fatal(err)
	}
	afterFirst := history.calls
	two, err := exportReadings(ctx, identity, deviceID, sameKey, body, "r3")
	if err != nil || string(two.Body) != string(one.Body) || history.calls != afterFirst || afterFirst == calls {
		t.Fatalf("replay re-fetched or changed: err=%v calls=%d/%d", err, history.calls, afterFirst)
	}

	// Only the creating subject, inside the tenant, with the permission, may download.
	if _, err := readMetadataDownload(ctx, tenantID, "someone-else", true, result.DownloadID); code(err) != errs.NotFound {
		t.Fatalf("other subject: %v", err)
	}
	if _, err := readMetadataDownload(ctx, uuid.NewString(), subjectID, true, result.DownloadID); code(err) != errs.NotFound {
		t.Fatalf("other tenant: %v", err)
	}
	if _, err := readMetadataDownload(ctx, tenantID, subjectID, false, result.DownloadID); code(err) != errs.NotFound {
		t.Fatalf("without permission: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE metadata_downloads SET created_at=NOW()-INTERVAL '2 days',expires_at=NOW()-INTERVAL '1 day' WHERE tenant_id=$1 AND id=$2`, tenantID, result.DownloadID); err != nil {
		t.Fatal(err)
	}
	if _, err := readMetadataDownload(ctx, tenantID, subjectID, true, result.DownloadID); !errors.Is(err, errDownloadExpired) {
		t.Fatalf("expired: %v", err)
	}

	// Limits: too many rows, runtime down, and an unprivileged role.
	big := make([]exportSample, exportMaxRows+1)
	for i := range big {
		big[i] = exportSample{Identifier: "big", At: base.Add(time.Duration(i) * time.Millisecond), Value: json.RawMessage(`1`)}
	}
	history.rows["big"] = big
	if _, err := exportReadings(ctx, identity, deviceID, key("c"), exportBody(`["big"]`, "2026-10-29T00:00:00Z", "2026-10-29T06:00:00Z", "jsonl"), "r4"); code(err) != errs.InvalidArgument {
		t.Fatalf("row cap: %v", err)
	}
	history.err = errors.New("down")
	if _, err := exportReadings(ctx, identity, deviceID, key("d"), body, "r5"); code(err) != errs.Unavailable {
		t.Fatalf("runtime down: %v", err)
	}
	history.err = nil
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"tenant_viewer"}})
	if _, err := exportReadings(ctx, identity, deviceID, key("e"), body, "r6"); code(err) != errs.PermissionDenied {
		t.Fatalf("viewer: %v", err)
	}
}

func TestAuthorizeReadingExportOperation(t *testing.T) {
	op := &NativeOperation{Kind: "device_export", CreatedBy: "u1"}
	if err := authorizeReadingExportOperation(op, "u1", true); err != nil {
		t.Fatalf("owner with permission rejected: %v", err)
	}
	for name, c := range map[string]struct {
		op      *NativeOperation
		subject string
		can     bool
	}{
		"no permission": {op, "u1", false},
		"other subject": {op, "u2", true},
		"other kind":    {&NativeOperation{Kind: "device_delete", CreatedBy: "u1"}, "u1", true},
		"nil":           {nil, "u1", true},
	} {
		var e *errs.Error
		if err := authorizeReadingExportOperation(c.op, c.subject, c.can); !errors.As(err, &e) || e.Code != errs.NotFound {
			t.Errorf("%s: expected not found, got %v", name, err)
		}
	}
}

func TestPurgeExpiredDownloadsKeepsLiveArtifacts(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM metadata_downloads WHERE tenant_id=$1`, tenantID)
		_, _ = db.Exec(ctx, `DELETE FROM metadata_operations WHERE tenant_id=$1`, tenantID)
	})
	insert := func(expiresIn string) string {
		opID, dlID := uuid.NewString(), uuid.NewString()
		if _, err := db.Exec(ctx, `INSERT INTO metadata_operations(id,tenant_id,created_by,updated_by,kind,state,originating_operation_name,originating_subject_id,
				resource_kind,resource_id,authorized_selection,request_digest,total_items,completed_items,failed_items,checkpoint,result,finished_at,expires_at)
			VALUES($1,$2,'u','u','device_export','succeeded','exportDeviceReadings','u','device',$3,'{}'::jsonb,$4,1,1,0,'{"awaiting":"none"}'::jsonb,'{}'::jsonb,NOW(),NOW()+INTERVAL '7 days')`,
			opID, tenantID, uuid.NewString(), strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO metadata_downloads(id,tenant_id,operation_id,created_by,file_name,content_type,content,checksum,size_bytes,row_count,created_at,expires_at)
			VALUES($1,$2,$3,'u','a.csv','text/csv','x',$4,1,1,NOW()-INTERVAL '2 hours',NOW()+$5::interval)`,
			dlID, tenantID, opID, strings.Repeat("b", 64), expiresIn); err != nil {
			t.Fatal(err)
		}
		return dlID
	}
	expired, live := insert("-1 hour"), insert("1 hour")
	if _, err := purgeExpiredDownloads(ctx); err != nil {
		t.Fatal(err)
	}
	exists := func(id string) bool {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_downloads WHERE id=$1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	if exists(expired) || !exists(live) {
		t.Fatalf("expired kept=%v live kept=%v", exists(expired), exists(live))
	}
}
