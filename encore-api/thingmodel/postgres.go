package thingmodel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const canonicalSchemaVersion = 1

// postgresRepository persists the v2 ThingModel aggregate in the existing
// device_metadata database. Legacy rows have canonical_model=NULL and are
// deliberately invisible to this repository until an explicit conversion.
type postgresRepository struct{}

func newPostgresRepository() app.Repository { return postgresRepository{} }

type postgresTransaction struct {
	tx *sqldb.Tx
}

type canonicalDocument struct {
	SchemaVersion int        `json:"schema_version"`
	Model         core.Model `json:"model"`
}

func (postgresRepository) Execute(ctx context.Context, scope app.ReceiptScope, fn func(app.Transaction) (app.Result, error)) (app.Result, error) {
	if fn == nil || !validReceiptScope(scope) {
		return app.Result{}, app.ErrInvalid
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return app.Result{}, persistenceError(err)
	}
	defer tx.Rollback()

	lockKey, _ := json.Marshal(struct {
		Tenant    core.TenantID `json:"tenant"`
		Subject   string        `json:"subject"`
		Operation string        `json:"operation"`
		Key       string        `json:"key"`
	}{scope.TenantID, scope.SubjectID, scope.Operation, scope.Key})
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(lockKey)); err != nil {
		return app.Result{}, persistenceError(err)
	}

	var storedHash string
	var resultJSON []byte
	var expiresAt sql.NullTime
	err = tx.QueryRow(ctx, `
		SELECT payload_hash, result, expires_at
		FROM thing_model_command_receipts
		WHERE tenant_id=$1 AND subject_id=$2 AND operation=$3 AND idempotency_key=$4
		FOR UPDATE`, string(scope.TenantID), scope.SubjectID, scope.Operation, scope.Key,
	).Scan(&storedHash, &resultJSON, &expiresAt)
	switch {
	case err == nil:
		if !expiresAt.Valid || !expiresAt.Time.After(time.Now().UTC()) || resultJSON == nil {
			// Keep the key as a tombstone after the 30-day replay window. This
			// prevents a late retry from accidentally creating a second command.
			if _, err := tx.Exec(ctx, `UPDATE thing_model_command_receipts SET result=NULL WHERE tenant_id=$1 AND subject_id=$2 AND operation=$3 AND idempotency_key=$4`, string(scope.TenantID), scope.SubjectID, scope.Operation, scope.Key); err != nil {
				return app.Result{}, persistenceError(err)
			}
			if err := tx.Commit(); err != nil {
				return app.Result{}, persistenceError(err)
			}
			return app.Result{}, app.ErrConflict
		}
		if storedHash != scope.PayloadHash {
			return app.Result{}, app.ErrConflict
		}
		var replay app.Result
		if err := json.Unmarshal(resultJSON, &replay); err != nil {
			return app.Result{}, persistenceError(err)
		}
		replay.Replayed = true
		if err := tx.Commit(); err != nil {
			return app.Result{}, persistenceError(err)
		}
		return replay, nil
	case !errors.Is(err, sqldb.ErrNoRows):
		return app.Result{}, persistenceError(err)
	}

	result, err := fn(&postgresTransaction{tx: tx})
	if err != nil {
		return app.Result{}, err
	}
	result.Replayed = false
	serialized, err := json.Marshal(result)
	if err != nil {
		return app.Result{}, app.ErrInvalid
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO thing_model_command_receipts
			(tenant_id, subject_id, operation, idempotency_key, payload_hash, result, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,NOW(),NOW() + INTERVAL '30 days')`,
		string(scope.TenantID), scope.SubjectID, scope.Operation, scope.Key, scope.PayloadHash, string(serialized))
	if err != nil {
		return app.Result{}, persistenceError(err)
	}
	if err := tx.Commit(); err != nil {
		return app.Result{}, persistenceError(err)
	}
	return result, nil
}

func (postgresRepository) Get(ctx context.Context, tenantID core.TenantID, modelID core.ModelID) (app.Record, error) {
	if !validUUID(string(tenantID)) || !validUUID(string(modelID)) {
		return app.Record{}, app.ErrInvalid
	}
	var raw []byte
	var lifecycle string
	var updatedAt time.Time
	var draftRevision, publishedRevision int64
	err := db.QueryRow(ctx, `
		SELECT canonical_model, status, draft_revision, published_revision, updated_at
		FROM thing_models
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND canonical_model IS NOT NULL`,
		string(tenantID), string(modelID),
	).Scan(&raw, &lifecycle, &draftRevision, &publishedRevision, &updatedAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return app.Record{}, app.ErrNotFound
	}
	if err != nil {
		return app.Record{}, persistenceError(err)
	}
	record, err := decodeRecord(raw, lifecycle, draftRevision, publishedRevision, updatedAt, tenantID, modelID)
	if err != nil {
		return app.Record{}, err
	}
	return record, nil
}

func (postgresRepository) List(ctx context.Context, tenantID core.TenantID, limit, offset int, keyword string) ([]app.Record, int, error) {
	if !validUUID(string(tenantID)) || limit < 1 || limit > 200 || offset < 0 {
		return nil, 0, app.ErrInvalid
	}
	pattern := "%" + strings.TrimSpace(keyword) + "%"
	var total int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM thing_models
		WHERE tenant_id=$1 AND deleted_at IS NULL AND canonical_model IS NOT NULL
		  AND ($2='' OR canonical_model->'model'->>'name' ILIKE $3)`,
		string(tenantID), strings.TrimSpace(keyword), pattern,
	).Scan(&total); err != nil {
		return nil, 0, persistenceError(err)
	}
	rows, err := db.Query(ctx, `
		SELECT canonical_model, status, draft_revision, published_revision, updated_at, id
		FROM thing_models
		WHERE tenant_id=$1 AND deleted_at IS NULL AND canonical_model IS NOT NULL
		  AND ($2='' OR canonical_model->'model'->>'name' ILIKE $3)
		ORDER BY created_at DESC, id ASC LIMIT $4 OFFSET $5`,
		string(tenantID), strings.TrimSpace(keyword), pattern, limit, offset,
	)
	if err != nil {
		return nil, 0, persistenceError(err)
	}
	defer rows.Close()

	records := make([]app.Record, 0)
	for rows.Next() {
		var raw []byte
		var status, id string
		var draftRevision, publishedRevision int64
		var updatedAt time.Time
		if err := rows.Scan(&raw, &status, &draftRevision, &publishedRevision, &updatedAt, &id); err != nil {
			return nil, 0, persistenceError(err)
		}
		record, err := decodeRecord(raw, status, draftRevision, publishedRevision, updatedAt, tenantID, core.ModelID(id))
		if err != nil {
			return nil, 0, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, persistenceError(err)
	}
	return records, total, nil
}

