package devicemetadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/cron"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// Long raw exports (DM-RT-018, second slice). A raw export longer than 24 hours
// is accepted as a pending operation; a cron worker walks the range backwards
// in one-hour slices (newest first, matching the synchronous export), appends
// each slice to metadata_export_chunks and records its position in the
// operation checkpoint. A crashed or restarted worker resumes from the
// checkpoint after its lease expires. When the last slice is done the chunks
// are assembled into one metadata_downloads artifact.

const (
	asyncExportMaxRange = 31 * 24 * time.Hour
	asyncExportMaxRows  = 1_000_000
	asyncExportMaxBytes = 60 << 20 // below the 64 MiB column limit
	asyncExportSlice    = time.Hour
	asyncExportLease    = 2 * time.Minute
	asyncExportBudget   = 40 * time.Second
	asyncExportAttempts = 10
	asyncExportWorker   = "export-worker"
)

type asyncExportSelection struct {
	Identifiers []string `json:"identifiers"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	Format      string   `json:"format"`
}

type asyncExportCheckpoint struct {
	Identifier int   `json:"identifier"` // index into the identifiers
	CursorMs   int64 `json:"cursorMs"`   // exclusive end of the next slice to read
	Rows       int64 `json:"rows"`
	Seq        int   `json:"seq"`
	Bytes      int64 `json:"bytes"`
}

func asyncExportSliceCount(from, to time.Time) int {
	return int((to.Sub(from) + asyncExportSlice - 1) / asyncExportSlice)
}

func exportReadingsAsync(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key string, plan readingExportPlan,
	canonical []byte, path, requestID string) (*metadataWriteResponse, error) {
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "exportDeviceReadings",
		Method: http.MethodPost, Path: path, Key: key, RequestID: requestID, CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		var locked string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
			identity.tenantID, deviceID).Scan(&locked); err != nil {
			if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "device not found"}
			}
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		selection, _ := json.Marshal(asyncExportSelection{
			Identifiers: plan.identifiers, From: plan.from.Format(time.RFC3339Nano), To: plan.to.Format(time.RFC3339Nano), Format: plan.format})
		checkpoint, _ := json.Marshal(asyncExportCheckpoint{CursorMs: plan.to.UnixMilli()})
		operationID := uuid.NewString()
		digest := operationRequestDigest(http.MethodPost, path, "", canonical)
		total := len(plan.identifiers) * asyncExportSliceCount(plan.from, plan.to)
		if _, err := tx.Exec(ctx, `INSERT INTO metadata_operations(
				id,tenant_id,created_by,updated_by,kind,state,originating_operation_name,originating_subject_id,
				resource_kind,resource_id,authorized_selection,request_digest,total_items,completed_items,failed_items,
				checkpoint,expires_at)
			VALUES($1,$2,$3,$3,'device_export','pending','exportDeviceReadings',$3,'device',$4,$5::jsonb,$6,$7,0,0,$8::jsonb,NOW()+INTERVAL '7 days')`,
			operationID, identity.tenantID, identity.subjectID, deviceID, selection, digest, total, checkpoint); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		operation, err := loadNativeOperationEncoreTx(ctx, tx, identity.tenantID, operationID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := operationAcceptedResponse(operation, operationID, requestID, "")
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{"format": plan.format, "identifiers": len(plan.identifiers), "aggregation": "raw", "async": true})
		return response, metadataWriteAudit{ResourceKind: "device_export", ResourceID: operationID, RedactedChanges: changes}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

type claimedExport struct {
	operationID, tenantID, subjectID, deviceID string
	selection                                  asyncExportSelection
	checkpoint                                 asyncExportCheckpoint
	attempts                                   int
}

// claimAsyncExport leases the oldest runnable long export, or returns nil.
func claimAsyncExport(ctx context.Context) (*claimedExport, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, wrapDBErr(err, "claim export failed")
	}
	defer tx.Rollback()
	var c claimedExport
	var selection, checkpoint []byte
	err = tx.QueryRow(ctx, `SELECT id::text,tenant_id::text,originating_subject_id,resource_id::text,authorized_selection,checkpoint,attempts
		FROM metadata_operations
		WHERE kind='device_export' AND state IN ('pending','running') AND (lease_until IS NULL OR lease_until < NOW())
		ORDER BY created_at, id
		LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&c.operationID, &c.tenantID, &c.subjectID, &c.deviceID, &selection, &checkpoint, &c.attempts)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapDBErr(err, "claim export failed")
	}
	if err := json.Unmarshal(selection, &c.selection); err != nil || json.Unmarshal(checkpoint, &c.checkpoint) != nil {
		return nil, wrapDBErr(errors.New("unreadable export state"), "claim export failed")
	}
	if _, err := tx.Exec(ctx, `UPDATE metadata_operations
		SET state='running',lease_owner=$3,lease_until=NOW()+$4::interval,attempts=attempts+1,started_at=COALESCE(started_at,NOW()),updated_by=$3
		WHERE tenant_id=$1 AND id=$2`, c.tenantID, c.operationID, asyncExportWorker, fmt.Sprintf("%d seconds", int(asyncExportLease.Seconds()))); err != nil {
		return nil, wrapDBErr(err, "claim export failed")
	}
	c.attempts++
	return &c, tx.Commit()
}

