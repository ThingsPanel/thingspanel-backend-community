package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/domain"
	"encore.app/automation/legacy"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A31: resumable migration, reconciliation and rollback
// ---------------------------------------------------------------------------
//
// The shape of this feature follows one rule: a migration is a long operation
// across two databases, so it cannot be one transaction. It is a run with items,
// each item has a status, and every endpoint is safe to call again. "It failed
// halfway" is a resumable state, not a reason to start over.
//
// Three things are deliberately *not* here:
//
//   - No re-evaluation. Applying an item writes exactly the plan that was
//     approved. It does not re-read the legacy database, so a scene edited in
//     between cannot change under the approval.
//   - No counting by subtraction. Applied, failed and unresolved are recomputed
//     from the item rows, so an interrupted run reports what actually happened
//     instead of what it meant to do.
//   - No silent overwrite. A rule that already carries the old id and differs is
//     reported, never replaced.

// migrationBatchDefault and migrationBatchMax bound one apply call.
//
// A migration over 10 000 scenes must not be one HTTP request that either
// finishes or leaves nothing behind. Bounded batches make progress visible and
// let the operator stop between them.
const (
	migrationBatchDefault = 50
	migrationBatchMax     = 200
)

// CreateMigration plans a run from a snapshot of the old rows.
//
// Dry run is the default: a plan nobody read is not an approval. Apply=true runs
// the first batch immediately, which is what the CLI uses after a human has
// looked at the dry run in the same command.
//
//encore:api auth method=POST path=/v1/migrations
func (s *Service) CreateMigration(ctx context.Context, req *MigrationCreateRequest) (*MigrationResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "请求体不能为空"}
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, detailError(errs.InvalidArgument, "迁移名称不能为空",
			ErrorDetail{Code: "migration_name_required", Field: "name"})
	}

	snap := s.legacySnapshotFor(ctx, tenantID, rt, req)
	id := runtime.NewID()
	items, err := planMigration(id, req, snap, func(legacyID string) (*store.Rule, error) {
		rule, gerr := rt.Store().GetRuleByLegacyID(ctx, tenantID, legacyID)
		if gerr != nil {
			if errors.Is(gerr, store.ErrNotFound) {
				return nil, nil
			}
			return nil, gerr
		}
		if rule.Kind != domain.KindAlarm {
			// The old id resolves to something that is not an alarm policy. That
			// is not "the policy is missing", it is "the mapping is wrong", and
			// adopting an alarm under a scene's key would be worse than refusing.
			return nil, nil
		}
		return rule, nil
	})
	if err != nil {
		return nil, detailError(errs.InvalidArgument, err.Error(),
			ErrorDetail{Code: "migration_snapshot_invalid"})
	}

	m := &store.Migration{
		ID: id, TenantID: tenantID, Name: req.Name,
		Status: store.MigrationStatusPlanned, SnapshotHash: snapshotHash(req),
	}
	if err := rt.Store().CreateMigration(ctx, m, items); err != nil {
		return nil, internal(err)
	}
	if req.Apply {
		return s.applyBatches(ctx, tenantID, rt, id, migrationBatchDefault)
	}
	return s.migrationResponse(ctx, tenantID, rt, id)
}

// ApplyMigration runs the next batch of a run.
//
// Calling it again after a crash, a timeout or a partial failure is the recovery
// step. Items that already succeeded are not returned by the batch query, so a
// second call cannot create a second rule for the same old id.
//
//encore:api auth method=POST path=/v1/migrations/:id/apply
func (s *Service) ApplyMigration(ctx context.Context, id string, req *MigrationApplyRequest) (*MigrationResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	limit := migrationBatchDefault
	if req != nil && req.Limit > 0 {
		limit = req.Limit
	}
	if limit > migrationBatchMax {
		limit = migrationBatchMax
	}
	m, err := rt.Store().GetMigration(ctx, tenantID, id)
	if err != nil {
		return nil, migrationNotFound(err)
	}
	if m.Status == store.MigrationStatusRolledBack {
		return nil, detailError(errs.FailedPrecondition,
			"这次迁移已经回退，不能再继续应用；需要重新建一次迁移",
			ErrorDetail{Code: "migration_rolled_back", Status: m.Status})
	}
	return s.applyBatches(ctx, tenantID, rt, id, limit)
}