func (postgresRepository) Versions(ctx context.Context, tenantID core.TenantID, modelID core.ModelID) ([]app.Publication, error) {
	if !validUUID(string(tenantID)) || !validUUID(string(modelID)) {
		return nil, app.ErrInvalid
	}
	if _, err := (postgresRepository{}).Get(ctx, tenantID, modelID); err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `
		SELECT id, canonical_content, canonical_checksum, changelog, published_by, published_at, resources_content, resources_revision, resources_checksum
		FROM thing_model_snapshots
		WHERE tenant_id=$1 AND thing_model_id=$2 AND canonical_snapshot=TRUE
		ORDER BY version DESC`, string(tenantID), string(modelID))
	if err != nil {
		return nil, persistenceError(err)
	}
	defer rows.Close()
	versions := make([]app.Publication, 0)
	for rows.Next() {
		publication, err := scanPublication(rows, tenantID, modelID)
		if err != nil {
			return nil, err
		}
		versions = append(versions, publication)
	}
	if err := rows.Err(); err != nil {
		return nil, persistenceError(err)
	}
	return versions, nil
}

func (postgresRepository) Version(ctx context.Context, tenantID core.TenantID, modelID core.ModelID, revision uint64) (app.Publication, error) {
	if !validUUID(string(tenantID)) || !validUUID(string(modelID)) {
		return app.Publication{}, app.ErrInvalid
	}
	if _, err := (postgresRepository{}).Get(ctx, tenantID, modelID); err != nil {
		return app.Publication{}, err
	}
	if revision == 0 || revision > math.MaxInt32 {
		return app.Publication{}, app.ErrNotFound
	}
	var id, checksum, changelog, publishedBy string
	var content, resourcesContent []byte
	var resourcesRevision sql.NullInt64
	var resourcesChecksum sql.NullString
	var publishedAt time.Time
	err := db.QueryRow(ctx, `
		SELECT id, canonical_content, canonical_checksum, COALESCE(changelog,''), published_by, published_at, resources_content, resources_revision, resources_checksum
		FROM thing_model_snapshots
		WHERE tenant_id=$1 AND thing_model_id=$2 AND version=$3 AND canonical_snapshot=TRUE`,
		string(tenantID), string(modelID), int64(revision),
	).Scan(&id, &content, &checksum, &changelog, &publishedBy, &publishedAt, &resourcesContent, &resourcesRevision, &resourcesChecksum)
	if errors.Is(err, sqldb.ErrNoRows) {
		return app.Publication{}, app.ErrNotFound
	}
	if err != nil {
		return app.Publication{}, persistenceError(err)
	}
	return decodePublicationWithResources(id, content, checksum, changelog, publishedBy, publishedAt, tenantID, modelID, resourcesContent, resourcesRevision, resourcesChecksum)
}

