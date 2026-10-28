package devicemetadata

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"encore.dev/storage/sqldb/sqlerr"
	"github.com/google/uuid"
)

func createThingModel(ctx context.Context, tenantID string, req *ThingModel) (*ThingModel, error) {
	if issues := validateThingModelPayload(req); len(issues) > 0 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: issues[0].Message}
	}
	id := newID()
	actorID := currentUserID()
	if err := db.QueryRow(ctx, `
		INSERT INTO thing_models (id, tenant_id, name, description_i18n, category, status, created_by, updated_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, COALESCE(NULLIF($6, ''), 'DRAFT'), $7, $7, NOW(), NOW())
		RETURNING id, tenant_id, name, description_i18n, category, status, current_snapshot_id, web_chart_config, app_chart_config, created_at, updated_at
	`, id, tenantID, req.Name, jsonOrNull(req.DescriptionI18n), req.Category, req.Status, actorID).Scan(
		&req.ID, &req.TenantID, &req.Name, &req.DescriptionI18n, &req.Category, &req.Status, &req.CurrentSnapshotID, &req.WebChartConfig, &req.AppChartConfig, &req.CreatedAt, &req.UpdatedAt,
	); err != nil {
		if isUniqueViolation(err, "thing_models_tenant_id_name_key") {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "thing model name already exists"}
		}
		return nil, wrapDBErr(err, "thing model not found")
	}
	return req, nil
}

func getThingModelByID(ctx context.Context, tenantID, id string) (*ThingModel, error) {
	var out ThingModel
	err := db.QueryRow(ctx, `
		SELECT id, tenant_id, name, description_i18n, COALESCE(category,''), status, current_snapshot_id, web_chart_config, app_chart_config, created_at, updated_at
		FROM thing_models
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(&out.ID, &out.TenantID, &out.Name, &out.DescriptionI18n, &out.Category, &out.Status, &out.CurrentSnapshotID, &out.WebChartConfig, &out.AppChartConfig, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, wrapDBErr(err, "thing model not found")
	}
	return &out, nil
}

func listThingModels(ctx context.Context, tenantID string, q *ThingModelListReq) ([]ThingModel, int, error) {
	q.pageRequest.normalize()
	search := "%" + strings.TrimSpace(q.Search) + "%"
	rows, err := db.Query(ctx, `
		SELECT id, tenant_id, name, description_i18n, COALESCE(category,''), status, current_snapshot_id, web_chart_config, app_chart_config, created_at, updated_at,
		       COUNT(*) OVER()
		FROM thing_models
		WHERE tenant_id = $1 AND deleted_at IS NULL
		  AND ($2 = '%%' OR name ILIKE $2)
		  AND ($3 = '' OR category = $3)
		  AND ($4 = '' OR status = $4)
		ORDER BY updated_at DESC
		LIMIT $5 OFFSET $6
	`, tenantID, search, q.Category, q.Status, q.PageSize, (q.Page-1)*q.PageSize)
	if err != nil {
		return nil, 0, wrapDBErr(err, "list thing models failed")
	}
	defer rows.Close()
	var items []ThingModel
	total := 0
	for rows.Next() {
		var item ThingModel
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.DescriptionI18n, &item.Category, &item.Status, &item.CurrentSnapshotID, &item.WebChartConfig, &item.AppChartConfig, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, wrapDBErr(err, "list thing models failed")
		}
		items = append(items, item)
	}
	return items, total, nil
}

func updateThingModel(ctx context.Context, tenantID, id string, req *ThingModel) (*ThingModel, error) {
	if issues := validateThingModelPayload(req); len(issues) > 0 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: issues[0].Message}
	}
	var out ThingModel
	actorID := currentUserID()
	err := db.QueryRow(ctx, `
		UPDATE thing_models
		SET name = $3, description_i18n = $4, category = $5, web_chart_config = COALESCE($7, web_chart_config), app_chart_config = COALESCE($8, app_chart_config), updated_by = $6, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING id, tenant_id, name, description_i18n, category, status, current_snapshot_id, web_chart_config, app_chart_config, created_at, updated_at
	`, tenantID, id, req.Name, jsonOrNull(req.DescriptionI18n), req.Category, actorID, jsonOrNull(req.WebChartConfig), jsonOrNull(req.AppChartConfig)).Scan(
		&out.ID, &out.TenantID, &out.Name, &out.DescriptionI18n, &out.Category, &out.Status, &out.CurrentSnapshotID, &out.WebChartConfig, &out.AppChartConfig, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err, "thing_models_tenant_id_name_key") {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "thing model name already exists"}
		}
		return nil, wrapDBErr(err, "thing model not found")
	}
	return &out, nil
}

func deleteThingModel(ctx context.Context, tenantID, id string) error {
	res, err := db.Exec(ctx, `UPDATE thing_models SET deleted_at = NOW(), updated_at = NOW() WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id)
	if err != nil {
		return wrapDBErr(err, "thing model not found")
	}
	if n := res.RowsAffected(); n == 0 {
		return wrapDBErr(sqldb.ErrNoRows, "thing model not found")
	}
	return nil
}

func publishThingModel(ctx context.Context, tenantID, id, publishedBy string, req *publishReq) (*ThingModelSnapshot, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, wrapDBErr(err, "publish failed")
	}
	defer tx.Rollback()

	model, err := getThingModelByIDTx(ctx, tx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if model.Status != "DRAFT" && model.Status != "PUBLISHED" {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "archived thing models cannot be published"}
	}
	items, err := listThingModelItemsAllTx(ctx, tx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if err := validateSnapshotItems(items); err != nil {
		return nil, err
	}

	if model.Status == "PUBLISHED" && model.CurrentSnapshotID != nil {
		candidate, err := buildSnapshotContent(model, items, 0)
		if err != nil {
			return nil, err
		}
		var prior json.RawMessage
		if err = tx.QueryRow(ctx, `SELECT content FROM thing_model_snapshots WHERE tenant_id=$1 AND thing_model_id=$2 AND id=$3`, tenantID, id, *model.CurrentSnapshotID).Scan(&prior); err != nil {
			return nil, wrapDBErr(err, "current snapshot unavailable")
		}
		equal, err := samePublicationContent(prior, nowJSON(candidate))
		if err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "current snapshot content is invalid"}
		}
		if equal {
			return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "no changes to publish"}
		}
	}

	var version int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM thing_model_snapshots WHERE tenant_id = $1 AND thing_model_id = $2`, tenantID, id).Scan(&version); err != nil {
		return nil, wrapDBErr(err, "publish failed")
	}
	payload, err := buildSnapshotContent(model, items, version)
	if err != nil {
		return nil, err
	}
	content := nowJSON(payload)
	if err := validateSnapshotPayloadSize(content); err != nil {
		return nil, err
	}

	var snap ThingModelSnapshot
	snapID := newID()
	err = tx.QueryRow(ctx, `
		INSERT INTO thing_model_snapshots (id, thing_model_id, tenant_id, version, content, changelog, published_at, published_by)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), $7)
		RETURNING id, thing_model_id, tenant_id, version, content, changelog, published_at, published_by
	`, snapID, id, tenantID, version, content, req.Changelog, publishedBy).Scan(
		&snap.ID, &snap.ThingModelID, &snap.TenantID, &snap.Version, &snap.Content, &snap.Changelog, &snap.PublishedAt, &snap.PublishedBy,
	)
	if err != nil {
		if isUniqueViolation(err, "thing_model_snapshots_thing_model_id_version_key") {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "thing model version conflict"}
		}
		return nil, wrapDBErr(err, "publish failed")
	}
	_, err = tx.Exec(ctx, `UPDATE thing_models SET status = 'PUBLISHED', current_snapshot_id = $3, updated_by = $4, updated_at = NOW() WHERE tenant_id = $1 AND id = $2`, tenantID, id, snapID, publishedBy)
	if err != nil {
		return nil, wrapDBErr(err, "publish failed")
	}
	if err := insertAuditLogTx(ctx, tx, tenantID, publishedBy, "THING_MODEL_PUBLISHED", "THING_MODEL", id, nil, content); err != nil {
		return nil, wrapDBErr(err, "publish failed")
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "publish failed")
	}
	return &snap, nil
}

func listVersions(ctx context.Context, tenantID, thingModelID string) ([]ThingModelSnapshot, error) {
	rows, err := db.Query(ctx, `
		SELECT id, thing_model_id, tenant_id, version, content, changelog, published_at, published_by
		FROM thing_model_snapshots
		WHERE tenant_id = $1 AND thing_model_id = $2
		ORDER BY version DESC
	`, tenantID, thingModelID)
	if err != nil {
		return nil, wrapDBErr(err, "versions not found")
	}
	defer rows.Close()
	var out []ThingModelSnapshot
	for rows.Next() {
		var item ThingModelSnapshot
		if err := rows.Scan(&item.ID, &item.ThingModelID, &item.TenantID, &item.Version, &item.Content, &item.Changelog, &item.PublishedAt, &item.PublishedBy); err != nil {
			return nil, wrapDBErr(err, "versions not found")
		}
		out = append(out, item)
	}
	return out, nil
}

func getVersion(ctx context.Context, tenantID, thingModelID string, version int) (*ThingModelSnapshot, error) {
	var out ThingModelSnapshot
	err := db.QueryRow(ctx, `
		SELECT id, thing_model_id, tenant_id, version, content, changelog, published_at, published_by
		FROM thing_model_snapshots
		WHERE tenant_id = $1 AND thing_model_id = $2 AND version = $3
	`, tenantID, thingModelID, version).Scan(&out.ID, &out.ThingModelID, &out.TenantID, &out.Version, &out.Content, &out.Changelog, &out.PublishedAt, &out.PublishedBy)
	if err != nil {
		return nil, wrapDBErr(err, "version not found")
	}
	return &out, nil
}

func loadSnapshotByVersion(ctx context.Context, tenantID, thingModelID string, version int) (*ThingModelSnapshot, error) {
	return getVersion(ctx, tenantID, thingModelID, version)
}

func rollbackThingModel(ctx context.Context, tenantID, thingModelID, actorID string, req *rollbackReq) (*ThingModelSnapshot, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, wrapDBErr(err, "rollback failed")
	}
	defer tx.Rollback()

	current, err := getThingModelByIDTx(ctx, tx, tenantID, thingModelID)
	if err != nil {
		return nil, err
	}
	beforeContent, err := collectSnapshotContentTx(ctx, tx, thingModelID, tenantID, 0)
	if err != nil {
		return nil, err
	}
	snap, err := loadSnapshotByVersionTx(ctx, tx, tenantID, thingModelID, req.TargetVersion)
	if err != nil {
		return nil, err
	}
	targetContent, err := parseSnapshotContent(snap.Content)
	if err != nil {
		return nil, err
	}
	if err := restoreThingModelItemsFromSnapshot(ctx, tx, tenantID, thingModelID, targetContent); err != nil {
		return nil, wrapDBErr(err, "rollback failed")
	}

	var nextVersion int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM thing_model_snapshots WHERE tenant_id = $1 AND thing_model_id = $2`, tenantID, thingModelID).Scan(&nextVersion); err != nil {
		return nil, wrapDBErr(err, "rollback failed")
	}
	targetContent.Version = nextVersion
	content := nowJSON(targetContent)
	if err := validateSnapshotPayloadSize(content); err != nil {
		return nil, err
	}

	newSnap := &ThingModelSnapshot{}
	newID := newID()
	changelog := fmt.Sprintf("rollback to version %d", req.TargetVersion)
	if trimmed := strings.TrimSpace(req.Changelog); trimmed != "" {
		changelog = changelog + ": " + trimmed
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO thing_model_snapshots (id, thing_model_id, tenant_id, version, content, changelog, published_at, published_by)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), $7)
		RETURNING id, thing_model_id, tenant_id, version, content, changelog, published_at, published_by
	`, newID, thingModelID, tenantID, nextVersion, content, changelog, actorID).Scan(
		&newSnap.ID, &newSnap.ThingModelID, &newSnap.TenantID, &newSnap.Version, &newSnap.Content, &newSnap.Changelog, &newSnap.PublishedAt, &newSnap.PublishedBy,
	); err != nil {
		return nil, wrapDBErr(err, "rollback failed")
	}
	_, err = tx.Exec(ctx, `UPDATE thing_models SET status = 'PUBLISHED', current_snapshot_id = $3, updated_by = $4, updated_at = NOW() WHERE tenant_id = $1 AND id = $2`, tenantID, thingModelID, newSnap.ID, actorID)
	if err != nil {
		return nil, wrapDBErr(err, "rollback failed")
	}
	if err := insertAuditLogTx(ctx, tx, tenantID, actorID, "THING_MODEL_ROLLBACK", "THING_MODEL", thingModelID, beforeContent, content); err != nil {
		return nil, wrapDBErr(err, "rollback failed")
	}
	_ = current
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "rollback failed")
	}
	return newSnap, nil
}

func archiveThingModel(ctx context.Context, tenantID, id string) error {
	_, err := db.Exec(ctx, `UPDATE thing_models SET status = 'ARCHIVED', updated_at = NOW() WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return wrapDBErr(err, "archive failed")
}

