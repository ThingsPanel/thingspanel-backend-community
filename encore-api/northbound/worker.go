package northbound

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"
)

// claimBatch picks due deliveries with FOR UPDATE SKIP LOCKED so several worker
// goroutines (or several replicas) never send the same record twice.
//
// The claim is also where an expired lease is recovered: a process that died
// mid-send leaves state='leased' with an old lease_until, and that row becomes
// eligible again. Recovery is therefore automatic, not an operator action.
func claimBatch(ctx context.Context, limit int) ([]claimedDelivery, error) {
	rows, err := db.Query(ctx, `
		WITH picked AS (
			SELECT id FROM nb_deliveries
			WHERE (state IN ('pending', 'retry_wait') AND next_attempt_at <= now())
			   OR (state = 'leased' AND lease_until < now())
			ORDER BY next_attempt_at, created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE nb_deliveries d
		SET state = 'leased',
		    attempts = d.attempts + 1,
		    lease_token = gen_random_uuid(),
		    lease_until = now() + $2::interval,
		    updated_at = now()
		FROM picked
		WHERE d.id = picked.id
		RETURNING d.id::text, d.event_id, d.tenant_id, d.pipeline_id::text, d.pipeline_version,
		          d.connection_id::text, d.connection_revision, d.idempotency_key, d.attempts,
		          d.lease_token::text, d.branch`, limit, fmt.Sprintf("%d seconds", int(cfg.Lease.Seconds())))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []claimedDelivery
	for rows.Next() {
		var c claimedDelivery
		if err := rows.Scan(&c.ID, &c.EventID, &c.TenantID, &c.PipelineID, &c.PipelineVersion,
			&c.ConnectionID, &c.ConnectionRevision, &c.IdempotencyKey, &c.Attempts, &c.LeaseToken, &c.Branch); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type claimedDelivery struct {
	ID                 string
	EventID            string
	TenantID           string
	PipelineID         string
	PipelineVersion    int64
	ConnectionID       string
	ConnectionRevision int64
	IdempotencyKey     string
	Attempts           int
	LeaseToken         string
	Branch             string
}

// deliveryContext is everything needed to build a write item.
type deliveryContext struct {
	DeviceID      string
	EventType     string
	OccurredAt    time.Time
	Subject       string
	Data          json.RawMessage
	Serialization json.RawMessage
	ConnectorID   string
	Config        json.RawMessage
	PipelineState string
	ConnState     string
}

func loadDeliveryContext(ctx context.Context, c claimedDelivery) (*deliveryContext, error) {
	var d deliveryContext
	var payload, transformRaw []byte
	err := db.QueryRow(ctx, `
		SELECT e.device_id, e.event_type, e.occurred_at, e.payload,
		       coalesce(p.state, 'disabled'), coalesce(c.state, 'disabled'),
		       coalesce(c.connector_id, ''), c.config, coalesce(v.serialization, '{}'::jsonb),
		       coalesce(v.transform, '{}'::jsonb)
		FROM nb_deliveries d
		JOIN nb_events e ON e.event_id = d.event_id
		LEFT JOIN nb_pipelines p ON p.id = d.pipeline_id
		LEFT JOIN nb_connections c ON c.id = d.connection_id
		LEFT JOIN nb_pipeline_versions v ON v.pipeline_id = d.pipeline_id AND v.version = d.pipeline_version
		WHERE d.id = $1::uuid`, c.ID).Scan(&d.DeviceID, &d.EventType, &d.OccurredAt, &payload,
		&d.PipelineState, &d.ConnState, &d.ConnectorID, &d.Config, &d.Serialization, &transformRaw)
	if err != nil {
		return nil, err
	}
	// The transform comes from the exact pipeline version pinned on the delivery
	// row, not from the pipeline's current settings. Editing a task therefore
	// only affects records created after the edit, and a record that was already
	// queued keeps the shape it was matched against.
	var t Transform
	if len(transformRaw) > 0 {
		if err := json.Unmarshal(transformRaw, &t); err != nil {
			return nil, err
		}
	}
	warnings := []string{}
	out, err := applyTransform(t, json.RawMessage(payload), &warnings)
	if err != nil {
		return nil, err
	}
	d.Data = out
	d.Subject = "devices/" + d.DeviceID
	return &d, nil
}

// deliverBatch sends one batch for a single connection and applies every result
// individually. A batch is only grouped by connection because the plugin is
// opened per connection; grouping by pipeline as well would fragment batches
// without changing semantics.
func deliverBatch(ctx context.Context, connectionID string, items []claimedDelivery) {
	// A pipeline that was paused or disabled between claim and send must not
	// reach the target. The record goes back to pending without consuming an
	// attempt, because nothing was attempted.
	active := make([]claimedDelivery, 0, len(items))
	ctxs := make(map[string]*deliveryContext, len(items))
	for _, it := range items {
		dc, err := loadDeliveryContext(ctx, it)
		if err != nil {
			markFailed(ctx, it, "context_unavailable", "delivery context could not be read", StateDead)
			continue
		}
		if dc.ConnState != "active" || dc.PipelineState != "active" {
			requeueWithoutAttempt(ctx, it, "pipeline_or_connection_inactive")
			continue
		}
		ctxs[it.ID] = dc
		active = append(active, it)
	}
	if len(active) == 0 {
		return
	}

	inst, desc, err := installationByID(ctx, ctxs[active[0].ID].ConnectorID)
	if err != nil {
		for _, it := range active {
			markFailed(ctx, it, "connector_unavailable", "connector is not installed", StateRetryWait)
		}
		return
	}
	secrets, err := loadSecrets(ctx, active[0].TenantID, connectionID)
	if err != nil {
		for _, it := range active {
			markFailed(ctx, it, "credential_unavailable", "stored credential cannot be read", StateDead)
		}
		return
	}
	opened, err := openPlugin(ctx, inst, connectionID, ctxs[active[0].ID].Config, secrets)
	if err != nil || !opened.OK {
		kind := classifyPluginError(err)
		state := StateRetryWait
		code := "connector_unreachable"
		if kind == pluginRejected {
			// A rejected credential or bad config will not fix itself by
			// retrying: stop and ask the operator to repair the connection.
			state = StateDead
			code = "connection_rejected"
			_, _ = db.Exec(ctx, `UPDATE nb_connections SET state = 'needs_repair', failure_code = $2, updated_at = now()
				WHERE id = $1::uuid`, connectionID, code)
		}
		for _, it := range active {
			markFailed(ctx, it, code, "the destination could not be opened", state)
		}
		return
	}

	writeItems := make([]WriteItem, 0, len(active))
	for _, it := range active {
		dc := ctxs[it.ID]
		writeItems = append(writeItems, WriteItem{
			DeliveryID:     it.ID,
			EventID:        it.EventID,
			IdempotencyKey: it.IdempotencyKey,
			EventType:      dc.EventType,
			Subject:        dc.Subject,
			OccurredAt:     dc.OccurredAt,
			DeviceID:       dc.DeviceID,
			Data:           dc.Data,
			Serialization:  dc.Serialization,
		})
	}
	if desc != nil && desc.Capabilities.MaxBatchEvents > 0 && len(writeItems) > desc.Capabilities.MaxBatchEvents {
		writeItems = writeItems[:desc.Capabilities.MaxBatchEvents]
	}
	attempt := active[0].Attempts
	started := time.Now()
	var resp WriteResponse
	werr := pluginCall(ctx, inst, "POST", pathWrite, WriteRequest{
		ContractVersion: ContractVersion,
		ConnectionID:    connectionID,
		InstanceID:      opened.InstanceID,
		PipelineID:      active[0].PipelineID,
		Attempt:         attempt,
		Items:           writeItems,
	}, &resp)
	duration := int(time.Since(started).Milliseconds())

	// Only the items actually included in the request may be updated. Anything
	// truncated by the batch limit stays pending for the next round.
	sent := make(map[string]WriteResult, len(resp.Results))
	for _, r := range resp.Results {
		sent[r.DeliveryID] = r
	}
	sentIDs := map[string]bool{}
	for _, it := range writeItems {
		sentIDs[it.DeliveryID] = true
	}

	if werr != nil {
		kind := classifyPluginError(werr)
		for _, it := range active {
			if !sentIDs[it.ID] {
				continue
			}
			switch kind {
			case pluginTimeout:
				// The plugin may have reached the target. This is exactly the
				// "unknown" case: do not retry blindly.
				markFailed(ctx, it, "target_result_unknown", "the connector did not answer before the deadline", StateUnknown)
			case pluginRejected:
				markFailed(ctx, it, "target_rejected", "the connector rejected the request", StateDead)
			default:
				retryOrDie(ctx, it, "connector_unreachable", "the connector could not be reached", 0)
			}
			recordAttempt(ctx, it, "plugin_error", "", "connector_unreachable", 0, duration)
		}
		return
	}

	for _, it := range active {
		if !sentIDs[it.ID] {
			continue
		}
		r, ok := sent[it.ID]
		if !ok {
			// The contract requires one result per item. A missing result is
			// unknown, never success.
			markFailed(ctx, it, "missing_result", "the connector returned no result for this record", StateUnknown)
			recordAttempt(ctx, it, "missing_result", "", "missing_result", 0, duration)
			continue
		}
		applyResult(ctx, it, r, duration)
	}
}

func applyResult(ctx context.Context, it claimedDelivery, r WriteResult, duration int) {
	switch r.Status {
	case OutcomeDelivered:
		ack := r.AckLevel
		if ack == "" {
			ack = AckDestinationAcked
		}
		receipt := "null"
		if len(r.Receipt) > 0 {
			receipt = string(r.Receipt)
		}
		tag, err := db.Exec(ctx, `
			UPDATE nb_deliveries
			SET state = 'delivered', ack_level = $3, target_receipt = $4::jsonb, delivered_at = now(),
			    failure_code = NULL, lease_token = NULL, lease_until = NULL, updated_at = now()
			WHERE id = $1::uuid AND lease_token = $2::uuid AND state = 'leased'`,
			it.ID, it.LeaseToken, ack, receipt)
		if err != nil || tag.RowsAffected() != 1 {
			// The lease was taken over by another worker. Never overwrite the
			// newer owner's result.
			recordAttempt(ctx, it, "lease_lost", ack, "lease_lost", 0, duration)
			return
		}
		recordAttempt(ctx, it, OutcomeDelivered, ack, "", 0, duration)
	case OutcomeRetryable:
		safe := true
		if r.SafeToRetry != nil {
			safe = *r.SafeToRetry
		}
		if !safe {
			markFailed(ctx, it, "not_safe_to_retry", r.ErrorDetail, StateUnknown)
			recordAttempt(ctx, it, OutcomeUnknown, r.AckLevel, r.ErrorCode, r.RetryAfterMS, duration)
			return
		}
		retryOrDie(ctx, it, orDefault(r.ErrorCode, "retryable_failure"), r.ErrorDetail, r.RetryAfterMS)
		recordAttempt(ctx, it, OutcomeRetryable, r.AckLevel, r.ErrorCode, r.RetryAfterMS, duration)
	case OutcomePermanent:
		markFailed(ctx, it, orDefault(r.ErrorCode, "permanent_failure"), r.ErrorDetail, StateDead)
		recordAttempt(ctx, it, OutcomePermanent, r.AckLevel, r.ErrorCode, 0, duration)
		if r.ErrorCode == "authentication_failed" || r.ErrorCode == "config_invalid" {
			_, _ = db.Exec(ctx, `UPDATE nb_connections SET state = 'needs_repair', failure_code = $2, updated_at = now()
				WHERE id = $1::uuid`, it.ConnectionID, r.ErrorCode)
		}
	case OutcomeUnknown:
		// The plugin itself says it cannot tell whether the target acted on the
		// record. This must never be retried automatically and never counted as
		// success; the plugin's own reason is kept so the operator can decide.
		code := orDefault(r.ErrorCode, "unknown_result")
		detail := orDefault(r.ErrorDetail, redact(r.Status))
		markFailed(ctx, it, code, detail, StateUnknown)
		recordAttempt(ctx, it, OutcomeUnknown, r.AckLevel, code, 0, duration)
	default:
		// A status the platform does not recognise is unknown, not success. The
		// plugin's code and detail are preserved for the same reason as above.
		code := orDefault(r.ErrorCode, "unknown_status")
		detail := orDefault(r.ErrorDetail, redact(r.Status))
		markFailed(ctx, it, code, detail, StateUnknown)
		recordAttempt(ctx, it, OutcomeUnknown, r.AckLevel, code, 0, duration)
	}
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// retryOrDie schedules the next attempt with exponential backoff and jitter, or
// moves the record to dead when the attempt budget is spent.
// nextBackoff is the single place that decides how long to wait before the next
// attempt. A target that asked for a longer delay wins, because ignoring it
// would just make the target reject the record again.
func nextBackoff(attempt int, retryAfterMS int64) time.Duration {
	backoff := backoffFor(attempt)
	if retryAfterMS > 0 {
		hint := time.Duration(retryAfterMS) * time.Millisecond
		if hint > backoff {
			backoff = hint
		}
	}
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	return backoff
}

func retryOrDie(ctx context.Context, it claimedDelivery, code, detail string, retryAfterMS int64) {
	if it.Attempts >= cfg.MaxAttempts {
		markFailed(ctx, it, "retry_exhausted", detail, StateDead)
		return
	}
	backoff := nextBackoff(it.Attempts, retryAfterMS)
	_, err := db.Exec(ctx, `
		UPDATE nb_deliveries
		SET state = 'retry_wait', next_attempt_at = now() + $3::interval, failure_code = $4,
		    failure_detail = $5, lease_token = NULL, lease_until = NULL, updated_at = now()
		WHERE id = $1::uuid AND lease_token = $2::uuid AND state = 'leased'`,
		it.ID, it.LeaseToken, fmt.Sprintf("%d milliseconds", backoff.Milliseconds()), code, redact(detail))
	if err != nil {
		log.Printf("northbound: retry scheduling failed for %s: %v", it.ID, err)
	}
}

func backoffFor(attempt int) time.Duration {
	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 9 {
		shift = 9
	}
	base := time.Second * time.Duration(1<<shift)
	// ±20% jitter keeps a large backlog from retrying in lockstep.
	jitter := time.Duration((rand.Float64()*0.4 - 0.2) * float64(base))
	d := base + jitter
	if d < 200*time.Millisecond {
		d = 200 * time.Millisecond
	}
	return d
}

func markFailed(ctx context.Context, it claimedDelivery, code, detail, state string) {
	if state == StateRetryWait {
		retryOrDie(ctx, it, code, detail, 0)
		return
	}
	_, err := db.Exec(ctx, `
		UPDATE nb_deliveries
		SET state = $3, failure_code = $4, failure_detail = $5, lease_token = NULL, lease_until = NULL,
		    updated_at = now()
		WHERE id = $1::uuid AND lease_token = $2::uuid AND state = 'leased'`,
		it.ID, it.LeaseToken, state, code, redact(detail))
	if err != nil {
		log.Printf("northbound: marking delivery %s as %s failed: %v", it.ID, state, err)
	}
}

// requeueWithoutAttempt returns a record to pending without consuming an
// attempt, for conditions where nothing was sent.
func requeueWithoutAttempt(ctx context.Context, it claimedDelivery, code string) {
	_, _ = db.Exec(ctx, `
		UPDATE nb_deliveries
		SET state = 'pending', attempts = GREATEST(attempts - 1, 0), next_attempt_at = now() + interval '2 seconds',
		    failure_code = $3, lease_token = NULL, lease_until = NULL, updated_at = now()
		WHERE id = $1::uuid AND lease_token = $2::uuid AND state = 'leased'`, it.ID, it.LeaseToken, code)
}

func recordAttempt(ctx context.Context, it claimedDelivery, outcome, ack, code string, retryAfterMS int64, duration int) {
	_, _ = db.Exec(ctx, `INSERT INTO nb_delivery_attempts (delivery_id, attempt, outcome, ack_level, error_code, retry_after_ms, duration_ms)
		VALUES ($1::uuid, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7)`,
		it.ID, it.Attempts, outcome, ack, code, retryAfterMS, duration)
}

// ---------------------------------------------------------------------------
// Worker loop
// ---------------------------------------------------------------------------

func (r *runtimeState) startWorkers() {
	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.deliveryLoop()
		}()
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.maintenanceLoop()
	}()
}

func (r *runtimeState) deliveryLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		claimed, err := claimBatch(ctx, 20)
		if err != nil {
			cancel()
			continue
		}
		if len(claimed) == 0 {
			cancel()
			continue
		}
		// Group by connection so one plugin open serves the whole batch.
		byConnection := map[string][]claimedDelivery{}
		for _, c := range claimed {
			byConnection[c.ConnectionID] = append(byConnection[c.ConnectionID], c)
		}
		for connID, items := range byConnection {
			deliverBatch(ctx, connID, items)
		}
		cancel()
	}
}