func (postgresRepository) Resources(ctx context.Context, tenantID core.TenantID, modelID core.ModelID) (app.ResourcesRecord, error) {
	if !validUUID(string(tenantID)) || !validUUID(string(modelID)) {
		return app.ResourcesRecord{}, app.ErrInvalid
	}
	return readResources(ctx, db, tenantID, modelID, false)
}

func (t *postgresTransaction) Get(ctx context.Context, tenantID core.TenantID, modelID core.ModelID) (app.Record, error) {
	if !validUUID(string(tenantID)) || !validUUID(string(modelID)) {
		return app.Record{}, app.ErrInvalid
	}
	var raw []byte
	var status string
	var draftRevision, publishedRevision int64
	var updatedAt time.Time
	err := t.tx.QueryRow(ctx, `
		SELECT canonical_model, status, draft_revision, published_revision, updated_at
		FROM thing_models
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND canonical_model IS NOT NULL
		FOR UPDATE`, string(tenantID), string(modelID),
	).Scan(&raw, &status, &draftRevision, &publishedRevision, &updatedAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return app.Record{}, app.ErrNotFound
	}
	if err != nil {
		return app.Record{}, persistenceError(err)
	}
	return decodeRecord(raw, status, draftRevision, publishedRevision, updatedAt, tenantID, modelID)
}

type resourcesQuerier interface {
	QueryRow(context.Context, string, ...any) *sqldb.Row
}

