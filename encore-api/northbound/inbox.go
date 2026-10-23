package northbound

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
)

// IngestEvents accepts a batch of source events from the Community runtime.
// This is the only door data comes in through. It is authenticated with the
// service token, so no user token can post events for another tenant.
//
//encore:api public method=POST path=/v1/inbox/events
func IngestEvents(ctx context.Context, req *InboxRequest) (*InboxResponse, error) {
	if err := requireInternal(req.ServiceToken); err != nil {
		return nil, err
	}
	if req.ContractVersion != SourceContractVersion {
		return nil, errBadRequest("unsupported source contract version " + req.ContractVersion)
	}
	res := &InboxResponse{Rejected: []InboxRejection{}}
	if len(req.Events) == 0 {
		return res, nil
	}
	if len(req.Events) > 500 {
		return nil, errBadRequest("at most 500 events per batch")
	}
	// One relay process may carry several tenants, so the tenant comes from
	// each envelope. The relay only ever forwards events it already persisted
	// under that same tenant.
	grouped := map[string][]SourceEnvelope{}
	order := []string{}
	for _, env := range req.Events {
		if env.TenantID == "" {
			res.Rejected = append(res.Rejected, InboxRejection{EventID: env.EventID, Code: "missing_tenant"})
			continue
		}
		if _, seen := grouped[env.TenantID]; !seen {
			order = append(order, env.TenantID)
		}
		grouped[env.TenantID] = append(grouped[env.TenantID], env)
	}
	for _, tenantID := range order {
		if err := acceptEvents(ctx, tenantID, grouped[tenantID], res); err != nil {
			return nil, err
		}
	}
	if req.SourceProfile != "" {
		recordRuntimeFact(ctx, "source_profile", req.SourceProfile)
	}
	return res, nil
}