// GetMigration reads a run and its items.
//
//encore:api auth method=GET path=/v1/migrations/:id
func (s *Service) GetMigration(ctx context.Context, id string) (*MigrationResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if _, err := rt.Store().GetMigration(ctx, tenantID, id); err != nil {
		return nil, migrationNotFound(err)
	}
	return s.migrationResponse(ctx, tenantID, rt, id)
}

// VerifyMigration reconciles the new state with the approved snapshot.
//
// It reports; it never repairs. "Clean up my data" is not something that should
// happen because somebody opened a page, and a verify that silently deleted an
// orphan would destroy the evidence of the defect that produced it.
//
//encore:api auth method=POST path=/v1/migrations/:id/verify
func (s *Service) VerifyMigration(ctx context.Context, id string) (*MigrationVerifyResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if _, err := rt.Store().GetMigration(ctx, tenantID, id); err != nil {
		return nil, migrationNotFound(err)
	}
	items, err := rt.Store().ListMigrationItems(ctx, id, "", 500)
	if err != nil {
		return nil, internal(err)
	}

	out := &MigrationVerifyResponse{
		Missing: []string{}, DisabledMismatch: []string{}, Changed: []string{},
		Orphans: []string{}, Unresolved: []string{},
	}
	claimed := map[string]string{} // legacy id -> rule id written by this run
	levelMismatch := 0

	for _, it := range items {
		if it.Status == store.MigrationUnsupported || it.Status == store.MigrationAmbiguous {
			// Unresolved items are listed separately and keep the run from being
			// "complete". They are exactly the objects a customer will miss.
			out.Unresolved = append(out.Unresolved, it.LegacyID)
			continue
		}
		// A skipped item still points at what this run owns. "Already migrated by
		// an earlier run" and "the alarm was already open" are successes, and
		// leaving them out of the claim set made every second run report its
		// predecessor's rules as orphans — the check cried wolf exactly when the
		// operator was re-running to confirm the first run was sound.
		if it.Status != store.MigrationApplied && it.Status != store.MigrationSkipped {
			continue
		}

		switch it.Kind {
		case store.MigrationKindAlarm:
			if it.EpisodeID == "" {
				// A skipped alarm with no episode is "already recovered, nothing
				// to adopt" — not something missing.
				if it.Status == store.MigrationApplied {
					out.Missing = append(out.Missing, it.LegacyID)
				}
				continue
			}
			ep, gerr := rt.Store().EpisodeByID(ctx, tenantID, it.EpisodeID)
			if gerr != nil {
				if errors.Is(gerr, store.ErrNotFound) {
					out.Missing = append(out.Missing, it.LegacyID)
					continue
				}
				return nil, internal(gerr)
			}
			if ep.State != "active" {
				// It was cleared after the import. That is legitimate — the
				// condition may have recovered — so it is reported, not failed.
				out.Changed = append(out.Changed, it.LegacyID)
			}
			if it.Alarm != nil && it.Alarm.LegacyLevel != "" &&
				ep.Level != "" && !strings.EqualFold(it.Alarm.LegacyLevel, ep.Level) {
				levelMismatch++
			}
		default:
			if it.RuleID == "" {
				// Same distinction: a skipped row that never got a rule (a
				// disabled alarm configuration) is a decision, not a gap.
				if it.Status == store.MigrationApplied {
					out.Missing = append(out.Missing, it.LegacyID)
				}
				continue
			}
			rule, gerr := rt.Store().GetRule(ctx, tenantID, it.RuleID)
			if gerr != nil {
				if errors.Is(gerr, store.ErrNotFound) {
					out.Missing = append(out.Missing, it.LegacyID)
					continue
				}
				return nil, internal(gerr)
			}
			claimed[it.LegacyID] = rule.ID
			// The switch is compared with the one the snapshot asked for, not
			// with "on". A scene always asks for on (the old scene table has no
			// switch), so a disabled migrated scene is still reported; an
			// automation that was off in the old console and arrived off is not,
			// because reporting it would teach the operator to ignore this list.
			if rule.Enabled != it.RuleEnabled() {
				out.DisabledMismatch = append(out.DisabledMismatch, it.LegacyID)
			}
			if it.HasDefinition && rule.DefinitionHash != domain.DefinitionHash(it.Definition) {
				// Edited after the migration. Allowed — a customer may tune a
				// migrated scene — so it is reported, not treated as an error.
				out.Changed = append(out.Changed, it.LegacyID)
			}
		}
	}

	// Orphans: a live rule carrying a legacy id from this run that no applied
	// item claims. That means the mapping was written twice, and the customer now
	// has two rules where the old console had one.
	legacyIDs := make([]string, 0, len(items))
	for _, it := range items {
		if it.Kind == store.MigrationKindAlarm {
			continue
		}
		// Every non-alarm kind creates a rule, so every one of them can leave an
		// orphan behind. Listing only scenes and automations here would hide a
		// duplicated alarm policy.
		legacyIDs = append(legacyIDs, it.LegacyID)
	}
	rules, err := rt.Store().RulesByLegacyIDs(ctx, tenantID, legacyIDs)
	if err != nil {
		return nil, internal(err)
	}
	for _, r := range rules {
		if claimed[r.LegacyID] == r.ID {
			continue
		}
		out.Orphans = append(out.Orphans, r.LegacyID)
	}

	out.DuplicateActiveAlarms, err = rt.Store().CountDuplicateActiveAlarms(ctx, tenantID)
	if err != nil {
		return nil, internal(err)
	}

	out.Complete = len(out.Missing) == 0 && len(out.DisabledMismatch) == 0 &&
		len(out.Orphans) == 0 && out.DuplicateActiveAlarms == 0 &&
		len(out.Unresolved) == 0

	out.Notes = verifyNotes(out, len(items), levelMismatch)
	return out, nil
}