func readResources(ctx context.Context, q resourcesQuerier, tenantID core.TenantID, modelID core.ModelID, lock bool) (app.ResourcesRecord, error) {
	query := `SELECT revision, profile, web_chart_config, app_chart_config FROM thing_model_resources WHERE tenant_id=$1 AND thing_model_id=$2`
	if lock {
		query += ` FOR UPDATE`
	}
	var revision int64
	var profileRaw, web, appConfig []byte
	err := q.QueryRow(ctx, query, string(tenantID), string(modelID)).Scan(&revision, &profileRaw, &web, &appConfig)
	if errors.Is(err, sqldb.ErrNoRows) {
		return app.ResourcesRecord{Document: app.ResourcesDocument{Profile: app.ResourceProfile{Tags: []string{}}}}, nil
	}
	if err != nil {
		return app.ResourcesRecord{}, persistenceError(err)
	}
	if revision < 0 {
		return app.ResourcesRecord{}, app.ErrInvalid
	}
	var doc app.ResourcesDocument
	if json.Unmarshal(profileRaw, &doc.Profile) != nil {
		return app.ResourcesRecord{}, app.ErrInvalid
	}
	doc.WebChartConfig = nullRaw(web)
	doc.AppChartConfig = nullRaw(appConfig)
	return app.ResourcesRecord{Document: doc, Revision: uint64(revision)}, nil
}

func nullRaw(raw []byte) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func (t *postgresTransaction) GetResources(ctx context.Context, tenantID core.TenantID, modelID core.ModelID, lock bool) (app.ResourcesRecord, error) {
	return readResources(ctx, t.tx, tenantID, modelID, lock)
}

func (t *postgresTransaction) SaveResources(ctx context.Context, tenantID core.TenantID, modelID core.ModelID, expected uint64, replacement app.ResourcesDocument) (app.ResourcesRecord, error) {
	if expected >= math.MaxInt64 {
		return app.ResourcesRecord{}, app.ErrInvalid
	}
	profile, err := json.Marshal(replacement.Profile)
	if err != nil {
		return app.ResourcesRecord{}, app.ErrInvalid
	}
	web, appConfig := nullableJSON(replacement.WebChartConfig), nullableJSON(replacement.AppChartConfig)
	// Backfill the revision-zero default lazily for native models created before
	// this resource table was introduced. The caller already holds the model row.
	_, err = t.tx.Exec(ctx, `INSERT INTO thing_model_resources (tenant_id, thing_model_id)
		SELECT tenant_id,id FROM thing_models WHERE tenant_id=$1 AND id=$2 AND canonical_model IS NOT NULL AND deleted_at IS NULL
		ON CONFLICT (tenant_id,thing_model_id) DO NOTHING`, string(tenantID), string(modelID))
	if err != nil {
		return app.ResourcesRecord{}, persistenceError(err)
	}
	result, err := t.tx.Exec(ctx, `UPDATE thing_model_resources SET revision=revision+1, profile=$1::jsonb, web_chart_config=$2::jsonb, app_chart_config=$3::jsonb, updated_by=NULL, updated_at=NOW() WHERE tenant_id=$4 AND thing_model_id=$5 AND revision=$6`, string(profile), web, appConfig, string(tenantID), string(modelID), int64(expected))
	if err != nil {
		return app.ResourcesRecord{}, persistenceError(err)
	}
	if result.RowsAffected() != 1 {
		return app.ResourcesRecord{}, app.ErrConflict
	}
	return app.ResourcesRecord{Document: replacement, Revision: expected + 1}, nil
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return string(raw)
}

