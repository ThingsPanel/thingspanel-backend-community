package devicemetadata

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/et"
	"github.com/google/uuid"
)

// flakyHistory fails exactly one call, to prove a restart resumes from the checkpoint.
type flakyHistory struct {
	*fakeHistory
	failOn int
}

func (f *flakyHistory) Page(ctx context.Context, tenantID, deviceID, identifier string, fromMs, toMs int64, page, pageSize int) ([]exportSample, int64, error) {
	if f.calls+1 == f.failOn {
		f.calls++
		return nil, 0, errors.New("runtime hiccup")
	}
	return f.fakeHistory.Page(ctx, tenantID, deviceID, identifier, fromMs, toMs, page, pageSize)
}

func halfHourSamples(from time.Time, count int) []exportSample {
	out := make([]exportSample, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, exportSample{Identifier: "temp", At: from.Add(time.Duration(i) * 30 * time.Minute), Value: json.RawMessage("1")})
	}
	return out
}

func TestLongExportRunsInSlicesResumesAndAssemblesOneFile(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "long-export-"+uuid.NewString()[:8]
	deviceID := uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "LNG-"+deviceID[:8], "long export device", "{}", true); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM metadata_export_chunks WHERE tenant_id=$1`,
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

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(36 * time.Hour)
	body := exportBody(`["temp"]`, from.Format(time.RFC3339), to.Format(time.RFC3339), "csv")
	key := "long-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	accepted, err := exportReadings(ctx, identity, deviceID, key, body, "req-long")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != 202 {
		t.Fatalf("status=%d, want 202", accepted.Status)
	}
	var operationID, state string
	var total int
	if err := db.QueryRow(ctx, `SELECT id::text,state,total_items FROM metadata_operations WHERE tenant_id=$1 AND kind='device_export'`, tenantID).
		Scan(&operationID, &state, &total); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || total != 36 {
		t.Fatalf("operation state=%s total=%d, want pending/36", state, total)
	}

	// First pass fails at the 5th source call: the operation backs off and the checkpoint keeps what was saved.
	src := &flakyHistory{fakeHistory: &fakeHistory{rows: map[string][]exportSample{"temp": halfHourSamples(from, 72)}}, failOn: 5}
	if _, err := processAsyncExports(ctx, src, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	var chunks int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM metadata_export_chunks WHERE operation_id=$1`, operationID).Scan(&chunks)
	if chunks != 4 {
		t.Fatalf("saved %d slices before the failure, want 4", chunks)
	}
	// Second pass resumes and finishes once the backoff has elapsed.
	if _, err := db.Exec(ctx, `UPDATE metadata_operations SET lease_until=NOW()-INTERVAL '1 second' WHERE id=$1`, operationID); err != nil {
		t.Fatal(err)
	}
	if _, err := processAsyncExports(ctx, src, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	var finalState string
	var completed int
	var result []byte
	if err := db.QueryRow(ctx, `SELECT state,completed_items,result FROM metadata_operations WHERE id=$1`, operationID).Scan(&finalState, &completed, &result); err != nil {
		t.Fatal(err)
	}
	if finalState != "succeeded" || completed != 36 {
		t.Fatalf("final state=%s completed=%d", finalState, completed)
	}
	var doc struct{ DownloadID string }
	if err := json.Unmarshal(result, &doc); err != nil || doc.DownloadID == "" {
		t.Fatalf("result=%s err=%v", result, err)
	}
	d, err := readMetadataDownload(ctx, tenantID, subjectID, true, doc.DownloadID)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(d.Content)), "\n")
	if len(lines) != 73 || lines[0] != "identifier,observedAt,value" {
		t.Fatalf("got %d lines, header %q; want 73 (header + 72 rows)", len(lines), lines[0])
	}
	if strings.Count(string(d.Content), "identifier,observedAt,value") != 1 {
		t.Fatal("header repeated across chunks")
	}
	// Newest first, no duplicate or missing sample across slice boundaries and across the resume.
	seen := map[string]bool{}
	for i, line := range lines[1:] {
		at := strings.Split(line, ",")[1]
		if seen[at] {
			t.Fatalf("duplicate row %s", at)
		}
		seen[at] = true
		want := to.Add(-30 * time.Minute * time.Duration(i+1)).Format(aggregateTimeFmt)
		if at != want {
			t.Fatalf("row %d is %s, want %s", i, at, want)
		}
	}
	_ = db.QueryRow(ctx, `SELECT count(*) FROM metadata_export_chunks WHERE operation_id=$1`, operationID).Scan(&chunks)
	if chunks != 0 {
		t.Fatalf("%d chunks left behind", chunks)
	}
}

func TestLongExportFailsCleanlyWhenTooLarge(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "long-big-"+uuid.NewString()[:8]
	deviceID := uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "BIG-"+deviceID[:8], "big export device", "{}", true); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM metadata_export_chunks WHERE tenant_id=$1`,
			`DELETE FROM metadata_idempotency WHERE tenant_id=$1`,
			`DELETE FROM metadata_audit_events WHERE tenant_id=$1`,
			`DELETE FROM metadata_operations WHERE tenant_id=$1`,
			`DELETE FROM device_access_bindings WHERE tenant_id=$1`,
			`DELETE FROM devices WHERE tenant_id=$1`,
		} {
			_, _ = db.Exec(ctx, q, tenantID)
		}
	})
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	identity, err := currentNativeDeviceWriteIdentity(false)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	body := exportBody(`["temp"]`, from.Format(time.RFC3339), from.Add(48*time.Hour).Format(time.RFC3339), "jsonl")
	if _, err := exportReadings(ctx, identity, deviceID, "big-"+strings.ReplaceAll(uuid.NewString(), "-", ""), body, "req-big"); err != nil {
		t.Fatal(err)
	}
	huge := &hugeHistory{}
	if _, err := processAsyncExports(ctx, huge, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	var state string
	var failure []byte
	if err := db.QueryRow(ctx, `SELECT state,failure FROM metadata_operations WHERE tenant_id=$1 AND kind='device_export'`, tenantID).Scan(&state, &failure); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || !strings.Contains(string(failure), "EXPORT_TOO_LARGE") {
		t.Fatalf("state=%s failure=%s", state, failure)
	}
}

// hugeHistory claims an enormous total without returning rows.
type hugeHistory struct{}

func (hugeHistory) Page(context.Context, string, string, string, int64, int64, int, int) ([]exportSample, int64, error) {
	return nil, asyncExportMaxRows + 1, nil
}