func deriveThingModelDraft(ctx context.Context, tenantID, id string, req *deriveDraftReq) (*ThingModel, error) {
	src, err := getThingModelByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = src.Name + " Draft"
	}
	newModel := &ThingModel{
		Name:            name,
		DescriptionI18n: src.DescriptionI18n,
		Category:        src.Category,
		Status:          "DRAFT",
	}
	dst, err := createThingModel(ctx, tenantID, newModel)
	if err != nil {
		if errs.Code(err) == errs.AlreadyExists && strings.TrimSpace(req.Name) == "" {
			// Default name taken; retry with timestamp suffix
			newModel.Name = fmt.Sprintf("%s Draft %s", src.Name, time.Now().Format("0102-1504"))
			dst, err = createThingModel(ctx, tenantID, newModel)
		}
		if err != nil {
			return nil, err
		}
	}
	items, _, err := listThingModelItems(ctx, tenantID, id, "", "", pageRequest{Page: 1, PageSize: 500})
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		item.ID = ""
		item.ThingModelID = dst.ID
		if _, err := createThingModelItem(ctx, tenantID, dst.ID, &item); err != nil {
			return nil, err
		}
	}
	return dst, nil
}

func diffThingModelVersions(ctx context.Context, tenantID, thingModelID string, fromVersion, toVersion int) (*ThingModelDiffResponse, error) {
	from, err := getVersion(ctx, tenantID, thingModelID, fromVersion)
	if err != nil {
		return nil, err
	}
	to, err := getVersion(ctx, tenantID, thingModelID, toVersion)
	if err != nil {
		return nil, err
	}
	fromContent, err := parseSnapshotContent(from.Content)
	if err != nil {
		return nil, err
	}
	toContent, err := parseSnapshotContent(to.Content)
	if err != nil {
		return nil, err
	}
	diff := computeVersionDiff(fromContent, toContent)
	return &ThingModelDiffResponse{
		FromVersion:  diff.FromVersion,
		ToVersion:    diff.ToVersion,
		AddedItems:   diff.AddedItems,
		RemovedItems: diff.RemovedItems,
		ChangedItems: diff.ChangedItems,
	}, nil
}

type ThingModelListReq struct {
	pageRequest
	Search   string `query:"search"`
	Category string `query:"category"`
	Status   string `query:"status"`
}

func createThingModelItem(ctx context.Context, tenantID, thingModelID string, req *ThingModelItem) (*ThingModelItem, error) {
	if issues := validateThingModelItemPayload(req); len(issues) > 0 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: issues[0].Message}
	}
	out, err := insertThingModelItem(ctx, db, tenantID, thingModelID, req)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func insertThingModelItem(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...interface{}) *sqldb.Row
}, tenantID, thingModelID string, req *ThingModelItem) (*ThingModelItem, error) {
	var out ThingModelItem
	sortOrder := req.SortOrder
	if sortOrder == 0 {
		sortOrder = nextThingModelItemSortOrder(ctx, q, thingModelID)
	}
	err := q.QueryRow(ctx, `
		INSERT INTO thing_model_items (
			id, thing_model_id, tenant_id, type, identifier, name_i18n, description_i18n, value_type, parameters,
			access, web_chart_config, app_chart_config, meta_items, sort_order, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			$10, $11, $12, COALESCE($13, '[]'::jsonb), $14, NOW(), NOW()
		)
		RETURNING id, thing_model_id, tenant_id, type, identifier, name_i18n, description_i18n, value_type,
		          parameters, access, web_chart_config, app_chart_config, meta_items, sort_order, created_at, updated_at
	`, newID(), thingModelID, tenantID, strings.ToUpper(req.Type), req.Identifier, jsonOrNull(req.NameI18n),
		jsonOrNull(req.DescriptionI18n), jsonOrNull(req.ValueType), jsonOrNull(req.Parameters), jsonOrNull(req.Access), jsonOrNull(req.WebChartConfig),
		jsonOrNull(req.AppChartConfig), jsonOrNull(req.MetaItems), sortOrder).Scan(
		&out.ID, &out.ThingModelID, &out.TenantID, &out.Type, &out.Identifier, &out.NameI18n, &out.DescriptionI18n, &out.ValueType,
		&out.Parameters, &out.Access, &out.WebChartConfig, &out.AppChartConfig, &out.MetaItems, &out.SortOrder, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err, "thing_model_items_thing_model_id_identifier_key") {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "thing model identifier already exists"}
		}
		return nil, wrapDBErr(err, "thing model item create failed")
	}
	return &out, nil
}

func listThingModelItems(ctx context.Context, tenantID, thingModelID, kind, query string, page pageRequest) ([]ThingModelItem, int, error) {
	page.normalize()
	search := "%" + strings.TrimSpace(query) + "%"
	rows, err := db.Query(ctx, `
		SELECT id, thing_model_id, tenant_id, type, identifier, name_i18n, description_i18n, value_type, parameters,
		       access, web_chart_config, app_chart_config, meta_items, sort_order, created_at, updated_at,
		       COUNT(*) OVER()
		FROM thing_model_items
		WHERE tenant_id = $1 AND thing_model_id = $2
		  AND ($3 = '' OR type = $3 OR ($3 IN ('PROPERTY', 'TELEMETRY') AND type IN ('PROPERTY', 'TELEMETRY')))
		  AND ($6 = '' OR identifier ILIKE $6 OR COALESCE(name_i18n->>'default', '') ILIKE $6)
		ORDER BY sort_order, created_at
		LIMIT $4 OFFSET $5
	`, tenantID, thingModelID, strings.ToUpper(kind), page.PageSize, (page.Page-1)*page.PageSize, search)
	if err != nil {
		return nil, 0, wrapDBErr(err, "list thing model items failed")
	}
	defer rows.Close()
	var out []ThingModelItem
	total := 0
	for rows.Next() {
		var item ThingModelItem
		if err := rows.Scan(&item.ID, &item.ThingModelID, &item.TenantID, &item.Type, &item.Identifier, &item.NameI18n, &item.DescriptionI18n, &item.ValueType,
			&item.Parameters, &item.Access, &item.WebChartConfig, &item.AppChartConfig, &item.MetaItems, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, wrapDBErr(err, "list thing model items failed")
		}
		out = append(out, item)
	}
	return out, total, nil
}