func (t *postgresTransaction) Insert(ctx context.Context, record app.Record) error {
	if err := validateRecord(record); err != nil {
		return err
	}
	_, raw, err := encodeModel(record.Model)
	if err != nil {
		return app.ErrInvalid
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO thing_models
			(id, tenant_id, name, status, canonical_model, draft_revision, published_revision, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,NOW(),NOW())`,
		string(record.Model.ID), string(record.Model.TenantID), record.Model.Name,
		statusFor(record), string(raw), int64(record.Model.DraftRevision), int64(record.Model.PublishedRevision))
	if err != nil {
		return persistenceError(err)
	}
	_, err = t.tx.Exec(ctx, `INSERT INTO thing_model_resources (tenant_id, thing_model_id) VALUES ($1,$2)`, string(record.Model.TenantID), string(record.Model.ID))
	if err != nil {
		return persistenceError(err)
	}
	return nil
}

func (t *postgresTransaction) Update(ctx context.Context, record app.Record) error {
	if err := validateRecord(record); err != nil {
		return err
	}
	_, raw, err := encodeModel(record.Model)
	if err != nil {
		return app.ErrInvalid
	}
	result, err := t.tx.Exec(ctx, `
		UPDATE thing_models
		SET name=$1, status=$2, canonical_model=$3::jsonb, draft_revision=$4,
		    published_revision=$5, updated_at=NOW()
		WHERE tenant_id=$6 AND id=$7 AND deleted_at IS NULL AND canonical_model IS NOT NULL`,
		record.Model.Name, statusFor(record), string(raw), int64(record.Model.DraftRevision),
		int64(record.Model.PublishedRevision), string(record.Model.TenantID), string(record.Model.ID))
	if err != nil {
		return persistenceError(err)
	}
	if result.RowsAffected() != 1 {
		return app.ErrNotFound
	}
	return nil
}

func (t *postgresTransaction) Publish(ctx context.Context, publication app.Publication) error {
	snapshot := publication.Snapshot
	if !validUUID(string(snapshot.ID)) || !validUUID(string(snapshot.ModelID)) || !validUUID(string(snapshot.TenantID)) || !validUUID(publication.PublishedBy) || snapshot.Revision == 0 ||
		snapshot.Revision > math.MaxInt32 || !json.Valid(snapshot.Content) || snapshot.Checksum == "" {
		return app.ErrInvalid
	}
	digest := sha256.Sum256(snapshot.Content)
	if hex.EncodeToString(digest[:]) != snapshot.Checksum {
		return app.ErrInvalid
	}
	var raw []byte
	var status string
	var draftRevision, publishedRevision int64
	var updatedAt time.Time
	err := t.tx.QueryRow(ctx, `
		SELECT canonical_model, status, draft_revision, published_revision, updated_at
		FROM thing_models
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND canonical_model IS NOT NULL
		FOR UPDATE`, string(snapshot.TenantID), string(snapshot.ModelID),
	).Scan(&raw, &status, &draftRevision, &publishedRevision, &updatedAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return app.ErrNotFound
	}
	if err != nil {
		return persistenceError(err)
	}
	if strings.EqualFold(status, "ARCHIVED") {
		return app.ErrArchived
	}
	if snapshot.Revision != uint64(publishedRevision+1) {
		return app.ErrConflict
	}
	var doc canonicalDocument
	if err := decodeCanonical(raw, &doc); err != nil {
		return err
	}
	if doc.Model.ID != snapshot.ModelID || doc.Model.TenantID != snapshot.TenantID || doc.Model.DraftRevision != uint64(draftRevision) || doc.Model.PublishedRevision != uint64(publishedRevision) {
		return app.ErrInvalid
	}
	canonicalSnapshot, err := core.BuildSnapshot(doc.Model, snapshot.ID)
	if err != nil || canonicalSnapshot.Revision != snapshot.Revision || canonicalSnapshot.Checksum != snapshot.Checksum || string(canonicalSnapshot.Content) != string(snapshot.Content) {
		return app.ErrInvalid
	}
	currentResources, err := readResources(ctx, t.tx, snapshot.TenantID, snapshot.ModelID, true)
	if err != nil {
		return err
	}
	currentResourcesChecksum, err := app.ChecksumResources(currentResources)
	if err != nil || currentResources.Revision != publication.Resources.Revision || currentResourcesChecksum != publication.ResourcesChecksum {
		return app.ErrConflict
	}
	resourcesJSON, err := json.Marshal(currentResources.Document)
	if err != nil || publication.ResourcesChecksum == "" || publication.Resources.Revision > math.MaxInt64 {
		return app.ErrInvalid
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO thing_model_snapshots
			(id, thing_model_id, tenant_id, version, content, changelog, published_by, published_at, canonical_snapshot, canonical_content, canonical_checksum, resources_content, resources_revision, resources_checksum)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,NOW(),TRUE,$8,$9,$10::jsonb,$11,$12)`,
		string(snapshot.ID), string(snapshot.ModelID), string(snapshot.TenantID), int64(snapshot.Revision),
		string(snapshot.Content), publication.Changelog, publication.PublishedBy, snapshot.Content, snapshot.Checksum, string(resourcesJSON), int64(currentResources.Revision), currentResourcesChecksum)
	if err != nil {
		return persistenceError(err)
	}
	doc.Model.PublishedRevision = snapshot.Revision
	_, raw, err = encodeDocument(doc)
	if err != nil {
		return app.ErrInvalid
	}
	result, err := t.tx.Exec(ctx, `
		UPDATE thing_models SET canonical_model=$1::jsonb, published_revision=$2,
			status='PUBLISHED', current_snapshot_id=$3, updated_at=NOW()
		WHERE tenant_id=$4 AND id=$5 AND canonical_model IS NOT NULL`,
		string(raw), int64(snapshot.Revision), string(snapshot.ID), string(snapshot.TenantID), string(snapshot.ModelID))
	if err != nil {
		return persistenceError(err)
	}
	if result.RowsAffected() != 1 {
		return app.ErrNotFound
	}
	return nil
}