// acceptEvents writes each payload once and fans out one delivery per matching
// pipeline branch. Everything happens in one transaction per tenant, so an
// event is never stored without its deliveries, and a failure for one branch
// never clears another branch's work.
func acceptEvents(ctx context.Context, tenantID string, events []SourceEnvelope, res *InboxResponse) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return errUnavailable("northbound inbox unavailable")
	}
	defer func() { _ = tx.Rollback() }()

	type pipelineRef struct {
		id, connectionID string
		version          int64
		connectionRev    int64
	}
	var pipelines []pipelineRef
	rows, err := tx.Query(ctx, `
		SELECT p.id::text, p.connection_id::text, p.active_version, c.revision
		FROM nb_pipelines p
		JOIN nb_connections c ON c.id = p.connection_id
		WHERE p.tenant_id = $1 AND p.state = 'active' AND p.active_version IS NOT NULL
		  AND c.state = 'active'
		ORDER BY p.id`, tenantID)
	if err != nil {
		return errUnavailable("northbound inbox unavailable")
	}
	for rows.Next() {
		var p pipelineRef
		if err := rows.Scan(&p.id, &p.connectionID, &p.version, &p.connectionRev); err != nil {
			rows.Close()
			return errUnavailable("northbound inbox unavailable")
		}
		pipelines = append(pipelines, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return errUnavailable("northbound inbox unavailable")
	}

	for _, env := range events {
		if len(env.EventID) == 0 || len(env.EventID) > 200 {
			res.Rejected = append(res.Rejected, InboxRejection{EventID: env.EventID, Code: "invalid_event_id"})
			continue
		}
		if !isValidEventType(env.EventType) {
			res.Rejected = append(res.Rejected, InboxRejection{EventID: env.EventID, Code: "invalid_event_type", Detail: env.EventType})
			continue
		}
		if env.DeviceID == "" {
			res.Rejected = append(res.Rejected, InboxRejection{EventID: env.EventID, Code: "missing_device"})
			continue
		}
		if len(env.Data) > cfg.MaxEventBytes {
			res.Rejected = append(res.Rejected, InboxRejection{EventID: env.EventID, Code: "payload_too_large"})
			continue
		}
		if len(env.Data) == 0 {
			env.Data = json.RawMessage(`{}`)
		}
		if env.OccurredAt.IsZero() {
			env.OccurredAt = time.Now().UTC()
		}
		if env.ReceivedAt.IsZero() {
			env.ReceivedAt = env.OccurredAt
		}
		if env.Subject == "" {
			env.Subject = "devices/" + env.DeviceID
		}

		// Capacity is checked before the payload is stored, so a rejected event
		// leaves no half-written row that would later look like "waiting".
		var pending int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nb_deliveries
			WHERE tenant_id = $1 AND state IN ('pending','leased','retry_wait')`, tenantID).Scan(&pending); err != nil {
			return errUnavailable("northbound inbox unavailable")
		}
		if pending >= cfg.MaxBacklogEvents {
			if err := recordCapacityReject(ctx, tx, tenantID); err != nil {
				return err
			}
			res.Rejected = append(res.Rejected, InboxRejection{EventID: env.EventID, Code: "capacity_exceeded",
				Detail: fmt.Sprintf("tenant backlog reached %d records", pending)})
			continue
		}

		digest := sha256Bytes(env.Data)
		tag, err := tx.Exec(ctx, `
			INSERT INTO nb_events (event_id, tenant_id, device_id, event_type, occurred_at, received_at,
				payload, payload_sha256, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)
			ON CONFLICT (event_id) DO NOTHING`,
			env.EventID, tenantID, env.DeviceID, env.EventType, env.OccurredAt.UTC(), env.ReceivedAt.UTC(),
			string(env.Data), digest, time.Now().UTC().Add(cfg.EventRetention))
		if err != nil {
			rlog.Error("northbound: event insert failed",
				"tenant", tenantID, "event", env.EventID, "err", err)
			return errUnavailable("northbound inbox unavailable")
		}
		if tag.RowsAffected() == 0 {
			// The event id already exists. The same payload is a duplicate; a
			// different payload under the same id is a source defect and must be
			// visible instead of silently dropped.
			var existing []byte
			if err := tx.QueryRow(ctx, `SELECT payload_sha256 FROM nb_events WHERE event_id = $1`, env.EventID).Scan(&existing); err != nil {
				return errUnavailable("northbound inbox unavailable")
			}
			if string(existing) != string(digest) {
				res.Rejected = append(res.Rejected, InboxRejection{EventID: env.EventID, Code: "event_id_conflict"})
				continue
			}
			res.Duplicate++
		} else {
			res.Accepted++
		}

		for _, p := range pipelines {
			created, err := fanoutToPipeline(ctx, tx, tenantID, p.id, p.connectionID, p.version, p.connectionRev, env)
			if err != nil {
				return err
			}
			if created {
				res.Fanout++
			}
		}
	}
	return tx.Commit()
}

// fanoutToPipeline creates exactly one delivery row for this pipeline branch.
// The row is inserted with ON CONFLICT DO NOTHING, so a repeated inbox batch
// cannot create a second ledger entry for the same logical branch.
func fanoutToPipeline(ctx context.Context, tx txConn, tenantID, pipelineID, connectionID string, version, connectionRev int64, env SourceEnvelope) (bool, error) {
	var filterRaw, transformRaw, serializationRaw []byte
	var versionConnection string
	err := tx.QueryRow(ctx, `
		SELECT filter, transform, serialization, connection_id::text
		FROM nb_pipeline_versions WHERE pipeline_id = $1::uuid AND version = $2`,
		pipelineID, version).Scan(&filterRaw, &transformRaw, &serializationRaw, &versionConnection)
	if err != nil {
		return false, errUnavailable("forwarding task version unavailable")
	}
	var filter Filter
	if err := json.Unmarshal(filterRaw, &filter); err != nil {
		return false, nil
	}
	matched, _ := matchEvent(filter, env)
	if !matched {
		return false, nil
	}
	var transform Transform
	if err := json.Unmarshal(transformRaw, &transform); err != nil {
		// A stored version that no longer parses is a platform defect, not a
		// property of this event, so the delivery is not created and the reason
		// is logged instead of being hidden in the ledger.
		rlog.Error("northbound: pipeline version transform is unreadable",
			"tenant", tenantID, "pipeline", pipelineID, "version", version, "err", err)
		return false, nil
	}
	branch := "default"
	deliveryID := DeliveryID(tenantID, pipelineID, env.EventID, branch)
	key := IdempotencyKey(tenantID, pipelineID, env.EventID, branch)
	// order_key is a plain text column, and PostgreSQL rejects NUL bytes in
	// text, so the segments are joined with a unit separator (0x1f) instead of
	// the NUL used for the hashed idempotency key.
	orderKey := tenantID + "\x1f" + pipelineID + "\x1f" + env.DeviceID
	tag, err := tx.Exec(ctx, `
		INSERT INTO nb_deliveries (id, event_id, tenant_id, pipeline_id, pipeline_version, connection_id,
			connection_revision, branch, idempotency_key, order_key, state, next_attempt_at)
		VALUES ($1::uuid, $2, $3, $4::uuid, $5, $6::uuid, $7, $8, $9, $10, 'pending', now())
		ON CONFLICT (pipeline_id, event_id, branch) DO NOTHING`,
		deliveryID, env.EventID, tenantID, pipelineID, version, connectionID, connectionRev, branch, key, orderKey)
	if err != nil {
		// The caller gets a generic message on purpose: the database detail is
		// not the caller's business. Operators still need it, so it goes to the
		// service log with the identifiers needed to trace the record.
		rlog.Error("northbound: delivery insert failed",
			"tenant", tenantID, "pipeline", pipelineID, "event", env.EventID, "err", err)
		return false, errUnavailable("delivery could not be recorded")
	}
	return tag.RowsAffected() == 1, nil
}

func recordCapacityReject(ctx context.Context, tx txConn, tenantID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO nb_tenant_quota (tenant_id, rejected_total, last_reject_at, last_reject_code, updated_at)
		VALUES ($1, 1, now(), 'capacity_exceeded', now())
		ON CONFLICT (tenant_id) DO UPDATE SET rejected_total = nb_tenant_quota.rejected_total + 1,
			last_reject_at = now(), last_reject_code = 'capacity_exceeded', updated_at = now()`, tenantID)
	if err != nil {
		return errUnavailable("northbound inbox unavailable")
	}
	return nil
}

func recordRuntimeFact(ctx context.Context, key, value string) {
	_, _ = db.Exec(ctx, `INSERT INTO nb_runtime_facts (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = now()`, key, value)
}

func readRuntimeFact(ctx context.Context, key, fallback string) string {
	var v string
	if err := db.QueryRow(ctx, `SELECT value FROM nb_runtime_facts WHERE key = $1`, key).Scan(&v); err != nil {
		return fallback
	}
	return v
}

// txConn is the subset of a transaction this package needs. Encore's
// *sqldb.Tx satisfies it, and so does a plain pgx.Tx in tests.
type txConn interface {
	Exec(ctx context.Context, sql string, args ...any) (sqldb.ExecResult, error)
	QueryRow(ctx context.Context, sql string, args ...any) *sqldb.Row
}