func getThingModelItem(ctx context.Context, tenantID, thingModelID, id string) (*ThingModelItem, error) {
	var out ThingModelItem
	err := db.QueryRow(ctx, `
		SELECT id, thing_model_id, tenant_id, type, identifier, name_i18n, description_i18n, value_type, parameters,
		       access, web_chart_config, app_chart_config, meta_items, sort_order, created_at, updated_at
		FROM thing_model_items
		WHERE tenant_id = $1 AND thing_model_id = $2 AND id = $3
	`, tenantID, thingModelID, id).Scan(&out.ID, &out.ThingModelID, &out.TenantID, &out.Type, &out.Identifier, &out.NameI18n,
		&out.DescriptionI18n, &out.ValueType, &out.Parameters, &out.Access, &out.WebChartConfig, &out.AppChartConfig, &out.MetaItems, &out.SortOrder, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, wrapDBErr(err, "thing model item not found")
	}
	return &out, nil
}

func updateThingModelItem(ctx context.Context, tenantID, thingModelID, id string, req *ThingModelItem) (*ThingModelItem, error) {
	if issues := validateThingModelItemPayload(req); len(issues) > 0 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: issues[0].Message}
	}
	var out ThingModelItem
	err := db.QueryRow(ctx, `
		UPDATE thing_model_items
		SET type = $4, identifier = $5, name_i18n = $6, description_i18n = $7, value_type = $8, parameters = $9,
		    access = $10, web_chart_config = $11, app_chart_config = $12, meta_items = COALESCE($13, '[]'::jsonb),
		    sort_order = $14, updated_at = NOW()
		WHERE tenant_id = $1 AND thing_model_id = $2 AND id = $3
		RETURNING id, thing_model_id, tenant_id, type, identifier, name_i18n, description_i18n, value_type,
		          parameters, access, web_chart_config, app_chart_config, meta_items, sort_order, created_at, updated_at
	`, tenantID, thingModelID, id, strings.ToUpper(req.Type), req.Identifier, jsonOrNull(req.NameI18n), jsonOrNull(req.DescriptionI18n),
		jsonOrNull(req.ValueType), jsonOrNull(req.Parameters), jsonOrNull(req.Access), jsonOrNull(req.WebChartConfig), jsonOrNull(req.AppChartConfig),
		jsonOrNull(req.MetaItems), req.SortOrder).Scan(&out.ID, &out.ThingModelID, &out.TenantID, &out.Type, &out.Identifier, &out.NameI18n, &out.DescriptionI18n, &out.ValueType,
		&out.Parameters, &out.Access, &out.WebChartConfig, &out.AppChartConfig, &out.MetaItems, &out.SortOrder, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err, "thing_model_items_thing_model_id_identifier_key") {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "thing model identifier already exists"}
		}
		return nil, wrapDBErr(err, "thing model item not found")
	}
	return &out, nil
}

func deleteThingModelItem(ctx context.Context, tenantID, thingModelID, id string) error {
	res, err := db.Exec(ctx, `DELETE FROM thing_model_items WHERE tenant_id = $1 AND thing_model_id = $2 AND id = $3`, tenantID, thingModelID, id)
	if err != nil {
		return wrapDBErr(err, "thing model item not found")
	}
	if n := res.RowsAffected(); n == 0 {
		return wrapDBErr(sqldb.ErrNoRows, "thing model item not found")
	}
	return nil
}

// deleteThingModelItemByID deletes an item using only its ID (no thingModelID required).
// Looks up the thingModelID internally from the item row.
func deleteThingModelItemByID(ctx context.Context, tenantID, id string) error {
	res, err := db.Exec(ctx, `DELETE FROM thing_model_items WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return wrapDBErr(err, "thing model item not found")
	}
	if n := res.RowsAffected(); n == 0 {
		return wrapDBErr(sqldb.ErrNoRows, "thing model item not found")
	}
	return nil
}

// updateThingModelItemByID updates an item using only its ID, resolving thingModelID internally.
func updateThingModelItemByID(ctx context.Context, tenantID, id string, req *ThingModelItem) (*ThingModelItem, error) {
	var thingModelID string
	if err := db.QueryRow(ctx, `SELECT thing_model_id FROM thing_model_items WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&thingModelID); err != nil {
		return nil, wrapDBErr(err, "thing model item not found")
	}
	return updateThingModelItem(ctx, tenantID, thingModelID, id, req)
}

func jsonOrNull(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func currentUserID() string {
	data := authn.CurrentData()
	if data == nil || strings.TrimSpace(data.UserID) == "" {
		return uuid.Nil.String()
	}
	return normalizePrincipalID(data.UserID)
}

func isUniqueViolation(err error, constraint string) bool {
	if sqldb.ErrCode(err) != sqlerr.UniqueViolation {
		return false
	}
	var sqlErr *sqldb.Error
	if errors.As(err, &sqlErr) && constraint != "" {
		return sqlErr.ConstraintName == constraint
	}
	return true
}

func nextThingModelItemSortOrder(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...interface{}) *sqldb.Row
}, thingModelID string) int {
	var next int
	if err := q.QueryRow(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 100 FROM thing_model_items WHERE thing_model_id = $1`, thingModelID).Scan(&next); err != nil {
		return 100
	}
	return next
}

func insertThingModelItemTx(ctx context.Context, tx *sqldb.Tx, tenantID, thingModelID string, req *ThingModelItem) error {
	_, err := insertThingModelItem(ctx, tx, tenantID, thingModelID, req)
	return err
}

func getThingModelByIDTx(ctx context.Context, tx *sqldb.Tx, tenantID, id string) (*ThingModel, error) {
	var out ThingModel
	err := tx.QueryRow(ctx, `
		SELECT id, tenant_id, name, description_i18n, COALESCE(category,''), status, current_snapshot_id, web_chart_config, app_chart_config, created_at, updated_at
		FROM thing_models
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
        FOR UPDATE
	`, tenantID, id).Scan(&out.ID, &out.TenantID, &out.Name, &out.DescriptionI18n, &out.Category, &out.Status, &out.CurrentSnapshotID, &out.WebChartConfig, &out.AppChartConfig, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, wrapDBErr(err, "thing model not found")
	}
	return &out, nil
}

func listThingModelItemsAllTx(ctx context.Context, tx *sqldb.Tx, tenantID, thingModelID string) ([]ThingModelItem, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, thing_model_id, tenant_id, type, identifier, name_i18n, description_i18n, value_type, parameters,
		       access, web_chart_config, app_chart_config, meta_items, sort_order, created_at, updated_at
		FROM thing_model_items
		WHERE tenant_id = $1 AND thing_model_id = $2
		ORDER BY sort_order, created_at
	`, tenantID, thingModelID)
	if err != nil {
		return nil, wrapDBErr(err, "list thing model items failed")
	}
	defer rows.Close()
	var out []ThingModelItem
	for rows.Next() {
		var item ThingModelItem
		if err := rows.Scan(&item.ID, &item.ThingModelID, &item.TenantID, &item.Type, &item.Identifier, &item.NameI18n, &item.DescriptionI18n, &item.ValueType,
			&item.Parameters, &item.Access, &item.WebChartConfig, &item.AppChartConfig, &item.MetaItems, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, wrapDBErr(err, "list thing model items failed")
		}
		out = append(out, item)
	}
	return out, nil
}

func collectSnapshotContentTx(ctx context.Context, tx *sqldb.Tx, thingModelID, tenantID string, version int) (json.RawMessage, error) {
	model, err := getThingModelByIDTx(ctx, tx, tenantID, thingModelID)
	if err != nil {
		return nil, err
	}
	items, err := listThingModelItemsAllTx(ctx, tx, tenantID, thingModelID)
	if err != nil {
		return nil, err
	}
	payload, err := buildSnapshotContent(model, items, version)
	if err != nil {
		return nil, err
	}
	return nowJSON(payload), nil
}

func loadSnapshotByVersionTx(ctx context.Context, tx *sqldb.Tx, tenantID, thingModelID string, version int) (*ThingModelSnapshot, error) {
	var out ThingModelSnapshot
	err := tx.QueryRow(ctx, `
		SELECT id, thing_model_id, tenant_id, version, content, changelog, published_at, published_by
		FROM thing_model_snapshots
		WHERE tenant_id = $1 AND thing_model_id = $2 AND version = $3
	`, tenantID, thingModelID, version).Scan(&out.ID, &out.ThingModelID, &out.TenantID, &out.Version, &out.Content, &out.Changelog, &out.PublishedAt, &out.PublishedBy)
	if err != nil {
		return nil, wrapDBErr(err, "version not found")
	}
	return &out, nil
}

