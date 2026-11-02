package devicemetadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"encore.app/internal/authz"
	"encore.app/internal/communityclient"
	"encore.app/internal/nativehttp"
	"encore.app/l1-access/authn"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/cron"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// Reading export (DM-RT-018), first slice. The job runs inside the request and
// answers 202 with an operation that is already terminal, so clients poll the
// same way they would for a queued job. Limits are stricter than the contract:
// telemetry only, at most 24 hours for raw samples (366 days when aggregated),
// 10 identifiers and exportMaxRows raw samples read. Anything above is refused; no partial file is produced.

const (
	exportMaxRows        = 50000
	exportMaxIdentifiers = 10
	exportPageSize       = 1000
	exportTTL            = 24 * time.Hour
)

type readingExportInput struct {
	Kind        string          `json:"kind"`
	Identifiers []string        `json:"identifiers"`
	From        string          `json:"from"`
	To          string          `json:"to"`
	Format      string          `json:"format"`
	Aggregation string          `json:"aggregation"`
	Window      json.RawMessage `json:"window"`
}

type readingExportPlan struct {
	identifiers []string
	from, to    time.Time
	format      string
	aggregation string // "raw" or an aggregation name
	window      string // empty for raw
	// async is a raw export longer than 24 hours; a worker builds it slice by slice.
	async bool
}

func decodeReadingExport(body []byte) (readingExportPlan, error) {
	var in readingExportInput
	var plan readingExportPlan
	if err := decodeStrict(body, &in); err != nil {
		return plan, err
	}
	refuse := func(message string) error { return &errs.Error{Code: errs.InvalidArgument, Message: message} }
	switch {
	case in.Kind != "telemetry":
		return plan, refuse("only telemetry can be exported by the Community runtime adapter")
	case !validAggregation(in.Aggregation):
		return plan, invalidStructure("aggregation must be raw, min, max, avg, sum, diff or last")
	case len(in.Window) == 0:
		return plan, invalidStructure("window is required (null for raw exports)")
	case in.Format != "csv" && in.Format != "jsonl":
		return plan, invalidStructure("format must be csv or jsonl")
	case len(in.Identifiers) < 1 || len(in.Identifiers) > exportMaxIdentifiers:
		return plan, invalidStructure("identifiers must contain 1 to " + strconv.Itoa(exportMaxIdentifiers) + " entries")
	}
	seen := map[string]bool{}
	for _, id := range in.Identifiers {
		if id == "" || utf8.RuneCountInString(id) > 255 || seen[id] {
			return plan, invalidStructure("identifiers must be unique, non-empty and at most 255 characters")
		}
		seen[id] = true
	}
	window := ""
	if in.Aggregation == "raw" {
		if string(in.Window) != "null" {
			return plan, invalidStructure("window must be null for raw exports")
		}
	} else {
		if err := json.Unmarshal(in.Window, &window); err != nil {
			return plan, invalidStructure("window must be one of the supported windows for aggregated exports")
		}
		if _, ok := readingWindows[window]; !ok {
			return plan, invalidStructure("window must be one of the supported windows for aggregated exports")
		}
	}
	from, err := time.Parse(time.RFC3339Nano, in.From)
	if err != nil {
		return plan, invalidStructure("from must be an RFC 3339 timestamp")
	}
	to, err := time.Parse(time.RFC3339Nano, in.To)
	if err != nil {
		return plan, invalidStructure("to must be an RFC 3339 timestamp")
	}
	from, to = from.UTC().Truncate(time.Millisecond), to.UTC().Truncate(time.Millisecond)
	maxRange := asyncExportMaxRange
	if in.Aggregation != "raw" {
		maxRange = aggregateMaxRange
	}
	if !from.Before(to) || to.Sub(from) > maxRange {
		return plan, invalidStructure("from must be before to, within 31 days for raw exports and 366 days for aggregated ones")
	}
	async := in.Aggregation == "raw" && to.Sub(from) > 24*time.Hour
	return readingExportPlan{identifiers: in.Identifiers, from: from, to: to, format: in.Format, aggregation: in.Aggregation, window: window, async: async}, nil
}

type exportSample struct {
	Identifier string
	At         time.Time
	Value      json.RawMessage
}