func (r *runtimeState) maintenanceLoop() {
	// Health probe and retention share one slow loop; neither is on the
	// delivery path.
	probe := time.NewTicker(30 * time.Second)
	retention := time.NewTicker(10 * time.Minute)
	defer probe.Stop()
	defer retention.Stop()
	// Probe once at startup so the page does not show stale health.
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		probeInstallations(ctx)
	}()
	for {
		select {
		case <-r.stop:
			return
		case <-probe.C:
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			probeInstallations(ctx)
			cancel()
		case <-retention.C:
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			pruneExpired(ctx)
			cancel()
		}
	}
}

// pruneExpired deletes only finished records past retention. Undelivered
// records are never deleted by the retention job, so a long outage cannot
// silently destroy a backlog.
func pruneExpired(ctx context.Context) {
	_, _ = db.Exec(ctx, `DELETE FROM nb_deliveries
		WHERE state IN ('delivered','dead')
		  AND updated_at < now() - $1::interval`, fmt.Sprintf("%d hours", int(cfg.DeliveryRetention.Hours())))
	_, _ = db.Exec(ctx, `DELETE FROM nb_events e
		WHERE e.expires_at < now()
		  AND NOT EXISTS (SELECT 1 FROM nb_deliveries d WHERE d.event_id = e.event_id
		                  AND d.state IN ('pending','leased','retry_wait','unknown'))`)
}
