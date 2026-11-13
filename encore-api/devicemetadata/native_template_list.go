package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// nativeTemplateListQuery contains validated OAS list filters. Zero page values
// select the documented defaults.
type nativeTemplateListQuery struct {
	Page, PageSize                                                               int
	Search, DeviceKind, Protocol, Brand, Tag, ModelID, AdministrativeState, Sort string
	ProtocolSet, BrandSet, TagSet                                                bool
}

// listNativeDeviceAccessTemplatesTx performs count, stable page selection, and
// page-wide device aggregation in the caller's transaction.
func listNativeDeviceAccessTemplatesTx(
	ctx context.Context,
	tx *sqldb.Tx,
	tenantID string,
	authorizedDeviceIDs []string,
	query nativeTemplateListQuery,
) ([]NativeDeviceAccessTemplate, int64, error) {
	if err := validateNativeTemplateReadContext(tx, tenantID, authorizedDeviceIDs); err != nil {
		return nil, 0, err
	}
	query, err := normalizeNativeTemplateListQuery(query)
	if err != nil {
		return nil, 0, err
	}
	where, filterArgs := nativeTemplateListWhere(query)
	args := append([]any{tenantID}, filterArgs...)
	var total int64
	if err := tx.QueryRow(ctx, `SELECT count(*)::bigint FROM device_access_templates t WHERE t.tenant_id=$1 AND t.deleted_at IS NULL`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total < 0 || total > 9007199254740991 {
		return nil, 0, &errs.Error{Code: errs.Unavailable, Message: "template count is outside the safe range"}
	}

	orderBy := nativeTemplateListOrder(query.Sort)
	pageArgs := append([]any(nil), args...)
	limitPos := len(pageArgs) + 1
	offsetPos := limitPos + 1
	pageArgs = append(pageArgs, query.PageSize, (query.Page-1)*query.PageSize)
	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT t.id::text,t.tenant_id::text,t.name,t.description,t.brand,t.product_name,t.model_number,t.image_asset_id,
		t.tags,t.configuration,t.administrative_state,t.resource_version,t.created_by,t.updated_by,t.created_at,t.updated_at
		FROM device_access_templates t WHERE t.tenant_id=$1 AND t.deleted_at IS NULL%s
		ORDER BY %s LIMIT $%d OFFSET $%d`, where, orderBy, limitPos, offsetPos), pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	page := make([]accessTemplateConfigurationRecord, 0, query.PageSize)
	for rows.Next() {
		var row accessTemplateConfigurationRecord
		var tags []byte
		var createdBy, updatedBy *string
		if err := rows.Scan(&row.ID, &row.TenantID, &row.Name, &row.Description, &row.Brand, &row.ProductName, &row.ModelNumber, &row.ImageAssetID,
			&tags, &row.Configuration, &row.AdministrativeState, &row.ResourceVersion, &createdBy, &updatedBy, &row.CreatedAt, &row.UpdatedAt); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if createdBy == nil || updatedBy == nil || *createdBy == "" || *updatedBy == "" || len(row.Configuration) == 0 || json.Unmarshal(tags, &row.Tags) != nil {
			rows.Close()
			return nil, 0, &errs.Error{Code: errs.Unavailable, Message: "template is not ready for the target read projection"}
		}
		row.CreatedBy, row.UpdatedBy = *createdBy, *updatedBy
		if validateAccessTemplateConfigurationRecord(&row) != nil {
			rows.Close()
			return nil, 0, &errs.Error{Code: errs.Unavailable, Message: "template is not ready for the target read projection"}
		}
		if err := validateNativeTemplateReadRecord(&row); err != nil {
			rows.Close()
			return nil, 0, err
		}
		page = append(page, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	if len(page) == 0 {
		return []NativeDeviceAccessTemplate{}, total, nil
	}

	ids := make([]string, len(page))
	for i := range page {
		ids[i] = page[i].ID
	}
	aggregateSQL := `WITH page_ids AS (SELECT unnest(string_to_array($2, ',')::uuid[]) AS id)
		SELECT p.id::text,count(DISTINCT d.id)::bigint
		FROM page_ids p
		LEFT JOIN device_access_bindings b ON b.tenant_id=$1 AND b.template_id=p.id
		LEFT JOIN devices d ON d.tenant_id=b.tenant_id AND d.id=b.device_id AND d.deleted_at IS NULL`
	aggregateArgs := []any{tenantID, strings.Join(ids, ",")}
	if len(authorizedDeviceIDs) > 0 {
		aggregateSQL += ` AND d.id=ANY(string_to_array($3, ',')::uuid[])`
		aggregateArgs = append(aggregateArgs, strings.Join(authorizedDeviceIDs, ","))
	}
	aggregateSQL += ` GROUP BY p.id`
	aggregates, err := tx.Query(ctx, aggregateSQL, aggregateArgs...)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]int64, len(page))
	for aggregates.Next() {
		var id string
		var count int64
		if err := aggregates.Scan(&id, &count); err != nil {
			aggregates.Close()
			return nil, 0, err
		}
		byID[id] = count
	}
	if err := aggregates.Err(); err != nil {
		aggregates.Close()
		return nil, 0, err
	}
	aggregates.Close()
	items := make([]NativeDeviceAccessTemplate, 0, len(page))
	for i := range page {
		count, ok := byID[page[i].ID]
		if !ok {
			return nil, 0, &errs.Error{Code: errs.Unavailable, Message: "template list aggregation is incomplete"}
		}
		projected, err := assembleNativeDeviceAccessTemplate(&page[i], count)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *projected)
	}
	return items, total, nil
}

func normalizeNativeTemplateListQuery(query nativeTemplateListQuery) (nativeTemplateListQuery, error) {
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 20
	}
	if query.Page < 1 || query.Page > 1000000 || query.PageSize < 1 || query.PageSize > 100 {
		return query, &errs.Error{Code: errs.InvalidArgument, Message: "template page or pageSize is outside the supported range"}
	}
	if query.Sort == "" {
		query.Sort = "-createdAt"
	}
	if query.Sort != "createdAt" && query.Sort != "-createdAt" && query.Sort != "name" && query.Sort != "-name" {
		return query, &errs.Error{Code: errs.InvalidArgument, Message: "invalid template sort"}
	}
	for _, item := range []struct {
		value string
		max   int
	}{{query.Search, 128}, {query.Protocol, 64}, {query.Brand, 64}, {query.Tag, 64}} {
		if !validAccessTemplateText(item.value, 0, item.max, false) {
			return query, &errs.Error{Code: errs.InvalidArgument, Message: "invalid template filter"}
		}
	}
	if query.DeviceKind != "" && query.DeviceKind != "standalone" && query.DeviceKind != "gateway" && query.DeviceKind != "subdevice" {
		return query, &errs.Error{Code: errs.InvalidArgument, Message: "invalid template deviceKind filter"}
	}
	if query.AdministrativeState != "" && query.AdministrativeState != "enabled" && query.AdministrativeState != "disabled" {
		return query, &errs.Error{Code: errs.InvalidArgument, Message: "invalid template administrativeState filter"}
	}
	if query.ModelID != "" {
		id, err := uuid.Parse(query.ModelID)
		if err != nil || id == uuid.Nil || id.String() != query.ModelID {
			return query, &errs.Error{Code: errs.InvalidArgument, Message: "invalid template modelId filter"}
		}
	}
	return query, nil
}

func nativeTemplateListWhere(query nativeTemplateListQuery) (string, []any) {
	var clauses []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)+1)) // $1 is tenantID.
	}
	if query.Search != "" {
		add(`position(lower($%d) in lower(t.name))>0`, query.Search)
	}
	if query.DeviceKind != "" {
		add(`t.configuration->>'deviceKind'=$%d`, query.DeviceKind)
	}
	if query.Protocol != "" || query.ProtocolSet {
		add(`t.configuration->'access'->>'protocol'=$%d`, query.Protocol)
	}
	if query.Brand != "" || query.BrandSet {
		add(`t.brand=$%d`, query.Brand)
	}
	if query.Tag != "" || query.TagSet {
		add(`t.tags ? $%d`, query.Tag)
	}
	if query.ModelID != "" {
		add(`t.configuration->'model'->>'modelId'=$%d`, query.ModelID)
	}
	if query.AdministrativeState != "" {
		add(`t.administrative_state=$%d`, query.AdministrativeState)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

func nativeTemplateListOrder(sort string) string {
	switch sort {
	case "createdAt":
		return "t.created_at ASC,t.id ASC"
	case "name":
		return "t.name ASC,t.id ASC"
	case "-name":
		return "t.name DESC,t.id DESC"
	case "-createdAt":
		return "t.created_at DESC,t.id DESC"
	default:
		return "t.created_at DESC,t.id DESC"
	}
}
