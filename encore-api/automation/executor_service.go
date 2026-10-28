package automation

import (
	"context"
	"errors"
	"strings"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/domain"
	"encore.app/automation/executor"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// RegisterExecutorRequest is the contract an external engine registers before it
// is allowed to produce anything.
type RegisterExecutorRequest struct {
	Name         string            `json:"name"`
	Contract     executor.Contract `json:"contract"`
	CredentialID string            `json:"credential_id,omitempty"`
}

type ExecutorResponse struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Engine       string            `json:"engine"`
	Contract     executor.Contract `json:"contract"`
	ContractHash string            `json:"contract_hash"`
	Epoch        int               `json:"epoch"`
	Enabled      bool              `json:"enabled"`
	LastSeq      int64             `json:"last_seq"`
	CredentialID string            `json:"credential_id,omitempty"`
	LastSeenAt   *time.Time        `json:"last_seen_at,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	// Summary is the one line the operator reads in the list.
	Summary string `json:"summary"`
	// Healthy is "seen within five minutes". It is computed, not stored, so a
	// silent executor becomes unhealthy without anyone writing to the row.
	Healthy bool `json:"healthy"`
}

type ListExecutorsRequest struct {
	Limit  int `json:"limit" query:"limit"`
	Offset int `json:"offset" query:"offset"`
}

type ListExecutorsResponse struct {
	Executors []ExecutorResponse `json:"executors"`
	Total     int                `json:"total"`
}

type HeartbeatRequest struct {
	Epoch int   `json:"epoch"`
	Seq   int64 `json:"seq"`
}

type HeartbeatResponse struct {
	ExecutorID string    `json:"executor_id"`
	Epoch      int       `json:"epoch"`
	LastSeq    int64     `json:"last_seq"`
	ServerTime time.Time `json:"server_time"`
}

// IntentRequest asks the platform to run a published rule revision on a device.
//
// It names a rule instead of carrying steps on purpose: a second, external
// execution path would have its own ordering, retries and idempotency, and then
// two paths would own the same device.
type IntentRequest struct {
	Epoch          int       `json:"epoch"`
	Seq            int64     `json:"seq"`
	ContractHash   string    `json:"contract_hash,omitempty"`
	IssuedAt       time.Time `json:"issued_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	IdempotencyKey string    `json:"idempotency_key"`
	CausalRoot     string    `json:"causal_root,omitempty"`
	RuleID         string    `json:"rule_id"`
	Revision       int       `json:"revision"`
	DeviceID       string    `json:"device_id"`
	Targets        []string  `json:"targets,omitempty"`
}

type IntentResponse struct {
	Accepted   bool             `json:"accepted"`
	ReasonCode string           `json:"reason_code,omitempty"`
	RunID      string           `json:"run_id,omitempty"`
	Status     string           `json:"status,omitempty"`
	Replayed   bool             `json:"replayed"`
	Issues     []executor.Issue `json:"issues,omitempty"`
}

// VerdictRequest is one alarm judgement from an external engine.
type VerdictRequest struct {
	Epoch          int        `json:"epoch"`
	Seq            int64      `json:"seq"`
	ContractHash   string     `json:"contract_hash,omitempty"`
	IssuedAt       time.Time  `json:"issued_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	IdempotencyKey string     `json:"idempotency_key"`
	CausalRoot     string     `json:"causal_root,omitempty"`
	PolicyID       string     `json:"policy_id"`
	PolicyRevision int        `json:"policy_revision"`
	DefinitionHash string     `json:"definition_hash,omitempty"`
	Mode           string     `json:"mode"`
	Observed       bool       `json:"observed"`
	Since          *time.Time `json:"since,omitempty"`
	SubjectID      string     `json:"subject_id"`
	Dimension      string     `json:"dimension,omitempty"`
	ActionKind     string     `json:"action_kind"`
}

type VerdictResponse struct {
	Accepted     bool             `json:"accepted"`
	ReasonCode   string           `json:"reason_code,omitempty"`
	Outcome      string           `json:"outcome,omitempty"`
	EpisodeID    string           `json:"episode_id,omitempty"`
	TransitionID string           `json:"transition_id,omitempty"`
	Replayed     bool             `json:"replayed"`
	Issues       []executor.Issue `json:"issues,omitempty"`
}

// ExecutorMessageResponse is one line of the message log: what the engine sent
// and what the platform decided about it.
type ExecutorMessageResponse struct {
	IdempotencyKey string    `json:"idempotency_key"`
	Direction      string    `json:"direction"`
	Epoch          int       `json:"epoch"`
	Seq            int64     `json:"seq"`
	Outcome        string    `json:"outcome"`
	ReasonCode     string    `json:"reason_code,omitempty"`
	Subject        string    `json:"subject,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type ListExecutorMessagesRequest struct {
	Limit  int `json:"limit" query:"limit"`
	Offset int `json:"offset" query:"offset"`
}

