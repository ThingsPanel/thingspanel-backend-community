package notification

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"encore.app/l3-core/notification/contract"
)

func (s *Store) GetNotification(ctx context.Context, tenantID, notificationID string) (contract.NotificationView, error) {
	db, err := s.database()
	if err != nil {
		return contract.NotificationView{}, err
	}
	var view contract.NotificationView
	var created time.Time
	err = db.QueryRowContext(ctx, `SELECT id,source_type,source_id,intake_status,COALESCE(blocked_reason,''),created_at FROM notification_requests WHERE tenant_id=$1 AND id=$2`, tenantID, notificationID).Scan(&view.ID, &view.Source.Type, &view.Source.ID, &view.IntakeStatus, &view.BlockedReason, &created)
	if err != nil {
		return contract.NotificationView{}, classifyStoreError(err)
	}
	view.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	rows, err := db.QueryContext(ctx, `SELECT id,dispatch_status,delivery_status,attempt_count,COALESCE(provider_message_id,'') FROM notification_deliveries WHERE tenant_id=$1 AND request_id=$2 ORDER BY ordinal`, tenantID, notificationID)
	if err != nil {
		return contract.NotificationView{}, classifyStoreError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var d contract.DeliverySummary
		if err := rows.Scan(&d.DeliveryID, &d.DispatchStatus, &d.DeliveryStatus, &d.AttemptCount, &d.ProviderMessageID); err != nil {
			return contract.NotificationView{}, classifyStoreError(err)
		}
		view.Deliveries = append(view.Deliveries, d)
	}
	if err := rows.Err(); err != nil {
		return contract.NotificationView{}, classifyStoreError(err)
	}
	if view.Deliveries == nil {
		view.Deliveries = []contract.DeliverySummary{}
	}
	return view, nil
}

func (s *Store) GetDelivery(ctx context.Context, tenantID, deliveryID string) (contract.DeliveryView, error) {
	db, err := s.database()
	if err != nil {
		return contract.DeliveryView{}, err
	}
	var v contract.DeliveryView
	var sourceType, sourceID string
	var created time.Time
	var provider string
	var errorCode sql.NullString
	err = db.QueryRowContext(ctx, `SELECT d.id,d.dispatch_status,d.delivery_status,d.attempt_count,COALESCE(d.provider_message_id,''),r.id,d.instance_id,d.config_version,d.recipient_kind,d.recipient_display,r.source_type,r.source_id,d.created_at,d.error_code FROM notification_deliveries d JOIN notification_requests r ON r.tenant_id=d.tenant_id AND r.id=d.request_id WHERE d.tenant_id=$1 AND d.id=$2`, tenantID, deliveryID).Scan(&v.DeliveryID, &v.DispatchStatus, &v.DeliveryStatus, &v.AttemptCount, &provider, &v.NotificationID, &v.InstanceID, &v.ConfigVersion, &v.Recipient.Kind, &v.Recipient.Display, &sourceType, &sourceID, &created, &errorCode)
	if err != nil {
		return contract.DeliveryView{}, classifyStoreError(err)
	}
	v.ProviderMessageID = provider
	v.Source = contract.SourceRef{Type: sourceType, ID: sourceID}
	v.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	if errorCode.Valid {
		v.Error = &contract.RedactedError{Code: errorCode.String, Message: "delivery failed"}
	}
	return v, nil
}

func (s *Store) ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) (DeliveryPage, error) {
	if tenantID == "" || filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 200 {
		return DeliveryPage{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return DeliveryPage{}, err
	}
	where := []string{"d.tenant_id=$1"}
	args := []any{tenantID}
	add := func(expr string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(expr, "?", "$"+strconv.Itoa(len(args))))
	}
	if filter.InstanceID != "" {
		add("d.instance_id=?", filter.InstanceID)
	}
	if filter.SourceType != "" {
		add("r.source_type=?", filter.SourceType)
	}
	if filter.SourceID != "" {
		add("r.source_id=?", filter.SourceID)
	}
	if filter.DispatchStatus != "" {
		add("d.dispatch_status=?", string(filter.DispatchStatus))
	}
	if filter.DeliveryStatus != "" {
		add("d.delivery_status=?", string(filter.DeliveryStatus))
	}
	if filter.From != nil {
		add("d.created_at>=?", *filter.From)
	}
	if filter.To != nil {
		add("d.created_at<?", *filter.To)
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM notification_deliveries d JOIN notification_requests r ON r.tenant_id=d.tenant_id AND r.id=d.request_id WHERE "+clause, args...).Scan(&total); err != nil {
		return DeliveryPage{}, classifyStoreError(err)
	}
	pageArgs := append(append([]any(nil), args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	query := "SELECT d.id,d.dispatch_status,d.delivery_status,d.attempt_count,COALESCE(d.provider_message_id,''),r.id,d.instance_id,d.config_version,d.recipient_kind,d.recipient_display,r.source_type,r.source_id,d.created_at,d.error_code FROM notification_deliveries d JOIN notification_requests r ON r.tenant_id=d.tenant_id AND r.id=d.request_id WHERE " + clause + " ORDER BY d.created_at DESC,d.id LIMIT $" + itoa(limitPos) + " OFFSET $" + itoa(offsetPos)
	rows, err := db.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return DeliveryPage{}, classifyStoreError(err)
	}
	defer rows.Close()
	items := []contract.DeliveryView{}
	for rows.Next() {
		var v contract.DeliveryView
		var sourceType, sourceID string
		var created time.Time
		var provider string
		var errorCode sql.NullString
		if err := rows.Scan(&v.DeliveryID, &v.DispatchStatus, &v.DeliveryStatus, &v.AttemptCount, &provider, &v.NotificationID, &v.InstanceID, &v.ConfigVersion, &v.Recipient.Kind, &v.Recipient.Display, &sourceType, &sourceID, &created, &errorCode); err != nil {
			return DeliveryPage{}, classifyStoreError(err)
		}
		v.ProviderMessageID = provider
		v.Source = contract.SourceRef{Type: sourceType, ID: sourceID}
		v.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		if errorCode.Valid {
			v.Error = &contract.RedactedError{Code: errorCode.String, Message: "delivery failed"}
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return DeliveryPage{}, classifyStoreError(err)
	}
	return DeliveryPage{Items: items, Page: filter.Page, PageSize: filter.PageSize, Total: total}, nil
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}
