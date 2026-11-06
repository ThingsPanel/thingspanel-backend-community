package plugininstall

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Reconcile advances one row by at most one operation pass. It is safe to call from several processes: the lease is
// a conditional UPDATE. Every step either is idempotent or is guarded by data already in the row, so a crash at any
// point only means the next pass starts again from the top of the same operation.
func (e *Engine) Reconcile(ctx context.Context, id string) error {
	now := e.now()
	ok, err := e.Repo.Claim(id, e.Owner, now, now.Add(e.Timing.Lease))
	if err != nil || !ok {
		return err
	}
	row, err := e.Repo.byID(id)
	if err != nil {
		_ = e.Repo.Release(id, e.Owner, nil)
		return err
	}
	var wait *time.Duration
	switch {
	case row.Desired == DesiredEnabled && row.State == StateInstalling:
		wait = e.install(ctx, row)
	case row.Desired == DesiredDisabled && row.State == StateInstalling:
		wait = e.stop(ctx, row, StateInstalled, "disable")
	case row.Desired == DesiredAbsent && row.State == StateUninstalling:
		wait = e.stop(ctx, row, StateUninstalled, "uninstall")
	case row.State == StateEnabled:
		wait = e.maintain(ctx, row)
	}
	var until *time.Time
	if wait != nil {
		t := e.now().Add(*wait)
		until = &t
	}
	return e.Repo.Release(id, e.Owner, until)
}

func (e *Engine) event(row *Installation, step, outcome, code string) {
	_ = e.Repo.AddEvent(&Event{InstallationID: row.ID, OpID: row.OpID, Step: step, Outcome: outcome, Code: code, CreatedAt: e.now()})
}

func backoff(attempts int) *time.Duration {
	d := 15 * time.Second << min(attempts, 4) // 15s .. 4m
	return &d
}

// waiting records a retryable infrastructure problem (agent down) without failing the operation, until it has lasted
// MaxAttempts passes.
func (e *Engine) waiting(row *Installation, step, code string) *time.Duration {
	row.OpStep, row.ErrorCode = step, code
	row.OpAttempts++
	if row.OpAttempts >= e.Timing.MaxAttempts {
		return e.fail(row, step, code)
	}
	e.event(row, step, "waiting", code)
	_ = e.Repo.Save(row)
	return backoff(row.OpAttempts)
}

func (e *Engine) fail(row *Installation, step, code string) *time.Duration {
	row.State, row.OpStep, row.ErrorCode = StateFailed, step, code
	e.event(row, step, "failed", code)
	_ = e.Repo.Save(row)
	return nil
}

func agentFailureCode(err error) (code string, retryable bool) {
	if errors.Is(err, ErrAgentOffline) {
		return CodeAgentOffline, true
	}
	return CodeAgentRejected, false
}

func requestKey(gen int64) string { return fmt.Sprintf("install-%d", gen) }

