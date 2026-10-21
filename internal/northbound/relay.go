package northbound

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Relay moves enrolled events from the Community outbox to the northbound
// service. It is the only component that talks to Encore, and it is stateless:
// stopping it loses nothing, because the outbox is durable.
type Relay struct {
	db     *gorm.DB
	logger *logrus.Logger
	client *http.Client
	stop   chan struct{}
	done   chan struct{}
}

// Start launches the relay loop. It is a no-op when northbound is disabled.
func Start(db *gorm.DB, log *logrus.Logger) *Relay {
	if !Enabled() || db == nil {
		return nil
	}
	r := &Relay{
		db:     db,
		logger: log,
		client: &http.Client{Timeout: 20 * time.Second},
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go r.loop()
	return r
}

// Stop waits for the current batch to finish.
func (r *Relay) Stop() {
	if r == nil {
		return
	}
	close(r.stop)
	<-r.done
}

func (r *Relay) loop() {
	defer close(r.done)
	cfg := Current()
	relay := time.NewTicker(cfg.RelayInterval)
	subscribe := time.NewTicker(cfg.SubscribeInterval)
	defer relay.Stop()
	defer subscribe.Stop()
	// Refresh once at start so a restart does not wait a full interval before
	// honouring newly created tasks.
	r.refreshSubscriptions()
	for {
		select {
		case <-r.stop:
			return
		case <-relay.C:
			r.relayOnce()
		case <-subscribe.C:
			r.refreshSubscriptions()
		}
	}
}

type outboxRow struct {
	ID         string    `gorm:"column:id"`
	TenantID   string    `gorm:"column:tenant_id"`
	DeviceID   string    `gorm:"column:device_id"`
	EventType  string    `gorm:"column:event_type"`
	EventID    string    `gorm:"column:event_id"`
	Payload    []byte    `gorm:"column:payload"`
	OccurredAt time.Time `gorm:"column:occurred_at"`
	Attempts   int       `gorm:"column:attempts"`
	LeaseToken string    `gorm:"column:lease_token"`
}

func (r *Relay) quiet() *gorm.DB {
	return r.db.Session(&gorm.Session{Logger: logger.Discard})
}

// claim leases a batch. An expired lease is recovered here, so a relay crash
// mid-handoff resumes automatically.
func (r *Relay) claim() ([]outboxRow, error) {
	cfg := Current()
	var rows []outboxRow
	err := r.quiet().Transaction(func(tx *gorm.DB) error {
		return tx.Raw(`WITH picked AS (
				SELECT id FROM northbound_source_outbox
				WHERE (state = 'pending' AND next_attempt_at <= now())
				   OR (state = 'leased' AND lease_until < now())
				ORDER BY next_attempt_at, created_at
				FOR UPDATE SKIP LOCKED
				LIMIT ?
			)
			UPDATE northbound_source_outbox o
			SET state = 'leased', attempts = o.attempts + 1, lease_token = gen_random_uuid(),
			    lease_until = now() + make_interval(secs => ?), updated_at = now()
			FROM picked WHERE o.id = picked.id
			RETURNING o.id::text, o.tenant_id, o.device_id, o.event_type, o.event_id,
			          o.payload::text AS payload, o.occurred_at, o.attempts, o.lease_token::text`,
			cfg.RelayBatch, int(cfg.Lease.Seconds())).Scan(&rows).Error
	})
	return rows, err
}

type relayEnvelope struct {
	ContractVersion string          `json:"contractVersion"`
	EventID         string          `json:"eventId"`
	TenantID        string          `json:"tenantId"`
	DeviceID        string          `json:"deviceId"`
	EventType       string          `json:"eventType"`
	OccurredAt      time.Time       `json:"occurredAt"`
	ReceivedAt      time.Time       `json:"receivedAt"`
	Source          string          `json:"source"`
	Subject         string          `json:"subject"`
	Data            json.RawMessage `json:"data"`
}

type relayInboxRequest struct {
	ContractVersion string          `json:"contractVersion"`
	SourceProfile   string          `json:"sourceProfile"`
	Events          []relayEnvelope `json:"events"`
}

type relayInboxResponse struct {
	Accepted  int `json:"accepted"`
	Duplicate int `json:"duplicate"`
	Fanout    int `json:"fanout"`
	Rejected  []struct {
		EventID string `json:"eventId"`
		Code    string `json:"code"`
		Detail  string `json:"detail"`
	} `json:"rejected"`
}

func (r *Relay) relayOnce() {
	rows, err := r.claim()
	if err != nil || len(rows) == 0 {
		if err != nil {
			r.noteError("claim failed: " + err.Error())
		}
		return
	}
	cfg := Current()
	req := relayInboxRequest{ContractVersion: SourceContractVersion, SourceProfile: cfg.SourceProfile}
	for _, row := range rows {
		payload := json.RawMessage(row.Payload)
		if !json.Valid(payload) {
			r.finishFailed(row, "invalid_payload")
			continue
		}
		req.Events = append(req.Events, relayEnvelope{
			ContractVersion: SourceContractVersion,
			EventID:         row.EventID,
			TenantID:        row.TenantID,
			DeviceID:        row.DeviceID,
			EventType:       row.EventType,
			OccurredAt:      row.OccurredAt.UTC(),
			ReceivedAt:      row.OccurredAt.UTC(),
			Source:          "thingspanel.community",
			Subject:         "devices/" + row.DeviceID,
			Data:            payload,
		})
	}
	if len(req.Events) == 0 {
		return
	}
	body, err := json.Marshal(req)
	if err != nil {
		for _, row := range rows {
			r.finishFailed(row, "encode_failed")
		}
		return
	}
	url := strings.TrimRight(cfg.EncoreBaseURL, "/") + "/v1/inbox/events"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		r.noteError("request build failed: " + err.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// The service token is a header, so it never becomes part of a stored event.
	httpReq.Header.Set("X-Northbound-Token", cfg.ServiceToken)
	resp, err := r.client.Do(httpReq)
	if err != nil {
		r.retryAll(rows, "northbound_unreachable")
		r.noteError("handoff failed: " + err.Error())
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		r.retryAll(rows, fmt.Sprintf("handoff_status_%d", resp.StatusCode))
		r.noteError(fmt.Sprintf("handoff returned %d: %s", resp.StatusCode, truncate(string(raw), 200)))
		return
	}
	var out relayInboxResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		r.retryAll(rows, "handoff_bad_response")
		r.noteError("handoff response could not be decoded")
		return
	}
	rejected := map[string]string{}
	for _, rej := range out.Rejected {
		rejected[rej.EventID] = rej.Code
	}
	for _, row := range rows {
		if code, bad := rejected[row.EventID]; bad {
			if code == "capacity_exceeded" {
				// The northbound side is at its own capacity: keep the record
				// enrolled and retry later instead of dropping it.
				r.retry(row, code)
				continue
			}
			r.finishFailed(row, code)
			continue
		}
		r.finishHandedOff(row)
	}
	r.noteSuccess()
}

func (r *Relay) finishHandedOff(row outboxRow) {
	_ = r.quiet().Exec(`UPDATE northbound_source_outbox
		SET state = 'handed_off', handed_off_at = now(), lease_token = NULL, lease_until = NULL,
		    failure_code = NULL, updated_at = now()
		WHERE id = ?::uuid AND lease_token = ?::uuid`, row.ID, row.LeaseToken).Error
}

func (r *Relay) finishFailed(row outboxRow, code string) {
	_ = r.quiet().Exec(`UPDATE northbound_source_outbox
		SET state = 'failed', failure_code = ?, lease_token = NULL, lease_until = NULL, updated_at = now()
		WHERE id = ?::uuid AND lease_token = ?::uuid`, truncate(code, 80), row.ID, row.LeaseToken).Error
}

// retry returns the row to pending with backoff, or marks it failed when the
// attempt budget is exhausted. The row stays enrolled either way.
func (r *Relay) retry(row outboxRow, code string) {
	cfg := Current()
	if row.Attempts >= cfg.MaxAttempts {
		r.finishFailed(row, "retry_exhausted:"+code)
		return
	}
	backoff := time.Duration(1<<min(row.Attempts, 8)) * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	_ = r.quiet().Exec(`UPDATE northbound_source_outbox
		SET state = 'pending', next_attempt_at = now() + make_interval(secs => ?),
		    failure_code = ?, lease_token = NULL, lease_until = NULL, updated_at = now()
		WHERE id = ?::uuid AND lease_token = ?::uuid`,
		backoff.Seconds(), truncate(code, 80), row.ID, row.LeaseToken).Error
}

func (r *Relay) retryAll(rows []outboxRow, code string) {
	for _, row := range rows {
		r.retry(row, code)
	}
}

func (r *Relay) noteSuccess() {
	_ = r.quiet().Exec(`INSERT INTO northbound_source_relay_state (id, last_success_at, last_error, last_error_at)
		VALUES (1, now(), NULL, NULL)
		ON CONFLICT (id) DO UPDATE SET last_success_at = now(), last_error = NULL`).Error
}

func (r *Relay) noteError(msg string) {
	if r.logger != nil {
		r.logger.Warnf("northbound relay: %s", truncate(msg, 200))
	}
	_ = r.quiet().Exec(`INSERT INTO northbound_source_relay_state (id, last_error, last_error_at)
		VALUES (1, ?, now())
		ON CONFLICT (id) DO UPDATE SET last_error = excluded.last_error, last_error_at = now()`,
		truncate(msg, 200)).Error
}

// refreshSubscriptions pulls the authoritative set of tenants with active tasks.
// On failure the previous cache is kept: enrolment continues and the backlog
// grows, which is visible, instead of silently stopping.
func (r *Relay) refreshSubscriptions() {
	cfg := Current()
	url := strings.TrimRight(cfg.EncoreBaseURL, "/") + "/v1/subscriptions"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Northbound-Token", cfg.ServiceToken)
	resp, err := r.client.Do(req)
	if err != nil {
		r.noteError("subscription refresh failed: " + err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		r.noteError(fmt.Sprintf("subscription refresh returned %d", resp.StatusCode))
		return
	}
	var out SubscriptionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		r.noteError("subscription refresh could not be decoded")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = r.quiet().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE northbound_source_tenants SET active_pipelines = 0, refreshed_at = now()`).Error; err != nil {
			return err
		}
		for _, t := range out.Tenants {
			if t.TenantID == "" {
				continue
			}
			if err := tx.Exec(`INSERT INTO northbound_source_tenants (tenant_id, active_pipelines, refreshed_at)
				VALUES (?, ?, now())
				ON CONFLICT (tenant_id) DO UPDATE SET active_pipelines = excluded.active_pipelines, refreshed_at = now()`,
				t.TenantID, t.ActivePipelines).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		r.noteError("subscription cache update failed: " + err.Error())
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var errRelayDisabled = errors.New("northbound relay is disabled")