func asyncExportProblem(operationID, code, detail string) []byte {
	problem, _ := json.Marshal(map[string]any{
		"type": "about:blank", "title": "The export could not be completed", "status": 422, "detail": detail,
		"instance": "/api/v2/reading-exports/" + operationID, "code": code, "requestId": "export-worker",
	})
	return problem
}

func failAsyncExport(ctx context.Context, c *claimedExport, code, detail string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return wrapDBErr(err, "fail export failed")
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `DELETE FROM metadata_export_chunks WHERE tenant_id=$1 AND operation_id=$2`, c.tenantID, c.operationID); err != nil {
		return wrapDBErr(err, "fail export failed")
	}
	if _, err := tx.Exec(ctx, `UPDATE metadata_operations
		SET state='failed',failed_items=total_items,completed_items=total_items,finished_at=NOW(),lease_owner=NULL,lease_until=NULL,
			failure=$3::jsonb,updated_by=$4
		WHERE tenant_id=$1 AND id=$2`, c.tenantID, c.operationID, asyncExportProblem(c.operationID, code, detail), asyncExportWorker); err != nil {
		return wrapDBErr(err, "fail export failed")
	}
	return tx.Commit()
}

// backoffAsyncExport keeps the lease for a while after a runtime error so the
// next attempt happens on a later tick instead of burning every retry at once.
func backoffAsyncExport(ctx context.Context, c *claimedExport) {
	_, _ = db.Exec(ctx, `UPDATE metadata_operations SET lease_until=NOW()+INTERVAL '60 seconds' WHERE tenant_id=$1 AND id=$2 AND state='running'`,
		c.tenantID, c.operationID)
}

func releaseAsyncExport(ctx context.Context, c *claimedExport) {
	_, _ = db.Exec(ctx, `UPDATE metadata_operations SET lease_owner=NULL,lease_until=NULL WHERE tenant_id=$1 AND id=$2 AND state='running'`,
		c.tenantID, c.operationID)
}

// runAsyncExport advances one leased export until it finishes or the budget ends.
func runAsyncExport(ctx context.Context, src readingHistorySource, c *claimedExport, deadline time.Time) error {
	fromMs := mustParseMs(c.selection.From)
	for time.Now().Before(deadline) {
		cp := c.checkpoint
		if cp.Identifier >= len(c.selection.Identifiers) {
			return finishAsyncExport(ctx, c)
		}
		sliceEnd := cp.CursorMs
		sliceStart := sliceEnd - asyncExportSlice.Milliseconds()
		if sliceStart < fromMs {
			sliceStart = fromMs
		}
		identifier := c.selection.Identifiers[cp.Identifier]
		var samples []exportSample
		for page := 1; ; page++ {
			rows, total, err := src.Page(ctx, c.tenantID, c.deviceID, identifier, sliceStart, sliceEnd-1, page, exportPageSize)
			if err != nil {
				if c.attempts >= asyncExportAttempts {
					return failAsyncExport(ctx, c, "RUNTIME_UNAVAILABLE", "The device runtime stayed unavailable while building the export.")
				}
				backoffAsyncExport(ctx, c)
				return nil
			}
			if cp.Rows+total > asyncExportMaxRows {
				return failAsyncExport(ctx, c, "EXPORT_TOO_LARGE", fmt.Sprintf("The export would exceed %d rows; narrow the time range or the identifiers.", asyncExportMaxRows))
			}
			samples = append(samples, rows...)
			if len(rows) == 0 || int64(page)*exportPageSize >= total {
				break
			}
		}
		sort.SliceStable(samples, func(i, j int) bool { return samples[i].At.After(samples[j].At) })
		rows := make([]exportRow, 0, len(samples))
		for _, s := range samples {
			rows = append(rows, exportRow{Identifier: s.Identifier, At: s.At, Aggregation: "raw", Value: s.Value})
		}
		content, _, err := renderExport(c.selection.Format, false, rows)
		if err != nil {
			return failAsyncExport(ctx, c, "RENDER_FAILED", "The export could not be rendered.")
		}
		if cp.Seq > 0 && c.selection.Format == "csv" {
			// Only the first chunk carries the header line.
			if i := bytes.IndexByte(content, '\n'); i >= 0 {
				content = content[i+1:]
			}
		}
		if cp.Bytes+int64(len(content)) > asyncExportMaxBytes {
			return failAsyncExport(ctx, c, "EXPORT_TOO_LARGE", "The export file would be too large; narrow the time range or the identifiers.")
		}
		next := asyncExportCheckpoint{Identifier: cp.Identifier, CursorMs: sliceStart, Rows: cp.Rows + int64(len(rows)), Seq: cp.Seq + 1, Bytes: cp.Bytes + int64(len(content))}
		if sliceStart <= fromMs {
			next.Identifier, next.CursorMs = cp.Identifier+1, mustParseMs(c.selection.To)
		}
		if err := saveAsyncExportSlice(ctx, c, cp.Seq, content, len(rows), next); err != nil {
			return err
		}
		c.checkpoint = next
	}
	releaseAsyncExport(ctx, c)
	return nil
}