// readingHistorySource pages stored samples for one identifier over the
// inclusive millisecond range [fromMs, toMs].
type readingHistorySource interface {
	Page(ctx context.Context, tenantID, deviceID, identifier string, fromMs, toMs int64, page, pageSize int) ([]exportSample, int64, error)
}

type communityHistorySource struct{}

func (communityHistorySource) Page(ctx context.Context, tenantID, deviceID, identifier string, fromMs, toMs int64, page, pageSize int) ([]exportSample, int64, error) {
	legacyID, err := lookupRuntimeDeviceID(ctx, tenantID, deviceID)
	if err != nil {
		return nil, 0, err
	}
	rows, total, err := nativeRuntimeClient().ListTelemetryHistory(ctx, legacyID, identifier, fromMs, toMs, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]exportSample, 0, len(rows))
	for _, row := range rows {
		out = append(out, exportSample{Identifier: row.Key, At: row.Ts, Value: row.Value})
	}
	return out, total, nil
}

var activeHistorySource readingHistorySource = communityHistorySource{}

func collectExportSamples(ctx context.Context, src readingHistorySource, tenantID, deviceID string, plan readingExportPlan) ([]exportSample, error) {
	var all []exportSample
	fromMs, toMs := plan.from.UnixMilli(), plan.to.UnixMilli()-1 // [from, to)
	for _, identifier := range plan.identifiers {
		for page := 1; ; page++ {
			rows, total, err := src.Page(ctx, tenantID, deviceID, identifier, fromMs, toMs, page, exportPageSize)
			if err != nil {
				return nil, &errs.Error{Code: errs.Unavailable, Message: "device runtime backend is unavailable or returned an unrecognized response"}
			}
			if total > exportMaxRows-int64(len(all)) {
				return nil, &errs.Error{Code: errs.InvalidArgument, Message: "the export would exceed " + strconv.Itoa(exportMaxRows) + " rows; narrow the time range or the identifiers"}
			}
			all = append(all, rows...)
			if len(rows) == 0 || int64(page)*exportPageSize >= total {
				break
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Identifier != all[j].Identifier {
			return all[i].Identifier < all[j].Identifier
		}
		return all[i].At.After(all[j].At)
	})
	return all, nil
}

// exportRow is one line of an export: a raw sample, or an aggregated bucket.
type exportRow struct {
	Identifier  string
	At          time.Time
	WindowEnd   *time.Time
	Aggregation string
	Value       json.RawMessage
	SampleCount *int64
}

func rowsForExport(deviceID string, plan readingExportPlan, samples []exportSample) ([]exportRow, error) {
	if plan.aggregation == "raw" {
		rows := make([]exportRow, 0, len(samples))
		for _, s := range samples {
			rows = append(rows, exportRow{Identifier: s.Identifier, At: s.At, Aggregation: "raw", Value: s.Value})
		}
		return rows, nil
	}
	byIdentifier := map[string][]communityclient.HistoryReading{}
	for _, s := range samples {
		byIdentifier[s.Identifier] = append(byIdentifier[s.Identifier], communityclient.HistoryReading{Key: s.Identifier, Ts: s.At, Value: s.Value})
	}
	var rows []exportRow
	for _, identifier := range plan.identifiers {
		buckets, err := aggregateHistory(deviceID, identifier, plan.aggregation, plan.window, byIdentifier[identifier])
		if err != nil {
			return nil, err
		}
		for _, b := range buckets {
			start, _ := time.Parse(aggregateTimeFmt, *b.WindowStart)
			end, _ := time.Parse(aggregateTimeFmt, *b.WindowEnd)
			rows = append(rows, exportRow{Identifier: identifier, At: start, WindowEnd: &end, Aggregation: plan.aggregation, Value: b.Value, SampleCount: b.SampleCount})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Identifier != rows[j].Identifier {
			return rows[i].Identifier < rows[j].Identifier
		}
		return rows[i].At.After(rows[j].At)
	})
	return rows, nil
}

func renderExport(format string, aggregated bool, rows []exportRow) ([]byte, string, error) {
	var buf bytes.Buffer
	stamp := func(t time.Time) string { return t.UTC().Format(aggregateTimeFmt) }
	if format == "csv" {
		w := csv.NewWriter(&buf)
		if aggregated {
			_ = w.Write([]string{"identifier", "windowStart", "windowEnd", "aggregation", "value", "sampleCount"})
			for _, r := range rows {
				_ = w.Write([]string{r.Identifier, stamp(r.At), stamp(*r.WindowEnd), r.Aggregation, string(r.Value), strconv.FormatInt(*r.SampleCount, 10)})
			}
		} else {
			_ = w.Write([]string{"identifier", "observedAt", "value"})
			for _, r := range rows {
				_ = w.Write([]string{r.Identifier, stamp(r.At), string(r.Value)})
			}
		}
		w.Flush()
		return buf.Bytes(), "text/csv; charset=utf-8", w.Error()
	}
	for _, r := range rows {
		doc := map[string]any{"identifier": r.Identifier, "value": r.Value}
		if aggregated {
			doc["windowStart"], doc["windowEnd"], doc["aggregation"], doc["sampleCount"] = stamp(r.At), stamp(*r.WindowEnd), r.Aggregation, *r.SampleCount
		} else {
			doc["observedAt"] = stamp(r.At)
		}
		line, err := json.Marshal(doc)
		if err != nil {
			return nil, "", err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), "application/x-ndjson", nil
}

func exportReadings(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key string, body []byte, requestID string) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	if !authz.Can(authn.CurrentData(), "device", "data:export") {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to export device data"}
	}
	plan, err := decodeReadingExport(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	path := "/api/v2/devices/" + deviceID + "/reading-exports"
	if plan.async {
		return exportReadingsAsync(ctx, identity, deviceID, key, plan, canonical, path, requestID)
	}
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
		samples, err := collectExportSamples(ctx, activeHistorySource, identity.tenantID, deviceID, plan)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		rows, err := rowsForExport(deviceID, plan, samples)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		content, contentType, err := renderExport(plan.format, plan.aggregation != "raw", rows)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Internal, Message: "failed to render the export"}
		}
		sum := sha256.Sum256(content)
		checksum := hex.EncodeToString(sum[:])
		operationID, downloadID := uuid.NewString(), uuid.NewString()
		resultDoc, _ := json.Marshal(map[string]any{"downloadId": downloadID, "checksum": checksum})
		digest := operationRequestDigest(http.MethodPost, path, "", canonical)
		if _, err := tx.Exec(ctx, `INSERT INTO metadata_operations(
				id,tenant_id,created_by,updated_by,kind,state,originating_operation_name,originating_subject_id,
				resource_kind,resource_id,authorized_selection,request_digest,total_items,completed_items,failed_items,
				checkpoint,result,finished_at,expires_at)
			VALUES($1,$2,$3,$3,'device_export','succeeded','exportDeviceReadings',$3,'device',$4,'{}'::jsonb,$5,1,1,0,
				'{"awaiting":"none"}'::jsonb,$6::jsonb,NOW(),NOW()+INTERVAL '7 days')`,
			operationID, identity.tenantID, identity.subjectID, deviceID, digest, resultDoc); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		fileName := fmt.Sprintf("readings-%s.%s", plan.from.Format("20060102T150405Z"), map[string]string{"csv": "csv", "jsonl": "jsonl"}[plan.format])
		if _, err := tx.Exec(ctx, `INSERT INTO metadata_downloads(id,tenant_id,operation_id,created_by,file_name,content_type,content,checksum,size_bytes,row_count,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, downloadID, identity.tenantID, operationID, identity.subjectID, fileName,
			contentType, content, checksum, len(content), len(rows), time.Now().UTC().Add(exportTTL)); err != nil {
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
		changes, _ := json.Marshal(map[string]any{"format": plan.format, "identifiers": len(plan.identifiers), "aggregation": plan.aggregation, "rows": len(rows)})
		return response, metadataWriteAudit{ResourceKind: "device_export", ResourceID: operationID, RedactedChanges: changes}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

// ---- download ----

// errDownloadExpired is answered as 410 Gone.
var errDownloadExpired = &errs.Error{Code: errs.NotFound, Message: "download has expired"}

type metadataDownload struct {
	FileName    string
	ContentType string
	Content     []byte
	Checksum    string
}

// readMetadataDownload returns the artifact only to the subject that created it,
// inside its tenant, while that subject still holds the originating permission.
// Anything else looks like "not found"; an expired artifact is gone (410).
func readMetadataDownload(ctx context.Context, tenantID, subjectID string, canExport bool, downloadID string) (*metadataDownload, error) {
	notFound := &errs.Error{Code: errs.NotFound, Message: "download not found"}
	if !canExport {
		return nil, notFound
	}
	var d metadataDownload
	var createdBy string
	var expires time.Time
	err := db.QueryRow(ctx, `SELECT file_name,content_type,content,checksum,created_by,expires_at FROM metadata_downloads
		WHERE tenant_id=$1 AND id=$2`, tenantID, downloadID).Scan(&d.FileName, &d.ContentType, &d.Content, &d.Checksum, &createdBy, &expires)
	if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return nil, notFound
	}
	if err != nil {
		return nil, wrapDBErr(err, "read download failed")
	}
	if createdBy != subjectID {
		return nil, notFound
	}
	if !expires.After(time.Now()) {
		return nil, errDownloadExpired
	}
	return &d, nil
}

//encore:api auth raw method=POST path=/api/v2/devices/:deviceID/reading-exports
func (s *Service) ExportDeviceReadings(w http.ResponseWriter, r *http.Request) {
	serveSimulatorWrite(w, r, false, func(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key, _ string, body []byte, requestID string) (*metadataWriteResponse, error) {
		return exportReadings(ctx, identity, deviceID, key, body, requestID)
	})
}

var _ = cron.NewJob("purge-expired-metadata-downloads", cron.JobConfig{
	Title:    "Delete expired export artifacts",
	Every:    1 * cron.Hour,
	Endpoint: PurgeExpiredMetadataDownloads,
})

// purgeExpiredDownloads removes artifacts past their expiry. Reads already
// treat them as gone; this only reclaims the storage. The operation row stays
// as the audit trail of the export.
func purgeExpiredDownloads(ctx context.Context) (int64, error) {
	tag, err := db.Exec(ctx, `DELETE FROM metadata_downloads WHERE expires_at <= NOW()`)
	if err != nil {
		return 0, wrapDBErr(err, "purge expired downloads failed")
	}
	return tag.RowsAffected(), nil
}

// PurgeExpiredMetadataDownloads is private operational work driven by cron.
//
//encore:api private
func PurgeExpiredMetadataDownloads(ctx context.Context) error {
	_, err := purgeExpiredDownloads(ctx)
	return err
}

// authorizeReadingExportOperation limits the export-task view to the subject
// that started the export while it still holds the export permission. Any
// other operation or subject looks like "not found".
func authorizeReadingExportOperation(operation *NativeOperation, subjectID string, canExport bool) error {
	if !canExport || operation == nil || operation.Kind != "device_export" || operation.CreatedBy != subjectID {
		return &errs.Error{Code: errs.NotFound, Message: "export not found"}
	}
	return nil
}

//encore:api auth raw method=GET path=/api/v2/reading-exports/:operationID
func (s *Service) GetReadingExport(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	operationID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("operationID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	data := authn.CurrentData()
	subject := ""
	if data != nil {
		subject = data.UserID
	}
	tx, err := beginTenantGroupTx(r.Context(), identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, wrapDBErr(err, "read export failed"), 0, false)
		return
	}
	defer tx.Rollback()
	operation, err := loadNativeOperationTx(r.Context(), tx, identity.tenantID, operationID)
	if err == nil {
		err = authorizeReadingExportOperation(operation, subject, authz.Can(data, "device", "data:export"))
	}
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if err := tx.Commit(); err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, wrapDBErr(err, "read export failed"), 0, false)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", NativeOperationResponse{
		Data: *operation, Meta: RequestMeta{RequestID: requestID},
	})
}

//encore:api auth raw method=GET path=/api/v2/metadata-downloads/:downloadID
func (s *Service) DownloadMetadataArtifact(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	downloadID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("downloadID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	data := authn.CurrentData()
	subject := ""
	if data != nil {
		subject = data.UserID
	}
	d, err := readMetadataDownload(r.Context(), identity.tenantID, subject, authz.Can(data, "device", "data:export"), downloadID)
	if err != nil {
		status := 0
		if errors.Is(err, errDownloadExpired) {
			status = http.StatusGone
		}
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, status, false)
		return
	}
	w.Header().Set("Content-Type", d.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+d.FileName+`"`)
	w.Header().Set("ETag", `"sha256-`+d.Checksum+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-Id", requestID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(d.Content)
}
