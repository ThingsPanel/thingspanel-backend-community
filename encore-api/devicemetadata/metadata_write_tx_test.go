package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

func metadataWriteTestScope(t *testing.T) (context.Context, metadataWriteCommand) {
	t.Helper()
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	command := metadataWriteCommand{
		TenantID: testTenantID(), SubjectID: "issuer:operator/test@example.test",
		Operation: "createDeviceAccessTemplate", Path: "/api/v2/device-access-templates",
		Key: "write-test-" + testTenantID(), Method: "POST", RequestID: testTenantID(),
		CanonicalBody: []byte(`{"name":"receipt-test"}`),
	}
	t.Cleanup(func() {
		for _, table := range []string{"metadata_audit_events", "metadata_idempotency", "device_access_templates"} {
			if _, err := db.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", command.TenantID); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	})
	return ctx, command
}

func metadataWriteTestApply(ctx context.Context, command metadataWriteCommand, calls *atomic.Int32, reject bool) func(*sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
	return func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		calls.Add(1)
		row, err := insertAccessTemplateConfigurationTx(ctx, tx, command.TenantID, command.SubjectID, &accessTemplateConfigurationRecord{
			Name: "receipt-" + testTenantID(), Tags: []string{}, AdministrativeState: "enabled", Configuration: json.RawMessage(accessTemplateStoreFixtureConfiguration),
		})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if reject {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.InvalidArgument, Message: "rejected after mutation"}
		}
		return metadataWriteResponse{
			Status: 201, Location: command.Path + "/" + row.ID, ETag: `"test-etag"`,
			Body: json.RawMessage(fmt.Sprintf(`{"data":{"id":%q,"exponent":1e3,"precise":1.234567890123456789},"meta":{"requestId":%q}}`, row.ID, command.RequestID)),
		}, metadataWriteAudit{ResourceKind: "device_access_template", ResourceID: row.ID, AfterVersion: &row.ResourceVersion, RedactedChanges: json.RawMessage(`{"created":true}`)}, nil
	}
}

func TestMetadataWriteTxReplayAndRequestPreconditions(t *testing.T) {
	ctx, command := metadataWriteTestScope(t)
	var calls atomic.Int32
	first, err := runMetadataWriteTx(ctx, command, metadataWriteTestApply(ctx, command, &calls, false))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := runMetadataWriteTx(ctx, command, metadataWriteTestApply(ctx, command, &calls, false))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || !replayed.Replayed || first.Replayed || first.Status != 201 || first.Location != replayed.Location || first.ETag != replayed.ETag || string(first.Body) != string(replayed.Body) {
		t.Fatalf("receipt failed to reproduce first committed response: calls=%d first=%+v replay=%+v", calls.Load(), first, replayed)
	}
	if !strings.Contains(string(first.Body), "1.234567890123456789") {
		t.Fatal("receipt lost numeric precision")
	}
	changed := command
	changed.CanonicalBody = []byte(`{"name":"different"}`)
	if _, err := runMetadataWriteTx(ctx, changed, metadataWriteTestApply(ctx, changed, &calls, false)); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("same key different body: %v", err)
	}
	changed = command
	changed.IfMatch = `"another-version"`
	if _, err := runMetadataWriteTx(ctx, changed, metadataWriteTestApply(ctx, changed, &calls, false)); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("same key changed precondition: %v", err)
	}
	var templates, audits, receipts int
	for _, check := range []struct {
		table string
		count *int
	}{{"device_access_templates", &templates}, {"metadata_audit_events", &audits}, {"metadata_idempotency", &receipts}} {
		if err := db.QueryRow(ctx, "SELECT count(*) FROM "+check.table+" WHERE tenant_id=$1", command.TenantID).Scan(check.count); err != nil {
			t.Fatal(err)
		}
	}
	if templates != 1 || audits != 1 || receipts != 1 {
		t.Fatalf("write/audit/receipt must each occur once: %d/%d/%d", templates, audits, receipts)
	}
}

func TestMetadataWriteTxMutationFailureRollsBackEverything(t *testing.T) {
	ctx, command := metadataWriteTestScope(t)
	var calls atomic.Int32
	if _, err := runMetadataWriteTx(ctx, command, metadataWriteTestApply(ctx, command, &calls, true)); errs.Code(err) != errs.InvalidArgument {
		t.Fatalf("expected rejected mutation: %v", err)
	}
	for _, table := range []string{"device_access_templates", "metadata_audit_events", "metadata_idempotency"} {
		var count int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", command.TenantID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("failed mutation left %d rows in %s", count, table)
		}
	}
	if _, err := runMetadataWriteTx(ctx, command, metadataWriteTestApply(ctx, command, &calls, false)); err != nil {
		t.Fatalf("rolled back key could not be safely retried: %v", err)
	}
}

func TestMetadataWriteTxConcurrentReplayExecutesOnce(t *testing.T) {
	ctx, command := metadataWriteTestScope(t)
	var calls atomic.Int32
	type outcome struct {
		result *metadataWriteResponse
		err    error
	}
	results := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r, err := runMetadataWriteTx(ctx, command, metadataWriteTestApply(ctx, command, &calls, false))
			results <- outcome{r, err}
		}()
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil {
		t.Fatalf("concurrent replay: %v / %v", a.err, b.err)
	}
	if calls.Load() != 1 || string(a.result.Body) != string(b.result.Body) || a.result.Replayed == b.result.Replayed {
		t.Fatalf("concurrent mutation was not deduplicated: calls=%d", calls.Load())
	}
}

func TestMetadataWriteTxExpiredReceiptBecomesTombstone(t *testing.T) {
	ctx, command := metadataWriteTestScope(t)
	var calls atomic.Int32
	if _, err := runMetadataWriteTx(ctx, command, metadataWriteTestApply(ctx, command, &calls, false)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE metadata_idempotency SET replay_until=NOW()-INTERVAL '1 second' WHERE tenant_id=$1`, command.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := runMetadataWriteTx(ctx, command, metadataWriteTestApply(ctx, command, &calls, false)); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("expired key: %v", err)
	}
	var state string
	var noBody bool
	if err := db.QueryRow(ctx, `SELECT state,response IS NULL FROM metadata_idempotency WHERE tenant_id=$1`, command.TenantID).Scan(&state, &noBody); err != nil {
		t.Fatal(err)
	}
	if state != "expired" || !noBody || calls.Load() != 1 {
		t.Fatalf("expired receipt was reused: %q noBody=%v calls=%d", state, noBody, calls.Load())
	}
}