// RollbackMigration undoes a run, within the boundary the task book draws.
//
// A rollback deletes a rule; it cannot un-send a command. So a rule that already
// ran is kept by default, because it is the only record that something was sent
// to a device. Force removes it anyway and says so. Accepted or unknown commands
// and notifications keep their original owner either way — this endpoint never
// touches the outbox.
//
//encore:api auth method=POST path=/v1/migrations/:id/rollback
func (s *Service) RollbackMigration(ctx context.Context, id string, req *MigrationRollbackRequest) (*MigrationResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if _, err := rt.Store().GetMigration(ctx, tenantID, id); err != nil {
		return nil, migrationNotFound(err)
	}
	force := req != nil && req.Force
	limit := migrationBatchDefault
	if req != nil && req.Limit > 0 {
		limit = req.Limit
	}
	if limit > migrationBatchMax {
		limit = migrationBatchMax
	}

	items, err := rt.Store().ListMigrationItems(ctx, id, store.MigrationApplied, limit)
	if err != nil {
		return nil, internal(err)
	}
	for _, it := range items {
		switch it.Kind {
		case store.MigrationKindAlarm:
			s.rollbackAlarm(ctx, tenantID, rt, id, it, force)
		default:
			s.rollbackRule(ctx, tenantID, rt, id, it, force)
		}
	}

	if pending, err := rt.Store().PendingMigrationCount(ctx, id); err == nil && pending == 0 {
		_ = rt.Store().SetMigrationStatus(ctx, id, store.MigrationStatusRolledBack)
	}
	return s.migrationResponse(ctx, tenantID, rt, id)
}

func (s *Service) rollbackRule(ctx context.Context, tenantID string, rt *runtime.Runtime,
	migrationID string, it *store.MigrationItem, force bool) {

	if it.RuleID == "" {
		_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
			store.MigrationRolledBack, "", "", "")
		return
	}
	runs, err := rt.Store().RuleRunCount(ctx, tenantID, it.RuleID)
	if err != nil {
		_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "rollback_failed", err.Error())
		return
	}
	if runs > 0 && !force {
		// The rule is the only record that a command was sent to a device.
		// Deleting it would leave the device in a state nothing explains.
		_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
			store.MigrationKept, it.RuleID, "rule_already_ran",
			fmt.Sprintf("这条规则已经执行过 %d 次，默认不删除；确认设备状态后可用 force 强制删除", runs))
		return
	}
	if err := rt.Store().DeleteRule(ctx, tenantID, it.RuleID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
				store.MigrationRolledBack, it.RuleID, "", "规则已不存在")
			return
		}
		_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, it.RuleID, "rollback_failed", err.Error())
		return
	}
	_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
		store.MigrationRolledBack, it.RuleID, "", "已删除迁移时创建的规则")
}