func mustParseMs(value string) int64 {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func saveAsyncExportSlice(ctx context.Context, c *claimedExport, seq int, content []byte, rowCount int, next asyncExportCheckpoint) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return wrapDBErr(err, "save export slice failed")
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_export_chunks(tenant_id,operation_id,seq,content,row_count) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT (tenant_id,operation_id,seq) DO UPDATE SET content=EXCLUDED.content,row_count=EXCLUDED.row_count`,
		c.tenantID, c.operationID, seq, content, rowCount); err != nil {
		return wrapDBErr(err, "save export slice failed")
	}
	checkpoint, _ := json.Marshal(next)
	if _, err := tx.Exec(ctx, `UPDATE metadata_operations
		SET checkpoint=$3::jsonb,completed_items=LEAST(total_items,completed_items+1),lease_until=NOW()+$4::interval,updated_by=$5
		WHERE tenant_id=$1 AND id=$2`, c.tenantID, c.operationID, checkpoint, fmt.Sprintf("%d seconds", int(asyncExportLease.Seconds())), asyncExportWorker); err != nil {
		return wrapDBErr(err, "save export slice failed")
	}
	return tx.Commit()
}

func finishAsyncExport(ctx context.Context, c *claimedExport) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return wrapDBErr(err, "finish export failed")
	}
	defer tx.Rollback()
	rows, err := tx.Query(ctx, `SELECT content FROM metadata_export_chunks WHERE tenant_id=$1 AND operation_id=$2 ORDER BY seq`, c.tenantID, c.operationID)
	if err != nil {
		return wrapDBErr(err, "finish export failed")
	}
	var file bytes.Buffer
	for rows.Next() {
		var part []byte
		if err := rows.Scan(&part); err != nil {
			rows.Close()
			return wrapDBErr(err, "finish export failed")
		}
		file.Write(part)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return wrapDBErr(err, "finish export failed")
	}
	contentType := "text/csv; charset=utf-8"
	ext := "csv"
	if c.selection.Format == "jsonl" {
		contentType, ext = "application/x-ndjson", "jsonl"
	}
	sum := sha256.Sum256(file.Bytes())
	checksum := hex.EncodeToString(sum[:])
	downloadID := uuid.NewString()
	from, _ := time.Parse(time.RFC3339Nano, c.selection.From)
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_downloads(id,tenant_id,operation_id,created_by,file_name,content_type,content,checksum,size_bytes,row_count,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, downloadID, c.tenantID, c.operationID, c.subjectID,
		fmt.Sprintf("readings-%s.%s", from.UTC().Format("20060102T150405Z"), ext), contentType, file.Bytes(), checksum, file.Len(),
		c.checkpoint.Rows, time.Now().UTC().Add(exportTTL)); err != nil {
		return wrapDBErr(err, "finish export failed")
	}
	result, _ := json.Marshal(map[string]any{"downloadId": downloadID, "checksum": checksum})
	if _, err := tx.Exec(ctx, `UPDATE metadata_operations
		SET state='succeeded',completed_items=total_items,failed_items=0,finished_at=NOW(),lease_owner=NULL,lease_until=NULL,result=$3::jsonb,updated_by=$4
		WHERE tenant_id=$1 AND id=$2`, c.tenantID, c.operationID, result, asyncExportWorker); err != nil {
		return wrapDBErr(err, "finish export failed")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM metadata_export_chunks WHERE tenant_id=$1 AND operation_id=$2`, c.tenantID, c.operationID); err != nil {
		return wrapDBErr(err, "finish export failed")
	}
	return tx.Commit()
}

// processAsyncExports works leased exports one after another until the budget
// is spent. It returns how many it touched.
func processAsyncExports(ctx context.Context, src readingHistorySource, budget time.Duration) (int, error) {
	deadline := time.Now().Add(budget)
	handled := 0
	for time.Now().Before(deadline) {
		claimed, err := claimAsyncExport(ctx)
		if err != nil {
			return handled, err
		}
		if claimed == nil {
			return handled, nil
		}
		handled++
		if err := runAsyncExport(ctx, src, claimed, deadline); err != nil {
			rlog.Error("long export step failed", "operation", claimed.operationID, "err", err)
			releaseAsyncExport(ctx, claimed)
			return handled, err
		}
	}
	return handled, nil
}

var _ = cron.NewJob("process-long-reading-exports", cron.JobConfig{
	Title:    "Build long reading exports slice by slice",
	Every:    1 * cron.Minute,
	Endpoint: ProcessLongReadingExports,
})

// ProcessLongReadingExports is private operational work driven by cron.
//
//encore:api private
func ProcessLongReadingExports(ctx context.Context) error {
	_, err := processAsyncExports(ctx, activeHistorySource, asyncExportBudget)
	return err
}