func insertAuditLogTx(ctx context.Context, tx *sqldb.Tx, tenantID, actorID, action, targetType, targetID string, before, after json.RawMessage) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO thing_model_audit_log (id, tenant_id, actor_id, action, target_type, target_id, before, after, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
	`, newID(), tenantID, actorID, action, targetType, targetID, jsonOrNull(before), jsonOrNull(after))
	return err
}

func createDeviceTemplate(ctx context.Context, tenantID string, req *DeviceTemplate) (*DeviceTemplate, error) {
	// Validate required fields
	if err := ensureNonEmpty("name", req.Name); err != nil {
		return nil, err
	}
	if err := ensureNonEmpty("thing_model_id", req.ThingModelID); err != nil {
		return nil, err
	}
	if err := ensureNonEmpty("thing_model_snapshot_id", req.ThingModelSnapshotID); err != nil {
		return nil, err
	}

	// Verify thing_model exists and belongs to tenant
	tm, err := getThingModelByID(ctx, tenantID, req.ThingModelID)
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "thing model not found"}
	}

	// Verify snapshot exists and belongs to the thing model
	var snapshotTMID string
	if err := db.QueryRow(ctx, `
		SELECT thing_model_id FROM thing_model_snapshots
		WHERE id = $1 AND tenant_id = $2
	`, req.ThingModelSnapshotID, tenantID).Scan(&snapshotTMID); err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "thing model snapshot not found"}
	}
	if snapshotTMID != tm.ID {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "snapshot does not belong to the specified thing model"}
	}

	var out DeviceTemplate
	id := newID()
	err = db.QueryRow(ctx, `
		INSERT INTO device_templates (
			id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
			protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
			tags, extras, description_i18n, image_url, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, COALESCE(NULLIF($12, ''), 'DIRECT'), $13, $14,
			COALESCE($15, '[]'::jsonb), COALESCE($16, '{}'::jsonb), $17, $18, COALESCE(NULLIF($19, ''), 'DRAFT'), NOW(), NOW()
		)
		RETURNING id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
		          protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
		          tags, extras, description_i18n, image_url, status, created_at, updated_at
	`, id, tenantID, req.Name, req.Brand, req.ProductName, req.ModelNumber, req.ThingModelID, req.ThingModelSnapshotID,
		req.ProtocolType, jsonOrNull(req.ProtocolConfig), req.VoucherType, req.ConnectionType, req.AutoRegister, jsonOrNull(req.OnlineConfig),
		jsonOrNull(req.Tags), jsonOrNull(req.Extras), jsonOrNull(req.DescriptionI18n), req.ImageURL, req.Status).Scan(
		&out.ID, &out.TenantID, &out.Name, &out.Brand, &out.ProductName, &out.ModelNumber, &out.ThingModelID, &out.ThingModelSnapshotID,
		&out.ProtocolType, &out.ProtocolConfig, &out.VoucherType, &out.ConnectionType, &out.AutoRegister, &out.OnlineConfig,
		&out.Tags, &out.Extras, &out.DescriptionI18n, &out.ImageURL, &out.Status, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err, "device_templates_tenant_id_name_key") {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "device template name already exists for this tenant"}
		}
		return nil, wrapDBErr(err, "device template create failed")
	}
	return &out, nil
}

func listDeviceTemplates(ctx context.Context, tenantID string, req *ListDeviceTemplatesReq) ([]DeviceTemplate, int, error) {
	req.pageRequest.normalize()

	query := strings.Builder{}
	query.WriteString(`
		SELECT id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
		       protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
		       tags, extras, description_i18n, image_url, status, created_at, updated_at,
		       COUNT(*) OVER()
		FROM device_templates
		WHERE tenant_id = $1 AND deleted_at IS NULL
	`)

	args := []interface{}{tenantID}
	argNum := 2

	// Status filter
	if strings.TrimSpace(req.Status) != "" {
		query.WriteString(fmt.Sprintf(` AND status = $%d`, argNum))
		args = append(args, req.Status)
		argNum++
	}

	// Brand filter
	if strings.TrimSpace(req.Brand) != "" {
		query.WriteString(fmt.Sprintf(` AND brand = $%d`, argNum))
		args = append(args, req.Brand)
		argNum++
	}

	// ThingModelID filter
	if strings.TrimSpace(req.ThingModelID) != "" {
		query.WriteString(fmt.Sprintf(` AND thing_model_id = $%d`, argNum))
		args = append(args, req.ThingModelID)
		argNum++
	}

	// Tag filter - check if tag exists in the JSONB array
	if strings.TrimSpace(req.Tag) != "" {
		query.WriteString(fmt.Sprintf(` AND tags @> $%d::jsonb`, argNum))
		// Convert tag string to JSON array with single element
		args = append(args, fmt.Sprintf(`["%s"]`, strings.TrimSpace(req.Tag)))
		argNum++
	}

	// Query filter - fuzzy search on name
	if strings.TrimSpace(req.Query) != "" {
		query.WriteString(fmt.Sprintf(` AND name ILIKE $%d`, argNum))
		args = append(args, "%"+strings.TrimSpace(req.Query)+"%")
		argNum++
	}

	query.WriteString(fmt.Sprintf(` ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, argNum, argNum+1))
	args = append(args, req.PageSize, (req.Page-1)*req.PageSize)

	rows, err := db.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, 0, wrapDBErr(err, "list templates failed")
	}
	defer rows.Close()

	var out []DeviceTemplate
	total := 0
	for rows.Next() {
		var item DeviceTemplate
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Brand, &item.ProductName, &item.ModelNumber, &item.ThingModelID, &item.ThingModelSnapshotID,
			&item.ProtocolType, &item.ProtocolConfig, &item.VoucherType, &item.ConnectionType, &item.AutoRegister, &item.OnlineConfig,
			&item.Tags, &item.Extras, &item.DescriptionI18n, &item.ImageURL, &item.Status, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, wrapDBErr(err, "list templates failed")
		}
		out = append(out, item)
	}
	return out, total, nil
}

func getDeviceTemplate(ctx context.Context, tenantID, id string) (*DeviceTemplate, error) {
	var item DeviceTemplate
	err := db.QueryRow(ctx, `
		SELECT id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
		       protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
		       tags, extras, description_i18n, image_url, status, created_at, updated_at
		FROM device_templates
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(&item.ID, &item.TenantID, &item.Name, &item.Brand, &item.ProductName, &item.ModelNumber, &item.ThingModelID, &item.ThingModelSnapshotID,
		&item.ProtocolType, &item.ProtocolConfig, &item.VoucherType, &item.ConnectionType, &item.AutoRegister, &item.OnlineConfig,
		&item.Tags, &item.Extras, &item.DescriptionI18n, &item.ImageURL, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, wrapDBErr(err, "device template not found")
	}
	return &item, nil
}

func updateDeviceTemplate(ctx context.Context, tenantID, id string, req *DeviceTemplate) (*DeviceTemplate, error) {
	// Validate required fields
	if err := ensureNonEmpty("name", req.Name); err != nil {
		return nil, err
	}
	if err := ensureNonEmpty("thing_model_id", req.ThingModelID); err != nil {
		return nil, err
	}
	if err := ensureNonEmpty("thing_model_snapshot_id", req.ThingModelSnapshotID); err != nil {
		return nil, err
	}

	// Check if template is ARCHIVED (immutable)
	current, err := getDeviceTemplate(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if current.Status == "ARCHIVED" {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: "ARCHIVED templates cannot be modified",
		}
	}

	// Verify thing_model exists and belongs to tenant
	tm, err := getThingModelByID(ctx, tenantID, req.ThingModelID)
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "thing model not found"}
	}

	// Verify snapshot exists and belongs to the thing model
	var snapshotTMID string
	if err := db.QueryRow(ctx, `
		SELECT thing_model_id FROM thing_model_snapshots
		WHERE id = $1 AND tenant_id = $2
	`, req.ThingModelSnapshotID, tenantID).Scan(&snapshotTMID); err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "thing model snapshot not found"}
	}
	if snapshotTMID != tm.ID {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "snapshot does not belong to the specified thing model"}
	}

	var out DeviceTemplate
	err = db.QueryRow(ctx, `
		UPDATE device_templates
		SET name = $3, brand = $4, product_name = $5, model_number = $6, thing_model_id = $7, thing_model_snapshot_id = $8,
		    protocol_type = $9, protocol_config = $10, voucher_type = $11, connection_type = COALESCE(NULLIF($12, ''), 'DIRECT'),
		    auto_register = $13, online_config = $14, tags = COALESCE($15, '[]'::jsonb), extras = COALESCE($16, '{}'::jsonb),
		    description_i18n = $17, image_url = $18, status = COALESCE(NULLIF($19, ''), status), updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
		          protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
		          tags, extras, description_i18n, image_url, status, created_at, updated_at
	`, tenantID, id, req.Name, req.Brand, req.ProductName, req.ModelNumber, req.ThingModelID, req.ThingModelSnapshotID,
		req.ProtocolType, jsonOrNull(req.ProtocolConfig), req.VoucherType, req.ConnectionType, req.AutoRegister, jsonOrNull(req.OnlineConfig),
		jsonOrNull(req.Tags), jsonOrNull(req.Extras), jsonOrNull(req.DescriptionI18n), req.ImageURL, req.Status).Scan(
		&out.ID, &out.TenantID, &out.Name, &out.Brand, &out.ProductName, &out.ModelNumber, &out.ThingModelID, &out.ThingModelSnapshotID,
		&out.ProtocolType, &out.ProtocolConfig, &out.VoucherType, &out.ConnectionType, &out.AutoRegister, &out.OnlineConfig,
		&out.Tags, &out.Extras, &out.DescriptionI18n, &out.ImageURL, &out.Status, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err, "device_templates_tenant_id_name_key") {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "device template name already exists for this tenant"}
		}
		return nil, wrapDBErr(err, "device template not found")
	}
	return &out, nil
}

func deleteDeviceTemplate(ctx context.Context, tenantID, id string) error {
	// Check if any Device references this template
	var deviceCount int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM devices
		WHERE tenant_id = $1 AND device_template_id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(&deviceCount); err != nil {
		return wrapDBErr(err, "device template not found")
	}
	if deviceCount > 0 {
		return &errs.Error{Code: errs.FailedPrecondition, Message: "device template is referenced by devices"}
	}

	// Check if any Product references this template
	var productCount int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM products
		WHERE tenant_id = $1 AND device_template_id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(&productCount); err != nil {
		return wrapDBErr(err, "device template not found")
	}
	if productCount > 0 {
		return &errs.Error{Code: errs.FailedPrecondition, Message: "device template is referenced by products"}
	}

	res, err := db.Exec(ctx, `UPDATE device_templates SET deleted_at = NOW(), updated_at = NOW() WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id)
	if err != nil {
		return wrapDBErr(err, "device template not found")
	}
	if n := res.RowsAffected(); n == 0 {
		return wrapDBErr(sqldb.ErrNoRows, "device template not found")
	}
	return nil
}