func (e *Engine) install(ctx context.Context, row *Installation) *time.Duration {
	entry, err := e.entry(ctx, row.PluginID)
	switch {
	case errors.Is(err, ErrAgentDown):
		return e.waiting(row, "check", CodeAgentOffline)
	case err != nil:
		return e.fail(row, "check", CodePluginUnknown)
	}
	if k := missingRequired(entry, row.PublicConfig(), row.SecretKeyList()); k != "" {
		row.State, row.ErrorCode, row.OpID = StatePendingConfig, CodeConfigMissing, ""
		_ = e.Repo.Save(row)
		return nil
	}
	// Secrets live in the Agent, not here: confirm it still holds every key we recorded (a wiped Agent store must send
	// the user back to the form instead of starting a plugin without its token).
	have, err := e.Agent.SecretKeys(ctx, row.PluginID)
	if err != nil {
		code, retry := agentFailureCode(err)
		if retry {
			return e.waiting(row, "check", code)
		}
		return e.fail(row, "check", code)
	}
	held := map[string]bool{}
	for _, k := range have {
		held[k] = true
	}
	for _, k := range row.SecretKeyList() {
		if !held[k] {
			row.setSecretKeys(without(row.SecretKeyList(), k))
			row.State, row.ErrorCode, row.OpID = StatePendingConfig, CodeConfigMissing, ""
			_ = e.Repo.Save(row)
			return nil
		}
	}
	e.event(row, "check", "ok", "")

	// Identity: persist the next generation first, then issue it. A token that is lost between here and the Agent call
	// is never recovered; the next pass simply takes the following generation (the lower one is revoked by Issue).
	prev := row.Generation
	var token string
	for try := 0; try < 3; try++ {
		gen := row.Generation + 1
		row.Generation, row.OpStep = gen, "identity"
		if err := e.Repo.Save(row); err != nil {
			return backoff(1)
		}
		token, err = e.IDs.Issue(row.TenantID, row.PluginID, entry.ServiceIdentifier, gen, e.Timing.IdentityTTL, e.now())
		if !errors.Is(err, ErrGenerationNotHigher) {
			break
		}
	}
	if err != nil {
		return e.fail(row, "identity", CodeIdentityFailed)
	}
	e.event(row, "identity", "ok", "")
	gen := uint64(row.Generation)
	key := requestKey(row.Generation)
	undo := func() { _ = e.IDs.Revoke(row.TenantID, row.PluginID, e.now()) } // never leave a live identity no container holds

	steps := []struct {
		name string
		do   func() error
	}{
		{"agent_revoke_old", func() error {
			if prev == 0 {
				return nil
			}
			return ignoreConflict(e.Agent.Revoke(ctx, row.PluginID, uint64(prev), fmt.Sprintf("replaced by generation %d", gen)))
		}},
		{"agent_accept", func() error { return e.Agent.AcceptGeneration(ctx, row.PluginID, gen) }},
		{"agent_retire_old", func() error {
			if prev == 0 {
				return nil
			}
			return ignoreConflict(e.Agent.Retire(ctx, row.PluginID, uint64(prev)))
		}},
		{"agent_prepare", func() error {
			return e.Agent.Prepare(ctx, PrepareRequest{Instance: row.PluginID, Generation: gen, RequestKey: key, Plugin: row.PluginID,
				Values: row.PublicConfig(), IdentityToken: token, ServiceIdentifier: entry.ServiceIdentifier})
		}},
		{"agent_grant", func() error {
			return e.Agent.Grant(ctx, row.PluginID, gen, key, fmt.Sprintf("grant-%d", gen), e.Timing.GrantTTL)
		}},
		{"agent_activate", func() error { return e.Agent.Activate(ctx, row.PluginID, gen, key) }},
	}
	for _, s := range steps {
		if err := s.do(); err != nil {
			undo()
			code, retry := agentFailureCode(err)
			row.OpStep = s.name
			if retry {
				return e.waiting(row, s.name, code)
			}
			return e.fail(row, s.name, code)
		}
		row.OpStep = s.name
	}
	token = "" // the only copy is now inside the container
	e.event(row, "agent", "ok", "")

	if _, _, err := e.Services.Ensure(ctx, *entry); err != nil {
		return e.fail(row, "register", CodeRegisterFailed)
	}
	e.event(row, "register", "ok", "")

	if err := e.waitHealthy(ctx, *entry); err != nil {
		row.Health = "error"
		return e.fail(row, "health", CodeHealthFailed)
	}
	now := e.now()
	row.State, row.Health, row.OpStep, row.ErrorCode, row.OpAttempts, row.MaintainedAt = StateEnabled, "ok", "", "", 0, &now
	e.event(row, "health", "ok", "")
	_ = e.Repo.Save(row)
	return nil
}

func ignoreConflict(err error) error {
	if errors.Is(err, ErrAgentConflict) {
		return nil
	}
	return err
}

func (e *Engine) waitHealthy(ctx context.Context, entry CatalogEntry) error {
	deadline := e.now().Add(e.Timing.HealthWait)
	for {
		err := e.Health.Check(ctx, entry)
		if err == nil {
			return nil
		}
		if !e.now().Before(deadline) || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.Timing.HealthInterval):
		}
	}
}

