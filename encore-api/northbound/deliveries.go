package northbound

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"encore.app/internal/authz"
)

// ListDeliveries returns the delivery ledger for this tenant. It is the only
// place a user can see what actually happened to a record.
//
//encore:api auth method=GET path=/v1/deliveries
func ListDeliveries(ctx context.Context, req *DeliveryListRequest) (*DeliveryListResponse, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionView); err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []any{data.TenantID}
	where := []string{"d.tenant_id = $1"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.Replace(clause, "?", "$"+itoa(len(args)), 1))
	}
	if req.PipelineID != "" {
		add("d.pipeline_id = ?::uuid", req.PipelineID)
	}
	if req.State != "" {
		add("d.state = ?", req.State)
	}
	if req.DeviceID != "" {
		add("e.device_id = ?", req.DeviceID)
	}
	if req.Cursor != "" {
		if ts, id, ok := parseCursor(req.Cursor); ok {
			args = append(args, ts, id)
			where = append(where, "(d.created_at, d.id) < ($"+itoa(len(args)-1)+", $"+itoa(len(args))+")")
		}
	}
	args = append(args, limit+1)
	query := `
		SELECT d.id::text, d.event_id, d.pipeline_id::text, coalesce(p.name, ''), d.connection_id::text,
		       coalesce(c.name, ''), e.device_id, e.event_type, d.state, coalesce(d.ack_level, ''),
		       d.attempts, d.next_attempt_at, coalesce(d.failure_code, ''), coalesce(d.failure_detail, ''),
		       d.target_receipt, e.occurred_at, d.created_at, d.delivered_at
		FROM nb_deliveries d
		JOIN nb_events e ON e.event_id = d.event_id
		LEFT JOIN nb_pipelines p ON p.id = d.pipeline_id
		LEFT JOIN nb_connections c ON c.id = d.connection_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT $` + itoa(len(args))
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, errUnavailable("delivery ledger unavailable")
	}
	defer rows.Close()
	out := &DeliveryListResponse{Deliveries: []DeliveryView{}}
	for rows.Next() {
		var v DeliveryView
		var receipt []byte
		if err := rows.Scan(&v.ID, &v.EventID, &v.PipelineID, &v.PipelineName, &v.ConnectionID, &v.ConnectionName,
			&v.DeviceID, &v.EventType, &v.State, &v.AckLevel, &v.Attempts, &v.NextAttemptAt,
			&v.FailureCode, &v.FailureDetail, &receipt, &v.OccurredAt, &v.CreatedAt, &v.DeliveredAt); err != nil {
			return nil, errUnavailable("delivery ledger unavailable")
		}
		if len(receipt) > 0 {
			v.Receipt = json.RawMessage(receipt)
		}
		out.Deliveries = append(out.Deliveries, v)
	}
	if len(out.Deliveries) > limit {
		last := out.Deliveries[limit-1]
		out.Deliveries = out.Deliveries[:limit]
		out.NextCursor = makeCursor(last.CreatedAt, last.ID)
	}
	return out, rows.Err()
}

// ReplayDelivery re-queues one failed or unknown delivery. The idempotency key
// is preserved: replay must not create a second business write on a target that
// already applied the first one.
//
//encore:api auth method=POST path=/v1/deliveries/:id/replay
func ReplayDelivery(ctx context.Context, id string, req *ReplayRequest) (*ReplayResponse, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionReplay); err != nil {
		return nil, err
	}
	deliveryID, err := parseUUID(id, "delivery id")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, errBadRequest("a reason is required to replay a delivery")
	}
	var state, connectorID string
	err = db.QueryRow(ctx, `
		SELECT d.state, coalesce(c.connector_id, '')
		FROM nb_deliveries d LEFT JOIN nb_connections c ON c.id = d.connection_id
		WHERE d.tenant_id = $1 AND d.id = $2::uuid`, data.TenantID, deliveryID).Scan(&state, &connectorID)
	if err != nil {
		return nil, errNotFound("delivery not found")
	}
	switch state {
	case StateDead, StateRetryWait, StatePending:
		// safe: nothing is in flight and the target has not confirmed.
	case StateUnknown:
		// The target may already have applied this record. Only allow it when
		// the connector declares strong idempotency, or when the operator
		// explicitly accepts the risk.
		inst, desc, derr := installationByID(ctx, connectorID)
		_ = inst
		strong := derr == nil && desc != nil && desc.Capabilities.Idempotency == "strong"
		if !strong && !req.AcknowledgeRisk {
			return nil, errConflict("the previous attempt has an unknown result; confirm the risk or use a connector with strong idempotency")
		}
	case StateDelivered:
		return nil, errConflict("this delivery already succeeded; replay would risk a duplicate write")
	case StateLeased:
		return nil, errConflict("this delivery is being sent right now")
	default:
		return nil, errConflict("delivery state does not allow replay")
	}
	tag, err := db.Exec(ctx, `
		UPDATE nb_deliveries
		SET state = 'pending', attempts = 0, next_attempt_at = now(), lease_token = NULL, lease_until = NULL,
		    failure_code = NULL, failure_detail = $3, updated_at = now()
		WHERE tenant_id = $1 AND id = $2::uuid AND state = $4`,
		data.TenantID, deliveryID, "replayed by "+data.UserID+": "+req.Reason, state)
	if err != nil {
		return nil, errUnavailable("delivery could not be replayed")
	}
	if tag.RowsAffected() != 1 {
		return nil, errConflict("delivery changed while replaying; reload and retry")
	}
	_, _ = db.Exec(ctx, `INSERT INTO nb_delivery_attempts (delivery_id, attempt, outcome, error_code, duration_ms)
		VALUES ($1::uuid, 0, 'replayed', 'operator_replay', 0)`, deliveryID)
	return &ReplayResponse{DeliveryID: deliveryID, State: StatePending, Message: "re-queued with the original idempotency key"}, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func makeCursor(ts time.Time, id string) string { return ts.UTC().Format(time.RFC3339Nano) + "|" + id }

func parseCursor(raw string) (time.Time, string, bool) {
	parts := strings.SplitN(raw, "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", false
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", false
	}
	return ts, parts[1], true
}