func setDeviceTemplateStatus(ctx context.Context, tenantID, id, status string) error {
	_, err := db.Exec(ctx, `UPDATE device_templates SET status = $3, updated_at = NOW() WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id, status)
	return wrapDBErr(err, "device template not found")
}

func deriveDeviceTemplateDraft(ctx context.Context, tenantID, id string) (*DeviceTemplate, error) {
	src, err := getDeviceTemplate(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	src.ID = ""
	src.Name = src.Name + " Draft"
	src.Status = "DRAFT"
	return createDeviceTemplate(ctx, tenantID, src)
}

func bindThingModelVersion(ctx context.Context, tenantID, id, snapshotID string) (*DeviceTemplate, error) {
	tpl, err := getDeviceTemplate(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	tpl.ThingModelSnapshotID = snapshotID
	return updateDeviceTemplate(ctx, tenantID, id, tpl)
}

func listDeviceTemplatesByThingModel(ctx context.Context, tenantID, thingModelID string) ([]DeviceTemplate, error) {
	rows, err := db.Query(ctx, `
		SELECT id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
		       protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
		       tags, extras, description_i18n, image_url, status, created_at, updated_at
		FROM device_templates
		WHERE tenant_id = $1 AND thing_model_id = $2 AND deleted_at IS NULL
		ORDER BY updated_at DESC
	`, tenantID, thingModelID)
	if err != nil {
		return nil, wrapDBErr(err, "list templates by thing model failed")
	}
	defer rows.Close()

	var templates []DeviceTemplate
	for rows.Next() {
		var item DeviceTemplate
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Brand, &item.ProductName, &item.ModelNumber, &item.ThingModelID, &item.ThingModelSnapshotID,
			&item.ProtocolType, &item.ProtocolConfig, &item.VoucherType, &item.ConnectionType, &item.AutoRegister, &item.OnlineConfig,
			&item.Tags, &item.Extras, &item.DescriptionI18n, &item.ImageURL, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, wrapDBErr(err, "list templates by thing model failed")
		}
		templates = append(templates, item)
	}
	return templates, nil
}

// publishDeviceTemplate validates and publishes a device template from DRAFT to PUBLISHED
func publishDeviceTemplate(ctx context.Context, tenantID, id string) (*DeviceTemplate, error) {
	// Fetch current template
	tpl, err := getDeviceTemplate(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	// Check current status is DRAFT
	if tpl.Status != "DRAFT" {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: fmt.Sprintf("cannot transition from %s to PUBLISHED", tpl.Status),
		}
	}

	// Validate protocol_type is provided
	if strings.TrimSpace(tpl.ProtocolType) == "" {
		return nil, &errs.Error{
			Code:    errs.InvalidArgument,
			Message: "protocol_type is required for publishing",
		}
	}

	// Validate protocol config
	validator, err := NewProtocolConfigValidator(tpl.ProtocolType, tpl.ProtocolConfig)
	if err != nil {
		return nil, &errs.Error{
			Code:    errs.InvalidArgument,
			Message: fmt.Sprintf("invalid protocol config: %v", err),
		}
	}
	if err := validator.Validate(); err != nil {
		return nil, &errs.Error{
			Code:    errs.InvalidArgument,
			Message: fmt.Sprintf("protocol config validation failed: %v", err),
		}
	}

	// Verify thing model snapshot still exists
	var snapshotTMID string
	if err := db.QueryRow(ctx, `
		SELECT thing_model_id FROM thing_model_snapshots
		WHERE id = $1 AND tenant_id = $2
	`, tpl.ThingModelSnapshotID, tenantID).Scan(&snapshotTMID); err != nil {
		return nil, &errs.Error{
			Code:    errs.NotFound,
			Message: "referenced thing model snapshot no longer exists",
		}
	}
	if snapshotTMID != tpl.ThingModelID {
		return nil, &errs.Error{
			Code:    errs.InvalidArgument,
			Message: "snapshot does not belong to the specified thing model",
		}
	}

	// Update status atomically with updated_at
	var out DeviceTemplate
	err = db.QueryRow(ctx, `
		UPDATE device_templates
		SET status = 'PUBLISHED', updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL AND status = 'DRAFT'
		RETURNING id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
		          protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
		          tags, extras, description_i18n, image_url, status, created_at, updated_at
	`, tenantID, id).Scan(
		&out.ID, &out.TenantID, &out.Name, &out.Brand, &out.ProductName, &out.ModelNumber, &out.ThingModelID, &out.ThingModelSnapshotID,
		&out.ProtocolType, &out.ProtocolConfig, &out.VoucherType, &out.ConnectionType, &out.AutoRegister, &out.OnlineConfig,
		&out.Tags, &out.Extras, &out.DescriptionI18n, &out.ImageURL, &out.Status, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &errs.Error{
				Code:    errs.FailedPrecondition,
				Message: "device template not found or is not in DRAFT status",
			}
		}
		return nil, wrapDBErr(err, "publish device template failed")
	}
	return &out, nil
}

// archiveDeviceTemplate archives a template (DRAFT or PUBLISHED → ARCHIVED)
func archiveDeviceTemplate(ctx context.Context, tenantID, id string) (*DeviceTemplate, error) {
	// Fetch current template
	tpl, err := getDeviceTemplate(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	// Check if already archived
	if tpl.Status == "ARCHIVED" {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: "ARCHIVED templates cannot be modified",
		}
	}

	// Validate transition
	if err := CanTransition(tpl.Status, "ARCHIVED"); err != nil {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: err.Error(),
		}
	}

	// Update status atomically
	var out DeviceTemplate
	err = db.QueryRow(ctx, `
		UPDATE device_templates
		SET status = 'ARCHIVED', updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL AND status != 'ARCHIVED'
		RETURNING id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
		          protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
		          tags, extras, description_i18n, image_url, status, created_at, updated_at
	`, tenantID, id).Scan(
		&out.ID, &out.TenantID, &out.Name, &out.Brand, &out.ProductName, &out.ModelNumber, &out.ThingModelID, &out.ThingModelSnapshotID,
		&out.ProtocolType, &out.ProtocolConfig, &out.VoucherType, &out.ConnectionType, &out.AutoRegister, &out.OnlineConfig,
		&out.Tags, &out.Extras, &out.DescriptionI18n, &out.ImageURL, &out.Status, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &errs.Error{
				Code:    errs.FailedPrecondition,
				Message: "device template not found or is already archived",
			}
		}
		return nil, wrapDBErr(err, "archive device template failed")
	}
	return &out, nil
}

// deriveDeviceTemplateDraftAdvanced creates a new DRAFT from a PUBLISHED template
func deriveDeviceTemplateDraftAdvanced(ctx context.Context, tenantID, id string) (*DeviceTemplate, error) {
	// Fetch source template
	src, err := getDeviceTemplate(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	// Validate source is PUBLISHED
	if src.Status != "PUBLISHED" {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: fmt.Sprintf("can only derive draft from PUBLISHED template, current status is %s", src.Status),
		}
	}

	// Create new template with new ID, same models, DRAFT status
	newTemplate := &DeviceTemplate{
		TenantID:             src.TenantID,
		Name:                 src.Name + " (draft)",
		Brand:                src.Brand,
		ProductName:          src.ProductName,
		ModelNumber:          src.ModelNumber,
		ThingModelID:         src.ThingModelID,
		ThingModelSnapshotID: src.ThingModelSnapshotID,
		ProtocolType:         src.ProtocolType,
		ProtocolConfig:       src.ProtocolConfig,
		VoucherType:          src.VoucherType,
		ConnectionType:       src.ConnectionType,
		AutoRegister:         src.AutoRegister,
		OnlineConfig:         src.OnlineConfig,
		Tags:                 src.Tags,
		Extras:               src.Extras,
		DescriptionI18n:      src.DescriptionI18n,
		ImageURL:             src.ImageURL,
		Status:               "DRAFT",
	}

	newID := uuid.New().String()
	now := time.Now()

	var out DeviceTemplate
	err = db.QueryRow(ctx, `
		INSERT INTO device_templates (
			id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
			protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
			tags, extras, description_i18n, image_url, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, COALESCE(NULLIF($12, ''), 'DIRECT'), $13, $14,
			COALESCE($15, '[]'::jsonb), COALESCE($16, '{}'::jsonb), $17, $18, 'DRAFT', $19, $19
		)
		RETURNING id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
		          protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
		          tags, extras, description_i18n, image_url, status, created_at, updated_at
	`, newID, newTemplate.TenantID, newTemplate.Name, newTemplate.Brand, newTemplate.ProductName, newTemplate.ModelNumber,
		newTemplate.ThingModelID, newTemplate.ThingModelSnapshotID, newTemplate.ProtocolType, jsonOrNull(newTemplate.ProtocolConfig),
		newTemplate.VoucherType, newTemplate.ConnectionType, newTemplate.AutoRegister, jsonOrNull(newTemplate.OnlineConfig),
		jsonOrNull(newTemplate.Tags), jsonOrNull(newTemplate.Extras), jsonOrNull(newTemplate.DescriptionI18n),
		newTemplate.ImageURL, now).Scan(
		&out.ID, &out.TenantID, &out.Name, &out.Brand, &out.ProductName, &out.ModelNumber, &out.ThingModelID, &out.ThingModelSnapshotID,
		&out.ProtocolType, &out.ProtocolConfig, &out.VoucherType, &out.ConnectionType, &out.AutoRegister, &out.OnlineConfig,
		&out.Tags, &out.Extras, &out.DescriptionI18n, &out.ImageURL, &out.Status, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, wrapDBErr(err, "derive device template draft failed")
	}
	return &out, nil
}

// bindThingModelVersionAdvanced updates template's snapshot with validation
func bindThingModelVersionAdvanced(ctx context.Context, tenantID, id, snapshotID string) (*DeviceTemplate, error) {
	// Fetch current template
	tpl, err := getDeviceTemplate(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	// Check if ARCHIVED (immutable)
	if tpl.Status == "ARCHIVED" {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: "ARCHIVED templates cannot be modified",
		}
	}

	// Verify new snapshot exists and belongs to same thing model
	var snapshotTMID string
	if err := db.QueryRow(ctx, `
		SELECT thing_model_id FROM thing_model_snapshots
		WHERE id = $1 AND tenant_id = $2
	`, snapshotID, tenantID).Scan(&snapshotTMID); err != nil {
		return nil, &errs.Error{
			Code:    errs.NotFound,
			Message: "thing model snapshot not found",
		}
	}

	if snapshotTMID != tpl.ThingModelID {
		return nil, &errs.Error{
			Code:    errs.InvalidArgument,
			Message: "snapshot does not belong to the specified thing model",
		}
	}

	// Update snapshot ID atomically
	var out DeviceTemplate
	err = db.QueryRow(ctx, `
		UPDATE device_templates
		SET thing_model_snapshot_id = $3, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING id, tenant_id, name, brand, product_name, model_number, thing_model_id, thing_model_snapshot_id,
		          protocol_type, protocol_config, voucher_type, connection_type, auto_register, online_config,
		          tags, extras, description_i18n, image_url, status, created_at, updated_at
	`, tenantID, id, snapshotID).Scan(
		&out.ID, &out.TenantID, &out.Name, &out.Brand, &out.ProductName, &out.ModelNumber, &out.ThingModelID, &out.ThingModelSnapshotID,
		&out.ProtocolType, &out.ProtocolConfig, &out.VoucherType, &out.ConnectionType, &out.AutoRegister, &out.OnlineConfig,
		&out.Tags, &out.Extras, &out.DescriptionI18n, &out.ImageURL, &out.Status, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, wrapDBErr(err, "bind thing model version failed")
	}
	return &out, nil
}

func generateProductKey() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimRight(base32.StdEncoding.EncodeToString(b), "=")), nil
}

func createProduct(ctx context.Context, tenantID string, req *Product) (*Product, error) {
	if err := ensureNonEmpty("name", req.Name); err != nil {
		return nil, err
	}
	if err := ensureNonEmpty("device_template_id", req.DeviceTemplateID); err != nil {
		return nil, err
	}

	tmpl, err := getDeviceTemplate(ctx, tenantID, req.DeviceTemplateID)
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "THING_MODEL_TEMPLATE_NOT_FOUND"}
	}

	if tmpl.Status == "ARCHIVED" {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "TEMPLATE_ARCHIVED"}
	}

	productKey := strings.TrimSpace(req.ProductKey)
	if productKey == "" {
		generated, err := generateProductKey()
		if err != nil {
			return nil, &errs.Error{Code: errs.Internal, Message: "failed to generate product key"}
		}
		productKey = generated
	}

	var out Product
	err = db.QueryRow(ctx, `
		INSERT INTO products (id, tenant_id, name, device_template_id, product_key, sku, image_url, ota_package_id, description_i18n, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		RETURNING id, tenant_id, name, device_template_id, product_key, sku, image_url, ota_package_id, description_i18n, created_at, deleted_at
	`, newID(), tenantID, req.Name, req.DeviceTemplateID, productKey, req.SKU, req.ImageURL, req.OTAPackageID, jsonOrNull(req.DescriptionI18n)).Scan(
		&out.ID, &out.TenantID, &out.Name, &out.DeviceTemplateID, &out.ProductKey, &out.SKU, &out.ImageURL, &out.OTAPackageID, &out.DescriptionI18n, &out.CreatedAt, &out.DeletedAt,
	)
	if err != nil {
		if isUniqueViolation(err, "products_tenant_id_product_key_key") {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "product key already exists for this tenant"}
		}
		return nil, wrapDBErr(err, "product create failed")
	}
	return &out, nil
}

func listProducts(ctx context.Context, tenantID string, req *ListProductsReq) ([]*Product, int, error) {
	req.pageRequest.normalize()

	query := `
		SELECT id, tenant_id, name, device_template_id, product_key, sku, image_url, ota_package_id, description_i18n, created_at, deleted_at, COUNT(*) OVER()
		FROM products
		WHERE tenant_id = $1 AND deleted_at IS NULL`

	args := []interface{}{tenantID}
	argIdx := 2

	if req.DeviceTemplateID != "" {
		query += fmt.Sprintf(` AND device_template_id = $%d`, argIdx)
		args = append(args, req.DeviceTemplateID)
		argIdx++
	}

	if req.Name != "" {
		query += fmt.Sprintf(` AND name ILIKE $%d`, argIdx)
		args = append(args, "%"+strings.TrimSpace(req.Name)+"%")
		argIdx++
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	args = append(args, req.PageSize, (req.Page-1)*req.PageSize)

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, wrapDBErr(err, "list products failed")
	}
	defer rows.Close()

	var out []*Product
	total := 0
	for rows.Next() {
		var item Product
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.DeviceTemplateID, &item.ProductKey, &item.SKU, &item.ImageURL, &item.OTAPackageID, &item.DescriptionI18n, &item.CreatedAt, &item.DeletedAt, &total); err != nil {
			return nil, 0, wrapDBErr(err, "list products failed")
		}
		out = append(out, &item)
	}
	return out, total, nil
}

func getProduct(ctx context.Context, tenantID, id string) (*Product, error) {
	var out Product
	err := db.QueryRow(ctx, `
		SELECT id, tenant_id, name, device_template_id, product_key, sku, image_url, ota_package_id, description_i18n, created_at, deleted_at
		FROM products
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(&out.ID, &out.TenantID, &out.Name, &out.DeviceTemplateID, &out.ProductKey, &out.SKU, &out.ImageURL, &out.OTAPackageID, &out.DescriptionI18n, &out.CreatedAt, &out.DeletedAt)
	if err != nil {
		return nil, wrapDBErr(err, "product not found")
	}
	return &out, nil
}

func getProductByProductKey(ctx context.Context, tenantID, productKey string) (*Product, error) {
	var out Product
	err := db.QueryRow(ctx, `
		SELECT id, tenant_id, name, device_template_id, product_key, sku, image_url, ota_package_id, description_i18n, created_at, deleted_at
		FROM products
		WHERE tenant_id = $1 AND product_key = $2 AND deleted_at IS NULL
	`, tenantID, productKey).Scan(&out.ID, &out.TenantID, &out.Name, &out.DeviceTemplateID, &out.ProductKey, &out.SKU, &out.ImageURL, &out.OTAPackageID, &out.DescriptionI18n, &out.CreatedAt, &out.DeletedAt)
	if err != nil {
		return nil, wrapDBErr(err, "product not found")
	}
	return &out, nil
}

func listProductsByTemplate(ctx context.Context, tenantID, templateID string) ([]*Product, error) {
	rows, err := db.Query(ctx, `
		SELECT id, tenant_id, name, device_template_id, product_key, sku, image_url, ota_package_id, description_i18n, created_at, deleted_at
		FROM products
		WHERE tenant_id = $1 AND device_template_id = $2 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, tenantID, templateID)
	if err != nil {
		return nil, wrapDBErr(err, "list products by template failed")
	}
	defer rows.Close()

	var out []*Product
	for rows.Next() {
		var item Product
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.DeviceTemplateID, &item.ProductKey, &item.SKU, &item.ImageURL, &item.OTAPackageID, &item.DescriptionI18n, &item.CreatedAt, &item.DeletedAt); err != nil {
			return nil, wrapDBErr(err, "list products by template failed")
		}
		out = append(out, &item)
	}
	return out, nil
}

func updateProduct(ctx context.Context, tenantID, id string, req *Product) (*Product, error) {
	if err := ensureNonEmpty("name", req.Name); err != nil {
		return nil, err
	}

	current, err := getProduct(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if req.DeviceTemplateID != "" && req.DeviceTemplateID != current.DeviceTemplateID {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "cannot change device_template_id"}
	}

	deviceTemplateID := req.DeviceTemplateID
	if deviceTemplateID == "" {
		deviceTemplateID = current.DeviceTemplateID
	}

	productKey := req.ProductKey
	if productKey == "" {
		productKey = current.ProductKey
	}

	var out Product
	err = db.QueryRow(ctx, `
		UPDATE products
		SET name = $3, sku = $4, image_url = $5, ota_package_id = $6, description_i18n = $7
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING id, tenant_id, name, device_template_id, product_key, sku, image_url, ota_package_id, description_i18n, created_at, deleted_at
	`, tenantID, id, req.Name, req.SKU, req.ImageURL, req.OTAPackageID, jsonOrNull(req.DescriptionI18n)).Scan(
		&out.ID, &out.TenantID, &out.Name, &out.DeviceTemplateID, &out.ProductKey, &out.SKU, &out.ImageURL, &out.OTAPackageID, &out.DescriptionI18n, &out.CreatedAt, &out.DeletedAt,
	)
	if err != nil {
		return nil, wrapDBErr(err, "product not found")
	}
	return &out, nil
}

func deleteProduct(ctx context.Context, tenantID, id string) error {
	_, err := getProduct(ctx, tenantID, id)
	if err != nil {
		return err
	}

	var count int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM devices
		WHERE tenant_id = $1 AND product_id = $2
	`, tenantID, id).Scan(&count)
	if err != nil {
		return wrapDBErr(err, "product delete failed")
	}

	if count > 0 {
		return &errs.Error{Code: errs.FailedPrecondition, Message: "PRODUCT_HAS_DEVICES"}
	}

	res, err := db.Exec(ctx, `UPDATE products SET deleted_at = NOW() WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id)
	if err != nil {
		return wrapDBErr(err, "product not found")
	}
	if n := res.RowsAffected(); n == 0 {
		return wrapDBErr(sqldb.ErrNoRows, "product not found")
	}
	return nil
}

func generateSN() string {
	// Generate 32-character alphanumeric string (nanoid-style)
	const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to UUID if random read fails
		return uuid.NewString()
	}
	for i := range b {
		b[i] = letters[b[i]%byte(len(letters))]
	}
	return string(b)
}

func sanitizeDevice(d *Device) *Device {
	// Create a copy without voucher
	copy := *d
	copy.Voucher = ""
	return &copy
}

func createDevice(ctx context.Context, tenantID string, req *Device) (*Device, error) {
	// Validation: device_template_id must exist and be PUBLISHED
	tmpl, err := getDeviceTemplate(ctx, tenantID, req.DeviceTemplateID)
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device template not found"}
	}
	if tmpl.Status != "PUBLISHED" {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device template must be PUBLISHED"}
	}

	// Validation: product_id if provided must match template
	if req.ProductID != nil && strings.TrimSpace(*req.ProductID) != "" {
		product, err := getProduct(ctx, tenantID, *req.ProductID)
		if err != nil {
			return nil, &errs.Error{Code: errs.NotFound, Message: "product not found"}
		}
		if product.DeviceTemplateID != req.DeviceTemplateID {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "product does not match device template"}
		}
	}

	// Validation: parent_device_id if provided must exist and be GATEWAY
	if req.ParentDeviceID != nil && strings.TrimSpace(*req.ParentDeviceID) != "" {
		parent, err := getDevice(ctx, tenantID, *req.ParentDeviceID)
		if err != nil {
			return nil, &errs.Error{Code: errs.NotFound, Message: "parent device not found"}
		}
		// Verify parent's template has GATEWAY connection type
		parentTmpl, err := getDeviceTemplate(ctx, tenantID, parent.DeviceTemplateID)
		if err != nil {
			return nil, &errs.Error{Code: errs.NotFound, Message: "parent device template not found"}
		}
		if parentTmpl.ConnectionType != "GATEWAY" {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "parent device must be a GATEWAY"}
		}
	}

	// Auto-generate SN if empty
	sn := strings.TrimSpace(req.SN)
	if sn == "" {
		sn = generateSN()
	}

	// Validation: SN unique per tenant
	existingDevice, _ := getDeviceBySN(ctx, tenantID, sn)
	if existingDevice != nil {
		return nil, &errs.Error{Code: errs.AlreadyExists, Message: "serial number already exists for this tenant"}
	}

	// Prepare voucher (generate if empty, encrypt before storage)
	voucher := strings.TrimSpace(req.Voucher)
	if voucher == "" {
		voucher = uuid.NewString()
	}
	// MVP: Store as-is (no encryption for now, can add encryption layer)
	encryptedVoucher := voucher

	var out Device
	id := newID()
	err = db.QueryRow(ctx, `
		INSERT INTO devices (id, tenant_id, device_template_id, product_id, sn, voucher, is_online, activate_flag, parent_device_id, created_at, activated_at, deleted_at)
		VALUES ($1, $2, $3, $4, $5, $6, 0, COALESCE(NULLIF($7, ''), 'INACTIVE'), $8, NOW(), NULL, NULL)
		RETURNING id, tenant_id, device_template_id, product_id, sn, voucher, is_online, activate_flag, parent_device_id, created_at, activated_at, deleted_at
	`, id, tenantID, req.DeviceTemplateID, req.ProductID, sn, encryptedVoucher, req.ActivateFlag, req.ParentDeviceID).Scan(
		&out.ID, &out.TenantID, &out.DeviceTemplateID, &out.ProductID, &out.SN, &out.Voucher, &out.IsOnline, &out.ActivateFlag, &out.ParentDeviceID, &out.CreatedAt, &out.ActivatedAt, &out.DeletedAt,
	)
	if err != nil {
		if isUniqueViolation(err, "devices_tenant_id_sn_key") {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "serial number already exists for this tenant"}
		}
		return nil, wrapDBErr(err, "device create failed")
	}

	// Return voucher only on creation (not in subsequent GETs)
	out.Voucher = voucher
	return &out, nil
}

func listDevices(ctx context.Context, tenantID string, req *ListDevicesReq) ([]Device, int, error) {
	req.pageRequest.normalize()
	snFilter := "%" + strings.TrimSpace(req.SN) + "%"

	query := `
		SELECT id, tenant_id, device_template_id, product_id, sn, voucher, is_online, activate_flag, parent_device_id, created_at, activated_at, deleted_at, COUNT(*) OVER()
		FROM devices
		WHERE tenant_id = $1 AND deleted_at IS NULL
		  AND ($2 = '%%' OR sn ILIKE $2)
		  AND ($3 = '' OR device_template_id::text = $3)
		  AND ($4 = '' OR product_id::text = $4)
		  AND ($5 = '' OR activate_flag = $5)
		ORDER BY created_at DESC LIMIT $6 OFFSET $7
	`

	rows, err := db.Query(ctx, query, tenantID, snFilter, req.DeviceTemplateID, req.ProductID, req.Status, req.PageSize, (req.Page-1)*req.PageSize)
	if err != nil {
		return nil, 0, wrapDBErr(err, "list devices failed")
	}
	defer rows.Close()

	var out []Device
	total := 0
	for rows.Next() {
		var item Device
		if err := rows.Scan(&item.ID, &item.TenantID, &item.DeviceTemplateID, &item.ProductID, &item.SN, &item.Voucher, &item.IsOnline, &item.ActivateFlag, &item.ParentDeviceID, &item.CreatedAt, &item.ActivatedAt, &item.DeletedAt, &total); err != nil {
			return nil, 0, wrapDBErr(err, "list devices failed")
		}
		// Sanitize voucher from list response
		item.Voucher = ""
		out = append(out, item)
	}
	return out, total, nil
}

func getDevice(ctx context.Context, tenantID, id string) (*Device, error) {
	var out Device
	err := db.QueryRow(ctx, `
		SELECT id, tenant_id, device_template_id, product_id, sn, voucher, is_online, activate_flag, parent_device_id, created_at, activated_at, deleted_at
		FROM devices
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(&out.ID, &out.TenantID, &out.DeviceTemplateID, &out.ProductID, &out.SN, &out.Voucher, &out.IsOnline, &out.ActivateFlag, &out.ParentDeviceID, &out.CreatedAt, &out.ActivatedAt, &out.DeletedAt)
	if err != nil {
		return nil, wrapDBErr(err, "device not found")
	}
	// Sanitize voucher from response
	out.Voucher = ""
	return &out, nil
}

func getDeviceBySN(ctx context.Context, tenantID, sn string) (*Device, error) {
	var out Device
	err := db.QueryRow(ctx, `
		SELECT id, tenant_id, device_template_id, product_id, sn, voucher, is_online, activate_flag, parent_device_id, created_at, activated_at, deleted_at
		FROM devices
		WHERE tenant_id = $1 AND sn = $2 AND deleted_at IS NULL
	`, tenantID, sn).Scan(&out.ID, &out.TenantID, &out.DeviceTemplateID, &out.ProductID, &out.SN, &out.Voucher, &out.IsOnline, &out.ActivateFlag, &out.ParentDeviceID, &out.CreatedAt, &out.ActivatedAt, &out.DeletedAt)
	if err != nil {
		return nil, wrapDBErr(err, "device not found")
	}
	return &out, nil
}

func updateDevice(ctx context.Context, tenantID, id string, req *Device) (*Device, error) {
	// Only allow update of: activate_flag, parent_device_id
	// Validate parent_device_id if provided
	if req.ParentDeviceID != nil && strings.TrimSpace(*req.ParentDeviceID) != "" {
		parent, err := getDevice(ctx, tenantID, *req.ParentDeviceID)
		if err != nil {
			return nil, &errs.Error{Code: errs.NotFound, Message: "parent device not found"}
		}
		parentTmpl, err := getDeviceTemplate(ctx, tenantID, parent.DeviceTemplateID)
		if err != nil {
			return nil, &errs.Error{Code: errs.NotFound, Message: "parent device template not found"}
		}
		if parentTmpl.ConnectionType != "GATEWAY" {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "parent device must be a GATEWAY"}
		}
	}

	var out Device
	err := db.QueryRow(ctx, `
		UPDATE devices
		SET activate_flag = COALESCE(NULLIF($3, ''), activate_flag), parent_device_id = $4
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING id, tenant_id, device_template_id, product_id, sn, voucher, is_online, activate_flag, parent_device_id, created_at, activated_at, deleted_at
	`, tenantID, id, req.ActivateFlag, req.ParentDeviceID).Scan(
		&out.ID, &out.TenantID, &out.DeviceTemplateID, &out.ProductID, &out.SN, &out.Voucher, &out.IsOnline, &out.ActivateFlag, &out.ParentDeviceID, &out.CreatedAt, &out.ActivatedAt, &out.DeletedAt,
	)
	if err != nil {
		return nil, wrapDBErr(err, "device not found")
	}
	// Sanitize voucher from response
	out.Voucher = ""
	return &out, nil
}

func deleteDevice(ctx context.Context, tenantID, id string) error {
	// Soft delete: set deleted_at = NOW()
	res, err := db.Exec(ctx, `UPDATE devices SET deleted_at = NOW() WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id)
	if err != nil {
		return wrapDBErr(err, "device not found")
	}
	if n := res.RowsAffected(); n == 0 {
		return wrapDBErr(sqldb.ErrNoRows, "device not found")
	}
	return nil
}

func updateDeviceOnline(ctx context.Context, tenantID, id string, online bool, ts int64) error {
	var activatedAt any
	if online && ts > 0 {
		activatedAt = time.UnixMilli(ts)
	}
	_, err := db.Exec(ctx, `
		UPDATE devices
		SET is_online = $3, activated_at = COALESCE($4, activated_at)
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id, boolToInt(online), activatedAt)
	return wrapDBErr(err, "device not found")
}

// idempotency cache: key format is "tenantID:deviceID:idempotencyKey"
var idempotencyCache = struct {
	sync.RWMutex
	entries map[string]*idempotencyCacheEntry
	timers  map[string]*time.Timer
}{
	entries: make(map[string]*idempotencyCacheEntry),
	timers:  make(map[string]*time.Timer),
}

type idempotencyCacheEntry struct {
	result *ActuateResp
	expiry time.Time
}

func cacheActuateResult(tenantID, deviceID, idempotencyKey string, result *ActuateResp) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return
	}
	key := fmt.Sprintf("%s:%s:%s", tenantID, deviceID, idempotencyKey)
	ttl := 5 * time.Minute
	expiry := time.Now().Add(ttl)

	idempotencyCache.Lock()
	defer idempotencyCache.Unlock()

	// Cancel existing timer
	if timer, exists := idempotencyCache.timers[key]; exists {
		timer.Stop()
	}

	idempotencyCache.entries[key] = &idempotencyCacheEntry{result: result, expiry: expiry}
	idempotencyCache.timers[key] = time.AfterFunc(ttl, func() {
		idempotencyCache.Lock()
		delete(idempotencyCache.entries, key)
		delete(idempotencyCache.timers, key)
		idempotencyCache.Unlock()
	})
}