// stop revokes the running generation (Agent first, then the platform identity) and, for an uninstall, removes the
// Agent-held site configuration. The generation counter and the service_plugins row stay: devices reference the
// latter, and a revoked generation must never be reused.
func (e *Engine) stop(ctx context.Context, row *Installation, final, step string) *time.Duration {
	if row.Generation > 0 {
		if err := ignoreConflict(e.Agent.Revoke(ctx, row.PluginID, uint64(row.Generation), step)); err != nil {
			code, retry := agentFailureCode(err)
			if retry {
				return e.waiting(row, "agent_revoke", code)
			}
			return e.fail(row, "agent_revoke", code)
		}
	}
	if err := e.IDs.Revoke(row.TenantID, row.PluginID, e.now()); err != nil {
		return e.fail(row, "identity_revoke", CodeIdentityFailed)
	}
	if final == StateUninstalled {
		if err := e.Agent.ClearConfig(ctx, row.PluginID); err != nil {
			code, retry := agentFailureCode(err)
			if retry {
				return e.waiting(row, "config_clear", code)
			}
			return e.fail(row, "config_clear", code)
		}
		row.ConfigPublic, row.SecretKeys = "{}", "[]"
	}
	row.State, row.Health, row.OpStep, row.ErrorCode, row.OpAttempts, row.MaintainedAt = final, "", "", "", 0, nil
	e.event(row, step, "ok", "")
	_ = e.Repo.Save(row)
	return nil
}

// maintain renews only the live identity and already-running Agent lease for the current generation. A renewal
// refusal never starts a new generation; only an explicit Retry operation may do that.
func (e *Engine) maintain(ctx context.Context, row *Installation) *time.Duration {
	entry, err := e.entry(ctx, row.PluginID)
	now := e.now()
	row.MaintainedAt = &now
	if err != nil {
		return e.maintainFailure(ctx, row, ErrAgentOffline)
	}
	gen := uint64(row.Generation)
	idErr := e.IDs.Renew(row.TenantID, row.PluginID, e.Timing.IdentityTTL, now)
	if errors.Is(idErr, ErrIdentityNotLive) {
		return e.maintainPermanentFailure(ctx, row, "identity_expired")
	}
	if idErr != nil {
		return e.maintainFailure(ctx, row, idErr)
	}
	grantErr := e.Agent.Renew(ctx, row.PluginID, gen, requestKey(row.Generation), fmt.Sprintf("renew-%d", now.Unix()), e.Timing.GrantTTL)
	if errors.Is(grantErr, ErrAgentConflict) || errors.Is(grantErr, ErrAgentRejected) {
		return e.maintainPermanentFailure(ctx, row, "renew_refused")
	}
	if grantErr != nil {
		return e.maintainFailure(ctx, row, grantErr)
	}
	if hErr := e.Health.Check(ctx, *entry); hErr == nil {
		row.Health, row.ErrorCode = "ok", ""
		row.OpAttempts, row.OpStep = 0, ""
	} else {
		row.Health = "error"
	}
	_ = e.Repo.Save(row)
	return nil
}

func (e *Engine) maintainFailure(ctx context.Context, row *Installation, _ error) *time.Duration {
	row.OpStep, row.ErrorCode, row.Health = "maintain", CodeRenewalFailed, "degraded"
	row.OpAttempts++
	max := e.Timing.MaxMaintainAttempts
	if max <= 0 {
		max = 5
	}
	if row.OpAttempts >= max {
		row.State, row.Health = StateFailed, "error"
		// Stop accepting traffic immediately when renewal is exhausted. If the host is offline, the
		// Agent's existing lease remains the bounded local stop condition.
		_ = e.IDs.Revoke(row.TenantID, row.PluginID, e.now())
		_ = e.Agent.Revoke(ctx, row.PluginID, uint64(row.Generation), "renewal attempts exhausted")
		e.event(row, "maintain", "failed", CodeRenewalFailed)
		_ = e.Repo.Save(row)
		return nil
	}
	e.event(row, "maintain", "waiting", CodeRenewalFailed)
	_ = e.Repo.Save(row)
	return backoff(row.OpAttempts - 1)
}

func (e *Engine) maintainPermanentFailure(ctx context.Context, row *Installation, reason string) *time.Duration {
	row.State, row.Health, row.OpStep, row.ErrorCode = StateFailed, "error", "maintain", CodeRenewalFailed
	// Prevent this generation from using an identity after the Agent has rejected its renewal.
	_ = e.IDs.Revoke(row.TenantID, row.PluginID, e.now())
	_ = e.Agent.Revoke(ctx, row.PluginID, uint64(row.Generation), reason)
	e.event(row, "maintain", "failed", CodeRenewalFailed)
	_ = e.Repo.Save(row)
	return nil
}

func without(list []string, drop string) []string {
	out := make([]string, 0, len(list))
	for _, k := range list {
		if k != drop {
			out = append(out, k)
		}
	}
	return out
}