func (s *Service) rollbackAlarm(ctx context.Context, tenantID string, rt *runtime.Runtime,
	migrationID string, it *store.MigrationItem, force bool) {

	if it.EpisodeID == "" {
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationRolledBack, "", "", "")
		return
	}
	ep, err := rt.Store().EpisodeByID(ctx, tenantID, it.EpisodeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
				store.MigrationRolledBack, it.EpisodeID, "", "告警记录已不存在")
			return
		}
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "rollback_failed", err.Error())
		return
	}
	if ep.State != "active" {
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationRolledBack, it.EpisodeID, "", "告警已结束，无需回退")
		return
	}
	if ep.AckedAt != nil && !force {
		// Somebody has already looked at this alarm. Taking it away silently
		// would make the operator's acknowledgement disappear.
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationKept, it.EpisodeID, "alarm_already_acknowledged",
			"这条告警已被确认过，默认不撤回；确认后可用 force 强制撤回")
		return
	}

	seq, serr := rt.Store().NextSeq(ctx)
	if serr != nil {
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "rollback_failed", serr.Error())
		return
	}
	// The fact is written before the state change, so a crash between the two
	// leaves "a rollback was recorded" rather than "the alarm vanished".
	if _, terr := rt.Store().AddTransition(ctx, nil, &store.Transition{
		ID: runtime.NewID(), EpisodeID: ep.ID, TenantID: tenantID,
		Kind: runtime.TransitionRollback, Seq: seq, Revision: ep.Revision,
		Epoch: ep.Epoch, Evidence: map[string]any{"reason": "migration_rollback"},
	}); terr != nil {
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "rollback_failed", terr.Error())
		return
	}
	if _, cerr := rt.Store().ClearEpisode(ctx, nil, ep.ID, seq, ep.Revision); cerr != nil {
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "rollback_failed", cerr.Error())
		return
	}
	_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
		store.MigrationRolledBack, it.EpisodeID, "", "已撤回迁移时领养的告警")
}

// ---------------------------------------------------------------------------
// internals
// ---------------------------------------------------------------------------

// applyBatches runs batches until the run has nothing left or the budget is spent.
func (s *Service) applyBatches(ctx context.Context, tenantID string, rt *runtime.Runtime,
	id string, limit int) (*MigrationResponse, error) {

	_ = rt.Store().SetMigrationStatus(ctx, id, store.MigrationStatusApplying)

	processed := 0
	for processed < limit {
		batch, err := rt.Store().NextMigrationBatch(ctx, id, limit-processed)
		if err != nil {
			return nil, internal(err)
		}
		if len(batch) == 0 {
			break
		}
		for _, it := range batch {
			s.applyItem(ctx, tenantID, rt, id, it)
			processed++
		}
	}

	// Counters are recomputed from the item rows, never incremented, so an
	// interrupted batch leaves numbers that match what actually happened.
	if _, err := rt.Store().RecomputeMigrationCounts(ctx, id); err != nil {
		return nil, internal(err)
	}
	pending, err := rt.Store().PendingMigrationCount(ctx, id)
	if err != nil {
		return nil, internal(err)
	}
	// "Applied" only when nothing is left. A run that stopped after 50 of 300
	// items and called itself applied would be lying about the customer's data.
	switch {
	case pending == 0:
		_ = rt.Store().SetMigrationStatus(ctx, id, store.MigrationStatusApplied)
	default:
		_ = rt.Store().SetMigrationStatus(ctx, id, store.MigrationStatusApplying)
	}
	return s.migrationResponse(ctx, tenantID, rt, id)
}