func getCachedActuateResult(tenantID, deviceID, idempotencyKey string) *ActuateResp {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil
	}
	key := fmt.Sprintf("%s:%s:%s", tenantID, deviceID, idempotencyKey)

	idempotencyCache.RLock()
	defer idempotencyCache.RUnlock()

	entry, exists := idempotencyCache.entries[key]
	if !exists {
		return nil
	}
	if time.Now().After(entry.expiry) {
		return nil
	}
	return entry.result
}

// parseAccessFromJSON parses the access control field
func parseAccessFromJSON(raw json.RawMessage) (*Access, error) {
	var access Access
	if err := json.Unmarshal(raw, &access); err != nil {
		return nil, fmt.Errorf("invalid access field: %w", err)
	}
	return &access, nil
}

// parseValueTypeFromJSON parses the value type field
func parseValueTypeFromJSON(raw json.RawMessage) (*ValueType, error) {
	var vt ValueType
	if err := json.Unmarshal(raw, &vt); err != nil {
		return nil, fmt.Errorf("invalid value type field: %w", err)
	}
	return &vt, nil
}

// findSnapshotItem finds an item by identifier in the snapshot content
func findSnapshotItem(snapshot *snapshotContent, identifier string) *snapshotContentItem {
	if snapshot == nil {
		return nil
	}
	for i, item := range snapshot.Items {
		if item.Identifier == identifier {
			return &snapshot.Items[i]
		}
	}
	return nil
}

