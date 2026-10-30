package devicemetadata

import (
	"context"
	"database/sql"
	"net/http"

	"encore.dev"
	"encore.dev/beta/errs"
)

func readGroupsForNativeDevice(ctx context.Context, identity nativeDeviceReadIdentity, deviceID string, page, pageSize int, requestID string) (*DeviceGroupPageResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "list device groups failed")
	}
	defer tx.Rollback()
	if _, err := getNativeDeviceRecordTx(ctx, tx, identity.tenantID, deviceID); err != nil {
		return nil, err
	}
	where := ` FROM device_groups g JOIN device_group_members m ON m.tenant_id=g.tenant_id AND m.group_id=g.id
		WHERE g.tenant_id=$1 AND m.device_id=$2 AND g.deleted_at IS NULL`
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT count(*)"+where, identity.tenantID, deviceID).Scan(&total); err != nil {
		return nil, wrapDBErr(err, "count device groups failed")
	}
	rows, err := tx.QueryContext(ctx, `SELECT g.id,g.tenant_id,g.parent_id,g.name,g.description,g.sort_order,g.resource_version,g.created_by,g.updated_by,g.created_at,g.updated_at`+where+` ORDER BY g.created_at DESC,g.id DESC LIMIT $3 OFFSET $4`, identity.tenantID, deviceID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, wrapDBErr(err, "list device groups failed")
	}
	defer rows.Close()
	items := make([]DeviceGroup, 0)
	for rows.Next() {
		var group DeviceGroup
		if err := rows.Scan(&group.ID, &group.TenantID, &group.ParentID, &group.Name, &group.Description, &group.SortOrder, &group.ResourceVersion, &group.CreatedBy, &group.UpdatedBy, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, wrapDBErr(err, "list device groups failed")
		}
		items = append(items, group)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDBErr(err, "list device groups failed")
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := hydrateGroupReadMetadataScoped(ctx, identity.tenantID, items, tx, identity.deviceIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "list device groups failed")
	}
	return &DeviceGroupPageResponse{Data: DeviceGroupPage{Items: items, Page: page, PageSize: pageSize, Total: total}, Meta: RequestMeta{RequestID: requestID}}, nil
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/groups
func (s *Service) ListGroupsForNativeDevice(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	if _, err := groupPrincipalForRead(); err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	values, err := parseNativeReadValues(r, []string{"page", "pageSize"})
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	query, err := decodeNativeDeviceListQuery(values)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	result, err := readGroupsForNativeDevice(r.Context(), identity, deviceID, query.page, query.pageSize, requestID)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, "", result)
}
