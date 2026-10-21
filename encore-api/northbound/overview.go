package northbound

import (
	"context"
	"time"

	"encore.app/internal/authz"
)

// Overview is the page's single status call: what is installed, what is
// configured, what is waiting, what failed, and what the platform can honestly
// promise about this deployment.
//
//encore:api auth method=GET path=/v1/overview
func Overview(ctx context.Context) (*OverviewResponse, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionView); err != nil {
		return nil, err
	}
	out := &OverviewResponse{
		ContractVersion: ContractVersion,
		TenantID:        data.TenantID,
		GeneratedAt:     time.Now().UTC(),
	}
	out.Capabilities = OverviewCaps{
		SourceProfile:    readRuntimeFact(ctx, "source_profile", "unknown"),
		GuaranteeStart:   "event accepted into the Community durable outbox",
		MaxBacklogEvents: cfg.MaxBacklogEvents,
		RetentionDays:    int(cfg.EventRetention.Hours() / 24),
	}
	catalog, err := ListConnectors(ctx)
	if err == nil {
		out.Connectors = catalog.Connectors
	} else {
		out.Connectors = []ConnectorView{}
	}
	_ = db.QueryRow(ctx, `SELECT count(*) FROM nb_connections WHERE tenant_id = $1`, data.TenantID).Scan(&out.Connections)
	_ = db.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE state = 'active')
		FROM nb_pipelines WHERE tenant_id = $1`, data.TenantID).Scan(&out.Pipelines, &out.ActivePipelines)
	_ = db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE state = 'pending'),
		       count(*) FILTER (WHERE state = 'retry_wait'),
		       count(*) FILTER (WHERE state = 'delivered' AND delivered_at > now() - interval '24 hours'),
		       count(*) FILTER (WHERE state = 'unknown'),
		       count(*) FILTER (WHERE state = 'dead'),
		       min(created_at) FILTER (WHERE state IN ('pending','leased','retry_wait'))
		FROM nb_deliveries WHERE tenant_id = $1`, data.TenantID).
		Scan(&out.Deliveries.Pending, &out.Deliveries.RetryWait, &out.Deliveries.Delivered,
			&out.Deliveries.Unknown, &out.Deliveries.Dead, &out.Deliveries.OldestAt)
	if out.Deliveries.OldestAt != nil {
		out.Deliveries.OldestAgeMS = time.Since(*out.Deliveries.OldestAt).Milliseconds()
	}
	var lastReject *time.Time
	var lastCode *string
	_ = db.QueryRow(ctx, `SELECT rejected_total, last_reject_at, last_reject_code
		FROM nb_tenant_quota WHERE tenant_id = $1`, data.TenantID).
		Scan(&out.Rejects.CapacityRejected, &lastReject, &lastCode)
	out.Rejects.LastRejectAt = lastReject
	if lastCode != nil {
		out.Rejects.LastRejectCode = *lastCode
	}
	return out, nil
}