func actuate(ctx context.Context, tenantID, deviceID string, req *ActuateReq) (*ActuateResp, error) {
	// Check idempotency cache first
	if req.IdempotencyKey != "" {
		if cached := getCachedActuateResult(tenantID, deviceID, req.IdempotencyKey); cached != nil {
			return cached, nil
		}
	}

	// Get device
	device, err := getDevice(ctx, tenantID, deviceID)
	if err != nil {
		return nil, err
	}

	// Get device template
	tmpl, err := getDeviceTemplate(ctx, tenantID, device.DeviceTemplateID)
	if err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device template not found"}
	}

	// Get template's snapshot
	if tmpl.ThingModelSnapshotID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "device template has no snapshot"}
	}

	snapshot, err := getVersion(ctx, tenantID, tmpl.ThingModelID, 0) // Load latest snapshot
	if err != nil {
		// Try to load the specific snapshot by ID
		snap, err := getThingModelSnapshotByID(ctx, tmpl.ThingModelSnapshotID)
		if err != nil {
			return nil, &errs.Error{Code: errs.NotFound, Message: "snapshot not found"}
		}
		snapshot = snap
	}

	// Parse snapshot content
	snapshotContent, err := parseSnapshotContent(snapshot.Content)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid snapshot content"}
	}

	// Find item by identifier
	item := findSnapshotItem(snapshotContent, strings.TrimSpace(req.Identifier))
	if item == nil {
		return &ActuateResp{Valid: false, Errors: []ValidationError{{
			Field:   "identifier",
			Code:    "IDENTIFIER_NOT_FOUND",
			Message: "identifier not found in device template",
		}}}, nil
	}

	// Check write access
	access, err := parseAccessFromJSON(item.Access)
	if err != nil {
		return &ActuateResp{Valid: false, Errors: []ValidationError{{
			Field:   "access",
			Code:    "ACCESS_INVALID",
			Message: "invalid access control",
		}}}, nil
	}

	if !access.Write {
		return &ActuateResp{Valid: false, Errors: []ValidationError{{
			Field:   "access",
			Code:    "BAD_ACCESS",
			Message: "item is not writable",
		}}}, nil
	}

	// Validate value/params against value type
	_, err = parseValueTypeFromJSON(item.ValueType)
	if err != nil {
		return &ActuateResp{Valid: false, Errors: []ValidationError{{
			Field:   "value_type",
			Code:    "VALUE_TYPE_INVALID",
			Message: "invalid value type",
		}}}, nil
	}

	// Determine which payload to validate based on item type
	var validationErrors []ValidationError
	if item.Type == "PROPERTY" {
		if len(req.Value) > 0 {
			vt, err := parseValueType(item.ValueType)
			if err == nil {
				if err := vt.ValidateValue(req.Value); err != nil {
					validationErrors = append(validationErrors, ValidationError{
						Field:   "value",
						Code:    "VALUE_INVALID",
						Message: err.Error(),
					})
				}
			}
		}
	} else if item.Type == "COMMAND" {
		if len(req.Params) > 0 {
			// For commands, validate params structure
			// MVP: Basic JSON validation
			var params map[string]any
			if err := json.Unmarshal(req.Params, &params); err != nil {
				validationErrors = append(validationErrors, ValidationError{
					Field:   "params",
					Code:    "PARAMS_INVALID",
					Message: "params must be valid JSON",
				})
			}
		}
	}

	resp := &ActuateResp{
		Valid:  len(validationErrors) == 0,
		Errors: validationErrors,
	}

	// Cache the result
	cacheActuateResult(tenantID, deviceID, req.IdempotencyKey, resp)

	return resp, nil
}