// applyItem applies one item and records what actually happened.
func (s *Service) applyItem(ctx context.Context, tenantID string, rt *runtime.Runtime,
	migrationID string, it *store.MigrationItem) {

	switch it.Kind {
	case store.MigrationKindAlarm:
		s.applyAlarmItem(ctx, tenantID, rt, migrationID, it)
	default:
		// Scenes, automations and alarm configurations all become a rule. They
		// differ in how the definition was produced, not in how it is stored, so
		// there is one path rather than three that drift apart.
		s.applySceneItem(ctx, tenantID, rt, migrationID, it)
	}
}

func (s *Service) applySceneItem(ctx context.Context, tenantID string, rt *runtime.Runtime,
	migrationID string, it *store.MigrationItem) {

	if !it.HasDefinition {
		_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "definition_missing", "这条记录没有可应用的定义")
		return
	}
	existing, err := rt.Store().FindRulesByLegacyID(ctx, tenantID, it.LegacyID, 5)
	if err != nil {
		_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "lookup_failed", err.Error())
		return
	}
	hash := domain.DefinitionHash(it.Definition)
	switch len(existing) {
	case 0:
		// create below
	case 1:
		if existing[0].DefinitionHash == hash {
			// Already migrated by an earlier run. This is what makes the second
			// apply a no-op instead of a duplicate.
			_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
				store.MigrationSkipped, existing[0].ID, "already_migrated",
				"已经迁移过，定义一致，未重复创建")
			return
		}
		_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, existing[0].ID, "existing_rule_differs",
			"已经有一条规则占用这个旧 id，但定义不同；不覆盖，请人工确认后删除其中一条")
		return
	default:
		_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
			store.MigrationAmbiguous, "", "legacy_id_ambiguous",
			fmt.Sprintf("有 %d 条规则都占用这个旧 id，无法确定该更新哪一条", len(existing)))
		return
	}

	name := it.Name
	if strings.TrimSpace(name) == "" {
		name = "迁移-" + it.LegacyID
	}
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: tenantID, Name: name,
		Kind: it.Definition.Kind, Enabled: it.RuleEnabled(),
		Definition: it.Definition, DefinitionHash: hash,
		LegacyID: it.LegacyID,
	}
	if err := rt.Store().CreateRule(ctx, rule); err != nil {
		_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "create_failed", err.Error())
		return
	}
	// The plan may have written a note that outlives the apply — "the old
	// notification group is not bound", for instance. Replacing it with "created"
	// would throw away the one thing the operator has to act on.
	note := "已创建规则"
	if r := strings.TrimSpace(it.Reason); r != "" {
		note = "已创建规则；" + r
	}
	_ = rt.Store().SetMigrationItemStatus(ctx, migrationID, it.LegacyID,
		store.MigrationApplied, rule.ID, "", note)
}