func (t *postgresTransaction) Audit(ctx context.Context, audit app.Audit) error {
	if !validUUID(audit.ID) || !validUUID(audit.SubjectID) || audit.Operation == "" || !validUUID(string(audit.After.Model.ID)) || !validUUID(string(audit.After.Model.TenantID)) {
		return app.ErrInvalid
	}
	if audit.Before != nil && (audit.Before.Model.ID != audit.After.Model.ID || audit.Before.Model.TenantID != audit.After.Model.TenantID) {
		return app.ErrInvalid
	}
	var before any
	if audit.Before != nil {
		before = audit.Before
	}
	afterJSON, err := json.Marshal(audit.After)
	if err != nil {
		return app.ErrInvalid
	}
	var beforeJSON any
	if before != nil {
		encoded, err := json.Marshal(before)
		if err != nil {
			return app.ErrInvalid
		}
		beforeJSON = string(encoded)
	}
	result, err := t.tx.Exec(ctx, `
		INSERT INTO thing_model_audit_log
			(id, tenant_id, actor_id, action, target_type, target_id, before, after, occurred_at)
		SELECT $1,$2,$3,$4,'THING_MODEL',$5,$6::jsonb,$7::jsonb,NOW()
		FROM thing_models WHERE tenant_id=$2 AND id=$5 AND canonical_model IS NOT NULL AND deleted_at IS NULL`,
		audit.ID, string(audit.After.Model.TenantID), audit.SubjectID, "thingmodel."+audit.Operation,
		string(audit.After.Model.ID), beforeJSON, string(afterJSON))
	if err != nil {
		return persistenceError(err)
	}
	if result.RowsAffected() != 1 {
		return app.ErrNotFound
	}
	return nil
}

func (t *postgresTransaction) Emit(ctx context.Context, event app.Event) error {
	if !validUUID(event.ID) || event.Operation == "" || !validUUID(string(event.ModelID)) || !validUUID(string(event.TenantID)) || event.DraftRevision == 0 || event.DraftRevision > math.MaxInt64 {
		return app.ErrInvalid
	}
	if event.Snapshot != nil && (event.Snapshot.ModelID != event.ModelID || event.Snapshot.TenantID != event.TenantID) {
		return app.ErrInvalid
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return app.ErrInvalid
	}
	result, err := t.tx.Exec(ctx, `
		INSERT INTO thing_model_outbox
			(id, tenant_id, model_id, operation, draft_revision, payload, created_at)
		SELECT $1,$2,$3,$4,$5,$6::jsonb,NOW()
		FROM thing_models WHERE tenant_id=$2 AND id=$3 AND canonical_model IS NOT NULL AND deleted_at IS NULL`,
		event.ID, string(event.TenantID), string(event.ModelID), event.Operation, int64(event.DraftRevision), string(payload))
	if err != nil {
		return persistenceError(err)
	}
	if result.RowsAffected() != 1 {
		return app.ErrNotFound
	}
	return nil
}