func getThingModelSnapshotByID(ctx context.Context, snapshotID string) (*ThingModelSnapshot, error) {
	var snap ThingModelSnapshot
	err := db.QueryRow(ctx, `
		SELECT id, thing_model_id, tenant_id, version, content, changelog, published_at, published_by
		FROM thing_model_snapshots
		WHERE id = $1
	`, snapshotID).Scan(&snap.ID, &snap.ThingModelID, &snap.TenantID, &snap.Version, &snap.Content, &snap.Changelog, &snap.PublishedAt, &snap.PublishedBy)
	if err != nil {
		return nil, wrapDBErr(err, "snapshot not found")
	}
	return &snap, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func listMetaItemKeys(ctx context.Context, tenantID string) ([]MetaItemKey, error) {
	rows, err := db.Query(ctx, `
		SELECT id, tenant_id, key, label_i18n, scope, value_schema, is_builtin, description_i18n
		FROM meta_item_keys
		WHERE tenant_id IS NULL OR tenant_id = $1
		ORDER BY is_builtin DESC, key
	`, tenantID)
	if err != nil {
		return nil, wrapDBErr(err, "meta item keys not found")
	}
	defer rows.Close()
	var out []MetaItemKey
	for rows.Next() {
		var item MetaItemKey
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Key, &item.LabelI18n, &item.Scope, &item.ValueSchema, &item.IsBuiltin, &item.DescriptionI18n); err != nil {
			return nil, wrapDBErr(err, "meta item keys not found")
		}
		out = append(out, item)
	}
	return out, nil
}

func getMetaItemKeyByKey(ctx context.Context, tenantID, key string) (*MetaItemKey, error) {
	var item MetaItemKey
	err := db.QueryRow(ctx, `
		SELECT id, tenant_id, key, label_i18n, scope, value_schema, is_builtin, description_i18n
		FROM meta_item_keys
		WHERE key = $2 AND (tenant_id IS NULL OR tenant_id = $1)
		ORDER BY tenant_id NULLS LAST
		LIMIT 1
	`, tenantID, key).Scan(&item.ID, &item.TenantID, &item.Key, &item.LabelI18n, &item.Scope, &item.ValueSchema, &item.IsBuiltin, &item.DescriptionI18n)
	if err != nil {
		return nil, wrapDBErr(err, "meta item key not found")
	}
	return &item, nil
}