func (s *Service) applyAlarmItem(ctx context.Context, tenantID string, rt *runtime.Runtime,
	migrationID string, it *store.MigrationItem) {

	if it.Alarm == nil {
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "payload_missing", "这条告警没有可领养的内容")
		return
	}
	a := it.Alarm

	// The policy must still exist. If it was deleted between plan and apply,
	// opening the episode would create an alarm that points at nothing.
	policy, err := rt.Store().GetRule(ctx, tenantID, a.PolicyID)
	if err != nil {
		code, msg := "policy_missing", "迁移计划里的告警策略已不存在，未导入"
		if !errors.Is(err, store.ErrNotFound) {
			code, msg = "lookup_failed", err.Error()
		}
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", code, msg)
		return
	}

	// Already active for the same identity: do not open a second one. This is the
	// "no duplicate active alarms" guarantee, made visible instead of implied by
	// an index violation.
	//
	// A lookup error is not "there is no active alarm". Treating the two the same
	// would open a second episode exactly when the database is struggling, which
	// is the worst possible moment to guess.
	existing, gerr := rt.Store().GetActiveEpisode(ctx, tenantID, policy.PolicyKey(), a.Subject, a.Dimension)
	switch {
	case gerr == nil && existing != nil:
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationSkipped, existing.ID, "alarm_already_active",
			"这台设备这个指标已经有一条活动告警，未重复打开")
		return
	case gerr != nil && !errors.Is(gerr, store.ErrNotFound):
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "lookup_failed", gerr.Error())
		return
	}

	openedAt := time.Now().UTC()
	if a.OpenedAt != "" {
		if t, perr := time.Parse(time.RFC3339, a.OpenedAt); perr == nil {
			openedAt = t.UTC()
		}
	}
	seq, serr := rt.Store().NextSeq(ctx)
	if serr != nil {
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "sequence_failed", serr.Error())
		return
	}

	ep := &store.Episode{
		ID: runtime.NewID(), TenantID: tenantID,
		PolicyID: policy.ID, PolicyKey: policy.PolicyKey(),
		SubjectID: a.Subject, Dimension: a.Dimension, State: "active",
		Level: policy.AlarmLevel(), Source: "legacy_import",
		Revision: policy.Revision, Epoch: int64(policy.Revision),
		OpenedAt: openedAt, FirstSeq: seq, LastSeq: seq,
	}
	if err := rt.Store().OpenEpisode(ctx, ep); err != nil {
		// A concurrent apply of the same run, or the detector firing at the same
		// moment. The partial unique index guarantees one active alarm, so this is
		// a "somebody got there first", not a failure.
		if existing, gerr := rt.Store().GetActiveEpisode(ctx, tenantID, policy.PolicyKey(), a.Subject, a.Dimension); gerr == nil && existing != nil {
			_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
				store.MigrationSkipped, existing.ID, "alarm_already_active",
				"这台设备这个指标已经有一条活动告警，未重复打开")
			return
		}
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, "", "open_episode_failed", err.Error())
		return
	}

	// The history row is written after the episode. Adopting deliberately does
	// not enqueue a notification: the customer was already told by the old
	// engine, and re-sending is the duplicate the task book forbids.
	if _, terr := rt.Store().AddTransition(ctx, nil, &store.Transition{
		ID: runtime.NewID(), EpisodeID: ep.ID, TenantID: tenantID,
		Kind: runtime.TransitionImported, Seq: seq, Revision: policy.Revision,
		Epoch: ep.Epoch,
		Evidence: map[string]any{
			"legacy_alarm_id": a.PolicyLegacyID + "/" + it.LegacyID,
			"legacy_level":    a.LegacyLevel,
			"legacy_status":   a.LegacyStatus,
			"opened_at":       openedAt.Format(time.RFC3339),
			"note":            "从旧系统领养，未重复发送通知",
		},
	}); terr != nil {
		_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
			store.MigrationFailed, ep.ID, "transition_failed", terr.Error())
		return
	}
	_ = rt.Store().SetMigrationAlarmOutcome(ctx, migrationID, it.LegacyID,
		store.MigrationApplied, ep.ID, "", "已领养为活动告警")
}

// migrationResponse assembles the run and its items.
func (s *Service) migrationResponse(ctx context.Context, tenantID string, rt *runtime.Runtime,
	id string) (*MigrationResponse, error) {

	m, err := rt.Store().GetMigration(ctx, tenantID, id)
	if err != nil {
		return nil, migrationNotFound(err)
	}
	items, err := rt.Store().ListMigrationItems(ctx, id, "", 500)
	if err != nil {
		return nil, internal(err)
	}
	out := &MigrationResponse{
		ID: m.ID, Name: m.Name, Status: m.Status, SnapshotHash: m.SnapshotHash,
		Total: m.Total, Applied: m.Applied, Skipped: m.Skipped,
		Failed: m.Failed, Unresolved: m.Unresolved,
		// Never nil: a page that does v-for over items must not receive null.
		Items:     make([]MigrationItemResponse, 0, len(items)),
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: m.UpdatedAt.UTC().Format(time.RFC3339),
	}
	for _, it := range items {
		out.Items = append(out.Items, MigrationItemResponse{
			LegacyID: it.LegacyID, Kind: it.Kind, Name: it.Name, Status: it.Status,
			RuleID: it.RuleID, EpisodeID: it.EpisodeID,
			ReasonCode: it.ReasonCode, Reason: it.Reason,
		})
	}
	out.Notes = runNotes(m, items)
	return out, nil
}