func validateRecord(record app.Record) error {
	model := record.Model
	if !validUUID(string(model.ID)) || !validUUID(string(model.TenantID)) || strings.TrimSpace(model.Name) == "" || model.DraftRevision > math.MaxInt64 || model.PublishedRevision > math.MaxInt64 {
		return app.ErrInvalid
	}
	if record.Lifecycle == "" {
		return app.ErrInvalid
	}
	return nil
}

func validReceiptScope(scope app.ReceiptScope) bool {
	if !validUUID(string(scope.TenantID)) || !validUUID(scope.SubjectID) || strings.TrimSpace(scope.Operation) == "" || len(scope.Key) < 8 || len(scope.Key) > 128 || len(scope.PayloadHash) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(scope.PayloadHash)
	return err == nil
}

func validUUID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

func encodeModel(model core.Model) (canonicalDocument, []byte, error) {
	return encodeDocument(canonicalDocument{SchemaVersion: canonicalSchemaVersion, Model: model})
}

func encodeDocument(doc canonicalDocument) (canonicalDocument, []byte, error) {
	if doc.SchemaVersion != canonicalSchemaVersion {
		return canonicalDocument{}, nil, app.ErrInvalid
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return canonicalDocument{}, nil, err
	}
	return doc, raw, nil
}

func decodeCanonical(raw []byte, doc *canonicalDocument) error {
	if !json.Valid(raw) || json.Unmarshal(raw, doc) != nil || doc.SchemaVersion != canonicalSchemaVersion || !validUUID(string(doc.Model.ID)) || !validUUID(string(doc.Model.TenantID)) || strings.TrimSpace(doc.Model.Name) == "" {
		return app.ErrInvalid
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return app.ErrInvalid
	}
	var model map[string]json.RawMessage
	if json.Unmarshal(top["model"], &model) != nil {
		return app.ErrInvalid
	}
	if _, ok := model["fields"]; !ok {
		return app.ErrInvalid
	}
	return nil
}

func decodeRecord(raw []byte, status string, draftRevision, publishedRevision int64, updatedAt time.Time, tenantID core.TenantID, modelID core.ModelID) (app.Record, error) {
	if draftRevision < 0 || publishedRevision < 0 {
		return app.Record{}, app.ErrInvalid
	}
	var doc canonicalDocument
	if err := decodeCanonical(raw, &doc); err != nil {
		return app.Record{}, err
	}
	model := doc.Model
	if model.TenantID != tenantID || model.ID != modelID || model.DraftRevision != uint64(draftRevision) || model.PublishedRevision != uint64(publishedRevision) {
		return app.Record{}, app.ErrInvalid
	}
	return app.Record{Model: model, Lifecycle: lifecycleFromStatus(status), UpdatedAt: updatedAt}, nil
}

func statusFor(record app.Record) string {
	switch strings.ToUpper(strings.TrimSpace(record.Lifecycle)) {
	case "ARCHIVED":
		return "ARCHIVED"
	case "PUBLISHED":
		return "PUBLISHED"
	case "DRAFT":
		return "DRAFT"
	default:
		if record.Model.PublishedRevision > 0 {
			return "PUBLISHED"
		}
		return "DRAFT"
	}
}

func lifecycleFromStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "ARCHIVED":
		return "ARCHIVED"
	case "PUBLISHED":
		return "PUBLISHED"
	default:
		return "DRAFT"
	}
}

type publicationScanner interface {
	Scan(dest ...any) error
}