type ListExecutorMessagesResponse struct {
	Messages []ExecutorMessageResponse `json:"messages"`
	Total    int                       `json:"total"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// The two outcomes that mean "the platform refused the message before it
// consumed a sequence number". They are recorded so an operator can see what was
// refused, but they are deliberately *not* treated as a replay: a message that
// never advanced the sequence may legitimately be re-sent after the caller fixes
// it. Every other outcome means the message was admitted — including a run that
// the rule's own mode limits turned into a skipped run — and a retry must be
// answered with that outcome instead of being executed a second time.
const (
	outcomeDenied = "denied"
	outcomeFailed = "failed"
)

func toExecutorResponse(e *store.Executor, now time.Time) ExecutorResponse {
	healthy := false
	if e.LastSeenAt != nil {
		healthy = now.Sub(*e.LastSeenAt) <= 5*time.Minute
	}
	return ExecutorResponse{
		ID: e.ID, Name: e.Name, Engine: e.Engine, Contract: e.Contract,
		ContractHash: e.ContractHash, Epoch: e.Epoch, Enabled: e.Enabled,
		LastSeq: e.LastSeq, CredentialID: e.CredentialID, LastSeenAt: e.LastSeenAt,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
		Summary: e.Contract.Summary(), Healthy: healthy,
	}
}

// contractIssues turns the pure validator's findings into a client-visible
// rejection. The fields travel in `details` so a form or a CLI can point at the
// right input instead of printing one sentence for five problems.
func contractIssues(issues []executor.Issue) error {
	fields := make([]map[string]string, 0, len(issues))
	for _, issue := range issues {
		fields = append(fields, map[string]string{
			"field": issue.Field, "code": issue.Code, "message": issue.Message,
		})
	}
	message := "登记没有通过校验"
	if len(issues) > 0 {
		message = issues[0].Message
	}
	return detailError(errs.InvalidArgument, message, ErrorDetail{
		Code:   "executor_contract_invalid",
		Fields: fields,
	})
}

func (s *Service) executorByID(ctx context.Context, tenantID, id string) (*store.Executor, error) {
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	reg, err := rt.Store().GetExecutor(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, &errs.Error{Code: errs.NotFound, Message: "执行者不存在"}
		}
		return nil, internal(err)
	}
	return reg, nil
}

// ---------------------------------------------------------------------------
// Endpoints
// ---------------------------------------------------------------------------

// RegisterExecutor records a contract and issues the first epoch.
//
// The registration is what makes everything else checkable: the scope bounds the
// devices, the outputs bound the message kinds, and the epoch makes a later
// narrowing take effect immediately.
//
//encore:api auth method=POST path=/v1/executors
func (s *Service) RegisterExecutor(ctx context.Context, req *RegisterExecutorRequest) (*ExecutorResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, detailError(errs.InvalidArgument, "请填写执行者名称", ErrorDetail{
			Code: "required", Field: "name",
		})
	}
	if issues := req.Contract.Validate(); len(issues) > 0 {
		return nil, contractIssues(issues)
	}
	reg := &store.Executor{
		ID: runtime.NewID(), TenantID: tenantID, Name: strings.TrimSpace(req.Name),
		Engine: string(req.Contract.Engine), Contract: req.Contract,
		ContractHash: req.Contract.Hash(), Epoch: 1, Enabled: true,
		CredentialID: req.CredentialID,
	}
	if err := rt.Store().CreateExecutor(ctx, reg); err != nil {
		if isDuplicateKey(err) {
			return nil, detailError(errs.AlreadyExists, "已经有同名的执行者", ErrorDetail{
				Code: "duplicate_name", Field: "name",
			})
		}
		return nil, internal(err)
	}
	stored, err := rt.Store().GetExecutor(ctx, tenantID, reg.ID)
	if err != nil {
		return nil, internal(err)
	}
	out := toExecutorResponse(stored, rt.Now())
	return &out, nil
}

// ListExecutors shows what is registered, with its contract and liveness.
//
//encore:api auth method=GET path=/v1/executors
func (s *Service) ListExecutors(ctx context.Context, req *ListExecutorsRequest) (*ListExecutorsResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	rows, total, err := rt.Store().ListExecutors(ctx, tenantID, req.Limit, req.Offset)
	if err != nil {
		return nil, internal(err)
	}
	now := rt.Now()
	out := &ListExecutorsResponse{Executors: make([]ExecutorResponse, 0, len(rows)), Total: total}
	for _, row := range rows {
		out.Executors = append(out.Executors, toExecutorResponse(row, now))
	}
	return out, nil
}

// HeartbeatExecutor records liveness and returns the sequence the platform has
// accepted, so the executor can tell it is out of step instead of guessing.
//
//encore:api auth method=POST path=/v1/executors/:id/heartbeat
func (s *Service) HeartbeatExecutor(ctx context.Context, id string, req *HeartbeatRequest) (*HeartbeatResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	reg, err := s.executorByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if err := rt.Store().TouchExecutor(ctx, tenantID, id, rt.Now()); err != nil {
		return nil, internal(err)
	}
	return &HeartbeatResponse{
		ExecutorID: reg.ID, Epoch: reg.Epoch, LastSeq: reg.LastSeq, ServerTime: rt.Now(),
	}, nil
}

// ListExecutorMessages shows the message log of one executor: what it sent, and
// what the platform decided. This is the "输入输出证据" an operator needs when an
// engine claims it did something the platform has no record of.
//
//encore:api auth method=GET path=/v1/executors/:id/messages
func (s *Service) ListExecutorMessages(ctx context.Context, id string, req *ListExecutorMessagesRequest) (*ListExecutorMessagesResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if _, err := s.executorByID(ctx, tenantID, id); err != nil {
		return nil, err
	}
	rows, total, err := rt.Store().ListExecutorMessages(ctx, tenantID, id, req.Limit, req.Offset)
	if err != nil {
		return nil, internal(err)
	}
	out := &ListExecutorMessagesResponse{Messages: make([]ExecutorMessageResponse, 0, len(rows)), Total: total}
	for _, row := range rows {
		out.Messages = append(out.Messages, ExecutorMessageResponse{
			IdempotencyKey: row.IdempotencyKey, Direction: row.Direction,
			Epoch: row.Epoch, Seq: row.Seq, Outcome: row.Outcome,
			ReasonCode: row.ReasonCode, Subject: row.Subject, CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

// SubmitExecutorIntent accepts "run this rule revision on this device" from a
// registered executor.
//
// The order of the checks is deliberate. Cheap, local refusals come first
// (contract, epoch, sequence), then the idempotency lookup, and only then the
// rule and the device directory — so a replayed message costs one row lookup
// instead of a directory call.
//
//encore:api auth method=POST path=/v1/executors/:id/intents
func (s *Service) SubmitExecutorIntent(ctx context.Context, id string, req *IntentRequest) (*IntentResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	reg, err := s.executorByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	registration := executor.Registration{
		ID: reg.ID, Epoch: reg.Epoch, Contract: reg.Contract,
		ContractHash: reg.ContractHash, Enabled: reg.Enabled, LastSeq: reg.LastSeq,
	}
	envelope := executor.Envelope{
		ExecutorID: id, Epoch: req.Epoch, Seq: req.Seq, ContractHash: req.ContractHash,
		IssuedAt: req.IssuedAt, ExpiresAt: req.ExpiresAt,
		IdempotencyKey: req.IdempotencyKey, CausalRoot: req.CausalRoot,
	}

	// A replay of the same intent is answered, not re-executed. This is checked
	// before the sequence guard so a transport-level retry that kept its key does
	// not look like a replay attack.
	if prior, found := s.replayExecutorMessage(ctx, rt, tenantID, id, req.IdempotencyKey, "intent"); found {
		out := IntentResponse{Accepted: true, Replayed: true, RunID: prior.Subject, Status: prior.Outcome}
		return &out, nil
	}

	rule, ruleErr := rt.Store().GetRule(ctx, tenantID, req.RuleID)
	ruleRef := executor.RuleRef{}
	if ruleErr == nil {
		ruleRef = executor.RuleRef{ID: rule.ID, Revision: rule.Revision, Enabled: rule.Enabled}
	} else if !errors.Is(ruleErr, store.ErrNotFound) {
		return nil, internal(ruleErr)
	}

	var deviceConfigID string
	var labels []string
	if rt.Directory() != nil && req.DeviceID != "" {
		if devices, derr := rt.Directory().ByIDs(ctx, tenantID, []string{req.DeviceID}); derr == nil && len(devices) > 0 {
			deviceConfigID = devices[0].DeviceConfigID
			// The directory carries one label string per device, not a list. A
			// label scope therefore matches on that single value; the contract
			// validator already treats "no labels" as "not allowed".
			if label := strings.TrimSpace(devices[0].Label); label != "" {
				labels = []string{label}
			}
		}
	}

	intent := executor.Intent{
		Envelope: envelope, RuleID: req.RuleID, Revision: req.Revision,
		DeviceID: req.DeviceID, Targets: req.Targets, CausalRoot: req.CausalRoot,
	}
	if issues := intent.Validate(rt.Now(), registration, ruleRef, deviceConfigID, labels); len(issues) > 0 {
		s.recordExecutorMessage(ctx, rt, tenantID, id, req.IdempotencyKey, "intent", outcomeDenied, issues[0].Code, req.DeviceID, req.Epoch, req.Seq)
		out := IntentResponse{Accepted: false, ReasonCode: issues[0].Code, Issues: issues}
		return &out, nil
	}

	// The sequence guard is the database's decision, not ours: two messages with
	// the same number can arrive at once, and exactly one may proceed.
	advanced, err := rt.Store().AdvanceExecutorSeq(ctx, tenantID, id, req.Epoch, req.Seq)
	if err != nil {
		return nil, internal(err)
	}
	if !advanced {
		s.recordExecutorMessage(ctx, rt, tenantID, id, req.IdempotencyKey, "intent", outcomeDenied, "replayed_sequence", req.DeviceID, req.Epoch, req.Seq)
		return &IntentResponse{Accepted: false, ReasonCode: "replayed_sequence",
			Issues: []executor.Issue{{Field: "seq", Code: "replayed_sequence",
				Message: "序号没有前进；这条消息是重放或乱序，不会再次执行"}}}, nil
	}

	run, err := rt.RunNow(ctx, rule, req.DeviceID)
	if err != nil {
		// The intent was accepted and the sequence consumed; the run failed. Say
		// so instead of reporting a rejection, because retrying with a new key
		// would otherwise look like a fresh intent.
		s.recordExecutorMessage(ctx, rt, tenantID, id, req.IdempotencyKey, "intent", outcomeFailed, "run_failed", req.DeviceID, req.Epoch, req.Seq)
		return &IntentResponse{Accepted: true, ReasonCode: "run_failed",
			Issues: []executor.Issue{{Field: "rule_id", Code: "run_failed", Message: err.Error()}}}, nil
	}
	s.recordExecutorMessage(ctx, rt, tenantID, id, req.IdempotencyKey, "intent", run.Status, "", run.ID, req.Epoch, req.Seq)
	return &IntentResponse{Accepted: true, RunID: run.ID, Status: run.Status}, nil
}

// SubmitExecutorVerdict accepts an alarm judgement from a registered executor.
//
// The verdict is checked against the *published* policy — revision, definition
// hash, hold duration — before it reaches the alarm lifecycle. That is what stops
// an external job deployed against last week's threshold from raising an alarm
// today, and what stops a raw true/false from being read as "the condition held
// for five minutes".
//
//encore:api auth method=POST path=/v1/executors/:id/verdicts
func (s *Service) SubmitExecutorVerdict(ctx context.Context, id string, req *VerdictRequest) (*VerdictResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	reg, err := s.executorByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	registration := executor.Registration{
		ID: reg.ID, Epoch: reg.Epoch, Contract: reg.Contract,
		ContractHash: reg.ContractHash, Enabled: reg.Enabled, LastSeq: reg.LastSeq,
	}

	if prior, found := s.replayExecutorMessage(ctx, rt, tenantID, id, req.IdempotencyKey, "verdict"); found {
		return &VerdictResponse{Accepted: true, Replayed: true, Outcome: prior.Outcome}, nil
	}

	policyRef := executor.PolicyRef{}
	policy, perr := rt.PolicyRevision(ctx, tenantID, req.PolicyID)
	if perr == nil {
		spec := policy.Definition.Alarm
		policyRef = executor.PolicyRef{
			Revision:       policy.Revision,
			DefinitionHash: policy.DefinitionHash,
			ForSeconds:     policy.Definition.Trigger.For.Std().Seconds(),
			Enabled:        policy.Enabled,
			AllowsEvent:    spec != nil && spec.Allows(domain.AlarmSourceEvent),
			SourceAllowed:  spec != nil && spec.SourceAllowed(id),
		}
	} else if !errors.Is(perr, store.ErrNotFound) {
		return nil, internal(perr)
	}

	verdict := executor.Verdict{
		Envelope: executor.Envelope{
			ExecutorID: id, Epoch: req.Epoch, Seq: req.Seq, ContractHash: req.ContractHash,
			IssuedAt: req.IssuedAt, ExpiresAt: req.ExpiresAt,
			IdempotencyKey: req.IdempotencyKey, CausalRoot: req.CausalRoot,
		},
		PolicyID: req.PolicyID, PolicyRevision: req.PolicyRevision,
		DefinitionHash: req.DefinitionHash, Mode: executor.VerdictMode(req.Mode),
		Observed: req.Observed, Since: req.Since, SubjectID: req.SubjectID,
		Dimension: req.Dimension, ActionKind: req.ActionKind,
	}
	if issues := verdict.Validate(rt.Now(), registration, policyRef); len(issues) > 0 {
		s.recordExecutorMessage(ctx, rt, tenantID, id, req.IdempotencyKey, "verdict", outcomeDenied, issues[0].Code, req.SubjectID, req.Epoch, req.Seq)
		return &VerdictResponse{Accepted: false, ReasonCode: issues[0].Code, Issues: issues}, nil
	}

	advanced, err := rt.Store().AdvanceExecutorSeq(ctx, tenantID, id, req.Epoch, req.Seq)
	if err != nil {
		return nil, internal(err)
	}
	if !advanced {
		return &VerdictResponse{Accepted: false, ReasonCode: "replayed_sequence",
			Issues: []executor.Issue{{Field: "seq", Code: "replayed_sequence",
				Message: "序号没有前进；这条判定是重放或乱序"}}}, nil
	}

	decision, err := rt.SubmitAlarmEvent(ctx, tenantID, req.PolicyID, runtime.AlarmEventRequest{
		ActionID: req.IdempotencyKey, Source: id, Kind: req.ActionKind,
		SubjectID: req.SubjectID, Dimension: req.Dimension,
		Payload: map[string]any{
			"observed": req.Observed, "mode": req.Mode, "since": req.Since,
			"causal_root": req.CausalRoot, "executor_epoch": req.Epoch, "executor_seq": req.Seq,
		},
	})
	if err != nil {
		return nil, internal(err)
	}
	s.recordExecutorMessage(ctx, rt, tenantID, id, req.IdempotencyKey, "verdict", decision.Outcome, decision.ReasonCode, req.SubjectID, req.Epoch, req.Seq)
	return &VerdictResponse{
		Accepted: true, Outcome: decision.Outcome, ReasonCode: decision.ReasonCode,
		EpisodeID: decision.EpisodeID, TransitionID: decision.TransitionID,
		Replayed: decision.Replayed,
	}, nil
}

// isDuplicateKey reports whether an insert lost to the unique index on
// (tenant_id, name). Postgres says 23505; matching on the SQLSTATE avoids
// depending on the driver's error type.
func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "23505") || strings.Contains(text, "duplicate key")
}

func (s *Service) replayExecutorMessage(ctx context.Context, rt *runtime.Runtime, tenantID, executorID, key, direction string) (*store.ExecutorMessage, bool) {
	if strings.TrimSpace(key) == "" {
		return nil, false
	}
	prior, err := rt.Store().LookupExecutorMessage(ctx, tenantID, executorID, key)
	if err != nil || prior.Direction != direction || prior.Outcome == outcomeDenied || prior.Outcome == outcomeFailed {
		return nil, false
	}
	return prior, true
}

func (s *Service) recordExecutorMessage(ctx context.Context, rt *runtime.Runtime, tenantID, executorID, key, direction, outcome, reason, subject string, epoch int, seq int64) {
	if strings.TrimSpace(key) == "" {
		return
	}
	_, _ = rt.Store().RecordExecutorMessage(ctx, &store.ExecutorMessage{
		TenantID: tenantID, ExecutorID: executorID, IdempotencyKey: key,
		Direction: direction, Epoch: epoch, Seq: seq, Outcome: outcome,
		ReasonCode: reason, Subject: subject,
	})
}