// runNotes says the things a count cannot say.
func runNotes(m *store.Migration, items []*store.MigrationItem) []string {
	var notes []string
	if m.Total == 0 {
		notes = append(notes, "快照里没有任何旧对象；如果这不是预期，检查导出旧数据的那一步")
	}
	if m.Unresolved > 0 {
		notes = append(notes, fmt.Sprintf(
			"有 %d 条旧配置无法自动迁移，它们在 items 里是 unsupported/ambiguous，不计入已迁移数",
			m.Unresolved))
	}
	if m.Failed > 0 {
		notes = append(notes, fmt.Sprintf(
			"有 %d 条应用失败，再次调用 apply 会重试；反复失败的说明需要人工处理", m.Failed))
	}
	if m.Status == store.MigrationStatusApplying {
		notes = append(notes, "这次迁移还没有应用完，再次调用 apply 继续")
	}
	automations := 0
	unresolvedAutomations := 0
	for _, it := range items {
		if it.Kind == store.MigrationKindAutomation {
			automations++
			if it.Status == store.MigrationUnsupported || it.Status == store.MigrationAmbiguous {
				unresolvedAutomations++
			}
		}
	}
	if automations > 0 {
		notes = append(notes, fmt.Sprintf(
			"快照里有 %d 条联动，其中 %d 条已转换、%d 条无法自动转换（原因逐条写在 items 里）",
			automations, automations-unresolvedAutomations, unresolvedAutomations))
	}
	return notes
}

func verifyNotes(out *MigrationVerifyResponse, total, levelMismatch int) []string {
	var notes []string
	if out.Complete {
		notes = append(notes, fmt.Sprintf("对账通过：%d 条旧对象与当前状态一致", total))
	}
	if len(out.Changed) > 0 {
		notes = append(notes, fmt.Sprintf(
			"有 %d 条迁移后被改过；这是允许的，仅作提示，不影响对账结果", len(out.Changed)))
	}
	if len(out.Unresolved) > 0 {
		notes = append(notes, fmt.Sprintf(
			"有 %d 条没有迁移结果，必须先给出处理方式，否则不能算迁移完成", len(out.Unresolved)))
	}
	if levelMismatch > 0 {
		notes = append(notes, fmt.Sprintf(
			"有 %d 条告警的旧等级与新策略等级不同，显示按新策略等级；旧等级保留在告警历史里",
			levelMismatch))
	}
	if out.DuplicateActiveAlarms > 0 {
		notes = append(notes, "同一设备同一指标出现了多条活动告警；这说明唯一索引没有生效，必须立刻处理")
	}
	return notes
}

func migrationNotFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return detailError(errs.NotFound, "没有这次迁移记录", ErrorDetail{Code: "migration_missing"})
	}
	return internal(err)
}

// legacySnapshotFor builds the resolver the legacy planner reads.
//
// The split is the same one A13 established: device and scene existence come from
// the snapshot the caller supplied, because only the caller can read the legacy
// database; the alarm policy mapping comes from here, because that is where the
// policy now lives. Neither side guesses about the other.
func (s *Service) legacySnapshotFor(ctx context.Context, tenantID string, rt *runtime.Runtime,
	req *MigrationCreateRequest) *legacySnapshot {

	snap := &legacySnapshot{
		scenes:  map[string]*legacy.Scene{},
		devices: map[string]bool{},
		configs: map[string]bool{},
	}
	for i := range req.Scenes {
		sc := toLegacyScene(&req.Scenes[i])
		if sc.ID != "" {
			snap.scenes[sc.ID] = sc
		}
	}
	for _, d := range req.Devices {
		snap.devices[d] = true
	}
	for _, c := range req.DeviceConfigs {
		snap.configs[c] = true
	}
	snap.resolve = func(legacyAlarmID string) (string, string, bool, error) {
		rule, err := rt.Store().GetRuleByLegacyID(ctx, tenantID, legacyAlarmID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return "", "", false, nil
			}
			return "", "", false, err
		}
		if rule.Kind != domain.KindAlarm {
			return "", "", false, nil
		}
		return rule.ID, rule.PolicyKey(), true, nil
	}
	return snap
}
