package notification

import (
	"context"
	"strconv"
	"strings"
	"time"

	"encore.app/l3-core/notification/contract"
)

type NotificationListFilter struct {
	Page         int
	PageSize     int
	SourceType   string
	SourceID     string
	IntakeStatus contract.IntakeStatus
	From         *time.Time
	To           *time.Time
}

// ListNotifications starts from requests, so a blocked request with no target
// remains discoverable. Every count, page and detail query carries the tenant.
func (s *Store) ListNotifications(ctx context.Context, tenant string, filter NotificationListFilter) (contract.NotificationListData, error) {
	result := contract.NotificationListData{Items: []contract.NotificationView{}, Page: filter.Page, PageSize: filter.PageSize}
	if tenant == "" || filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return result, storeErr(StoreErrorInvalid)
	}
	if filter.IntakeStatus != "" && filter.IntakeStatus != "ready" && filter.IntakeStatus != "blocked" {
		return result, storeErr(StoreErrorInvalid)
	}
	switch filter.SourceType {
	case "", "alarm", "automation", "manual", "test":
	default:
		return result, storeErr(StoreErrorInvalid)
	}
	if filter.From != nil && filter.To != nil && !filter.To.After(*filter.From) {
		return result, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return result, err
	}
	where := []string{"tenant_id=$1"}
	args := []any{tenant}
	add := func(field string, value any) {
		args = append(args, value)
		where = append(where, field+"$"+strconv.Itoa(len(args)))
	}
	if filter.SourceType != "" {
		add("source_type=", filter.SourceType)
	}
	if filter.SourceID != "" {
		add("source_id=", filter.SourceID)
	}
	if filter.IntakeStatus != "" {
		add("intake_status=", string(filter.IntakeStatus))
	}
	if filter.From != nil {
		add("created_at>=", *filter.From)
	}
	if filter.To != nil {
		add("created_at<", *filter.To)
	}
	clause := strings.Join(where, " AND ")
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM notification_requests WHERE "+clause, args...).Scan(&result.Total); err != nil {
		return result, classifyStoreError(err)
	}
	pageArgs := append(append([]any(nil), args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := db.QueryContext(ctx, "SELECT id FROM notification_requests WHERE "+clause+" ORDER BY created_at DESC,id LIMIT $"+strconv.Itoa(len(args)+1)+" OFFSET $"+strconv.Itoa(len(args)+2), pageArgs...)
	if err != nil {
		return result, classifyStoreError(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return result, classifyStoreError(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, classifyStoreError(err)
	}
	rows.Close()
	for _, id := range ids {
		view, err := s.GetNotification(ctx, tenant, id)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, view)
	}
	return result, nil
}