func scanPublication(row publicationScanner, tenantID core.TenantID, modelID core.ModelID) (app.Publication, error) {
	var id, checksum, changelog, publishedBy string
	var content []byte
	var resourcesContent []byte
	var resourcesRevision sql.NullInt64
	var resourcesChecksum sql.NullString
	var publishedAt time.Time
	if err := row.Scan(&id, &content, &checksum, &changelog, &publishedBy, &publishedAt, &resourcesContent, &resourcesRevision, &resourcesChecksum); err != nil {
		return app.Publication{}, persistenceError(err)
	}
	return decodePublicationWithResources(id, content, checksum, changelog, publishedBy, publishedAt, tenantID, modelID, resourcesContent, resourcesRevision, resourcesChecksum)
}

type canonicalSnapshotContent struct {
	ModelID     core.ModelID     `json:"model_id"`
	TenantID    core.TenantID    `json:"tenant_id"`
	Name        string           `json:"name"`
	Description *core.I18nString `json:"description,omitempty"`
	Category    *string          `json:"category,omitempty"`
	Revision    uint64           `json:"revision"`
	Fields      []core.Field     `json:"fields"`
}

func decodePublication(id string, content []byte, checksum, changelog, publishedBy string, publishedAt time.Time, tenantID core.TenantID, modelID core.ModelID) (app.Publication, error) {
	return decodePublicationWithResources(id, content, checksum, changelog, publishedBy, publishedAt, tenantID, modelID, nil, sql.NullInt64{}, sql.NullString{})
}

func decodePublicationWithResources(id string, content []byte, checksum, changelog, publishedBy string, publishedAt time.Time, tenantID core.TenantID, modelID core.ModelID, resourcesRaw []byte, resourceRevision sql.NullInt64, resourceChecksum sql.NullString) (app.Publication, error) {
	var payload canonicalSnapshotContent
	if !json.Valid(content) || json.Unmarshal(content, &payload) != nil || payload.ModelID != modelID || payload.TenantID != tenantID || payload.Revision == 0 || checksum == "" {
		return app.Publication{}, app.ErrInvalid
	}
	snapshot, err := core.BuildSnapshot(core.Model{
		ID: payload.ModelID, TenantID: payload.TenantID, Name: payload.Name,
		Description: payload.Description, Category: payload.Category, Fields: payload.Fields,
		DraftRevision: payload.Revision, PublishedRevision: payload.Revision - 1,
	}, core.SnapshotID(id))
	if err != nil || snapshot.Checksum != checksum || string(snapshot.Content) != string(content) {
		return app.Publication{}, app.ErrInvalid
	}
	resources := app.ResourcesRecord{Document: app.ResourcesDocument{Profile: app.ResourceProfile{Tags: []string{}}}, Revision: 0}
	storedChecksum := ""
	if len(resourcesRaw) != 0 {
		if !resourceRevision.Valid || resourceRevision.Int64 < 0 || !resourceChecksum.Valid || json.Unmarshal(resourcesRaw, &resources.Document) != nil {
			return app.Publication{}, app.ErrInvalid
		}
		resources.Revision, storedChecksum = uint64(resourceRevision.Int64), strings.TrimSpace(resourceChecksum.String)
		computed, err := app.ChecksumResources(resources)
		if err != nil || computed != storedChecksum {
			return app.Publication{}, app.ErrInvalid
		}
	}
	return app.Publication{Snapshot: snapshot, Changelog: changelog, PublishedBy: publishedBy, CreatedAt: publishedAt, Resources: resources, ResourcesChecksum: storedChecksum}, nil
}

func persistenceError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, sqldb.ErrNoRows) {
		return app.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return app.ErrConflict
	}
	// Never return SQL text or driver errors through the API boundary.
	return fmt.Errorf("thingmodel: persistence failed")
}

var _ app.Repository = postgresRepository{}
var _ app.Transaction = (*postgresTransaction)(nil)
