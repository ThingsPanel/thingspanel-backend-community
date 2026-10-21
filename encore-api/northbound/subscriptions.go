package northbound

import (
	"context"

	"encore.dev/beta/errs"
)

// SubscriptionRequest authenticates the relay's subscription refresh with the
// same service token used for the inbox. There is no user-facing variant: a
// tenant must not be able to enumerate other tenants.
type SubscriptionRequest struct {
	ServiceToken string `header:"X-Northbound-Token"`
}

// TenantSubscription tells the Community relay which tenants currently have at
// least one active forwarding task. It is the only configuration fact the source
// side caches, and it never contains payload or credentials.
type TenantSubscription struct {
	TenantID        string `json:"tenantId"`
	ActivePipelines int64  `json:"activePipelines"`
}

type SubscriptionResponse struct {
	Tenants []TenantSubscription `json:"tenants"`
}

// Subscriptions returns the tenants with active forwarding tasks.
//
//encore:api public method=GET path=/v1/subscriptions
func Subscriptions(ctx context.Context, req *SubscriptionRequest) (*SubscriptionResponse, error) {
	if err := requireInternal(req.ServiceToken); err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `
		SELECT p.tenant_id, count(*) FILTER (WHERE p.state = 'active')
		FROM nb_pipelines p
		JOIN nb_connections c ON c.id = p.connection_id
		WHERE p.state = 'active' AND c.state = 'active'
		GROUP BY p.tenant_id`)
	if err != nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "subscription list unavailable"}
	}
	defer rows.Close()
	out := &SubscriptionResponse{Tenants: []TenantSubscription{}}
	for rows.Next() {
		var t TenantSubscription
		if err := rows.Scan(&t.TenantID, &t.ActivePipelines); err != nil {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "subscription list unavailable"}
		}
		out.Tenants = append(out.Tenants, t)
	}
	return out, rows.Err()
}
