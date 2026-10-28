package brokercontrol

// This is the administrative state machine in the existing route owner.
// Callers are trusted runtime/operator code, never devices or public API input.
// Evidence hashes attest external operations; they do not themselves establish
// that a broker/legacy process was fenced. Runtime integration is mandatory.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var (
	ErrMigrationInvalid  = errors.New("brokercontrol: invalid migration request")
	ErrBarrierIncomplete = errors.New("brokercontrol: fence barrier incomplete or membership changed")
	ErrTargetUnproven    = errors.New("brokercontrol: target access has not been proven")
)

type routeOperationAudit struct {
	operation  string
	actor      string
	planSHA256 string
}

type MigrationStore struct {
	db    *gorm.DB
	audit *routeOperationAudit
}

func NewMigrationStore(db *gorm.DB) *MigrationStore {
	if db != nil {
		db = db.Session(&gorm.Session{Logger: logger.Discard})
	}
	return &MigrationStore{db: db}
}

// WithOperationAudit returns an independent store copy that adds an audit event
// to the selected operator operation. The audit is an application-side label,
// not a signed human identity proof or a substitute for the route barriers.
func (s *MigrationStore) WithOperationAudit(actor, planSHA256, operation string) (*MigrationStore, error) {
	if s == nil || s.db == nil || !routeField(actor, 128) || !routeHash(planSHA256) {
		return nil, ErrMigrationInvalid
	}
	switch operation {
	case "prepare", "activate", "rebase":
	default:
		return nil, ErrMigrationInvalid
	}
	copy := *s
	copy.audit = &routeOperationAudit{operation: operation, actor: actor, planSHA256: planSHA256}
	return &copy, nil
}

type RouteMember struct {
	BrokerCell  string    `json:"broker_cell"`
	Role        string    `json:"role"`
	InstanceID  string    `json:"instance_id"`
	BootID      string    `json:"boot_id"`
	Generation  string    `json:"generation"`
	HeartbeatAt time.Time `json:"-"`
}

func (RouteMember) TableName() string { return "broker_route_members" }

type RouteChange struct {
	ID, TenantID, DeviceID, SourceCell, TargetCell string
	SourceEpoch                                    int64
	Actor, MembersSHA256, PlanSHA256, Phase        string
	CreatedAt                                      time.Time
	PreparedAt, ActivatedAt                        *time.Time
	TargetReadySHA256                              *string
}

func (RouteChange) TableName() string { return "broker_route_changes" }

type FreezeRouteRequest struct {
	ChangeID, TenantID, DeviceID, ExpectedCell, TargetCell, Actor string
	ExpectedEpoch                                                 int64
	MembersSHA256, PlanSHA256                                     string
}

func routeField(s string, max int) bool {
	return strings.TrimSpace(s) == s && len(s) <= max && validConfigString(s)
}
func routeHash(s string) bool {
	raw, e := hex.DecodeString(s)
	return e == nil && len(raw) == 32 && s == strings.ToLower(s)
}
func routeRole(role string) bool {
	switch role {
	case "broker", "adapter", "dispatcher", "effects", "legacy":
		return true
	}
	return false
}
func (s *MigrationStore) valid(ctx context.Context) bool {
	return s != nil && s.db != nil && ctx != nil
}
func routeCellLock(tx *gorm.DB, cell string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "broker-route-members:"+cell).Error
}

// Both existing tenant admission locks precede the binding, matching command,
// ledger, telemetry and automation writers. No external call runs under them.
func routeTenantLocks(tx *gorm.DB, tenant string) error {
	if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "brokercommands:"+tenant).Error; e != nil {
		return e
	}
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", tenant).Error
}

// RegisterMember must run on each real process boot, before it is ready, and
// periodically thereafter. A replacement boot gets a new generation, so an old
// instance's barrier acknowledgement cannot be inherited.
func (s *MigrationStore) RegisterMember(ctx context.Context, cell, role, instance, boot string) (RouteMember, error) {
	var out RouteMember
	if !s.valid(ctx) || !routeField(cell, 128) || !routeRole(role) || !routeField(instance, 128) || !routeField(boot, 128) {
		return out, ErrMigrationInvalid
	}
	e := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := routeCellLock(tx, cell); e != nil {
			return e
		}
		return tx.Raw(`INSERT INTO broker_route_members(broker_cell,role,instance_id,boot_id,generation,heartbeat_at) VALUES(?,?,?,?,?::uuid,now()) ON CONFLICT(broker_cell,role,instance_id) DO UPDATE SET boot_id=excluded.boot_id,generation=CASE WHEN broker_route_members.boot_id=excluded.boot_id THEN broker_route_members.generation ELSE excluded.generation END,heartbeat_at=now() RETURNING *`, cell, role, instance, boot, uuid.NewString()).Scan(&out).Error
	})
	return out, e
}

func membersTx(tx *gorm.DB, cell string) ([]RouteMember, string, error) {
	return readMembersTx(tx, cell, true)
}
func readMembersTx(tx *gorm.DB, cell string, requireFresh bool) ([]RouteMember, string, error) {
	var members []RouteMember
	if e := tx.Where("broker_cell=?", cell).Order("role,instance_id").Limit(65).Find(&members).Error; e != nil {
		return nil, "", e
	}
	if len(members) == 0 || len(members) > 64 {
		return nil, "", ErrBarrierIncomplete
	}
	roles := map[string]bool{}
	var stale int64
	if e := tx.Model(&RouteMember{}).Where("broker_cell=? AND heartbeat_at<now()-interval '30 seconds'", cell).Count(&stale).Error; e != nil {
		return nil, "", e
	}
	if requireFresh && stale != 0 {
		return nil, "", ErrBarrierIncomplete
	}
	for _, m := range members {
		roles[m.Role] = true
	}
	for _, r := range []string{"broker", "adapter", "dispatcher", "effects"} {
		if !roles[r] {
			return nil, "", ErrBarrierIncomplete
		}
	}
	raw, e := json.Marshal(members)
	if e != nil {
		return nil, "", e
	}
	sum := sha256.Sum256(raw)
	return members, hex.EncodeToString(sum[:]), nil
}
func (s *MigrationStore) Members(ctx context.Context, cell string) ([]RouteMember, string, error) {
	if !s.valid(ctx) || !routeField(cell, 128) {
		return nil, "", ErrMigrationInvalid
	}
	var members []RouteMember
	var digest string
	e := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := routeCellLock(tx, cell); e != nil {
			return e
		}
		var e error
		members, digest, e = membersTx(tx, cell)
		return e
	})
	return members, digest, e
}
func routeEvent(tx *gorm.DB, id, stage string, evidence any) error {
	raw, e := json.Marshal(evidence)
	if e != nil {
		return e
	}
	return tx.Exec("INSERT INTO broker_route_change_events(change_id,stage,evidence) VALUES(?::uuid,?,?::jsonb)", id, stage, string(raw)).Error
}
func freezeValid(r FreezeRouteRequest) bool {
	_, e := uuid.Parse(r.ChangeID)
	return e == nil && routeField(r.TenantID, 36) && routeField(r.DeviceID, 36) && routeField(r.ExpectedCell, 128) && routeField(r.TargetCell, 128) && r.ExpectedCell != r.TargetCell && routeField(r.Actor, 128) && r.ExpectedEpoch > 0 && r.ExpectedEpoch < math.MaxInt64 && routeHash(r.MembersSHA256) && routeHash(r.PlanSHA256)
}

// Freeze atomically revokes business/command admission and retires accepted
// old work as auditable fenced/unknown records. It never deletes or republishes
// a command, and waits for any final dispatch transaction holding the binding.
func (s *MigrationStore) Freeze(ctx context.Context, r FreezeRouteRequest) (RouteChange, error) {
	var out RouteChange
	if !s.valid(ctx) || !freezeValid(r) {
		return out, ErrMigrationInvalid
	}
	e := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := routeCellLock(tx, r.ExpectedCell); e != nil {
			return e
		}
		if e := routeTenantLocks(tx, r.TenantID); e != nil {
			return e
		}
		var binding TransportBinding
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND device_id=?", r.TenantID, r.DeviceID).Take(&binding).Error; e != nil {
			return e
		}
		var old RouteChange
		q := tx.Where("id=?::uuid", r.ChangeID).Take(&old)
		if q.Error == nil {
			if old.TenantID != r.TenantID || old.DeviceID != r.DeviceID || old.SourceCell != r.ExpectedCell || old.TargetCell != r.TargetCell || old.SourceEpoch != r.ExpectedEpoch || old.Actor != r.Actor || old.MembersSHA256 != r.MembersSHA256 || old.PlanSHA256 != r.PlanSHA256 {
				return ErrBindingConflict
			}
			out = old
			return nil
		}
		if !errors.Is(q.Error, gorm.ErrRecordNotFound) {
			return q.Error
		}
		if (binding.Phase != "active" && binding.Phase != "connecting") || binding.BrokerCell != r.ExpectedCell || binding.RouteEpoch != uint64(r.ExpectedEpoch) {
			return ErrBindingConflict
		}
		members, digest, e := membersTx(tx, r.ExpectedCell)
		if e != nil {
			return e
		}
		if digest != r.MembersSHA256 {
			return ErrBarrierIncomplete
		}
		// Failure before activation still needs a NEW freeze and another epoch.
		// Supersede its connecting change atomically; never restore an old epoch.
		if binding.Phase == "connecting" {
			var previous RouteChange
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND device_id=? AND phase='connecting' AND target_cell=? AND source_epoch=?", r.TenantID, r.DeviceID, r.ExpectedCell, r.ExpectedEpoch-1).Take(&previous).Error; e != nil {
				return e
			}
			if e := tx.Model(&previous).Update("phase", "superseded").Error; e != nil {
				return e
			}
			if e := routeEvent(tx, previous.ID, "superseded", map[string]string{"next_change_id": r.ChangeID}); e != nil {
				return e
			}
		}
		out = RouteChange{ID: r.ChangeID, TenantID: r.TenantID, DeviceID: r.DeviceID, SourceCell: r.ExpectedCell, TargetCell: r.TargetCell, SourceEpoch: r.ExpectedEpoch, Actor: r.Actor, MembersSHA256: digest, PlanSHA256: r.PlanSHA256, Phase: "frozen"}
		if e := tx.Create(&out).Error; e != nil {
			return e
		}
		for _, m := range members {
			if e := tx.Exec("INSERT INTO broker_route_barrier_members(change_id,role,instance_id,boot_id,generation) VALUES(?::uuid,?,?,?,?::uuid)", r.ChangeID, m.Role, m.InstanceID, m.BootID, m.Generation).Error; e != nil {
				return e
			}
		}
		if e := tx.Model(&binding).Updates(map[string]any{"phase": "draining", "updated_at": gorm.Expr("now()")}).Error; e != nil {
			return e
		}
		counts := map[string]int64{}
		retire := func(key, query string, args ...any) error {
			q := tx.Exec(query, args...)
			counts[key] = q.RowsAffected
			return q.Error
		}
		if e := retire("receipts_fenced", `UPDATE broker_ingress_receipts SET state='fenced',last_error='route freeze; retained for reconciliation',processed_at=now() WHERE tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=? AND state='pending'`, r.TenantID, r.DeviceID, r.ExpectedCell, r.ExpectedEpoch); e != nil {
			return e
		}
		if e := retire("effects_retired", `UPDATE broker_ingress_effects SET state=CASE WHEN state='pending' THEN 'fenced' ELSE 'unknown' END,last_error='route freeze; retained for reconciliation',updated_at=now() WHERE receipt_id IN (SELECT id FROM broker_ingress_receipts WHERE tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=?) AND state IN ('pending','dispatching')`, r.TenantID, r.DeviceID, r.ExpectedCell, r.ExpectedEpoch); e != nil {
			return e
		}
		if e := retire("commands_retired", `UPDATE broker_command_requests SET state=CASE WHEN state='accepted' THEN 'fenced' ELSE 'unknown' END,last_error='route freeze; publication may already have occurred; do not replay',updated_at=now(),completed_at=CASE WHEN state='accepted' THEN now() ELSE completed_at END WHERE tenant_id=? AND target_device_id=? AND route_cell=? AND route_epoch=? AND state IN ('accepted','dispatching','broker_acknowledged')`, r.TenantID, r.DeviceID, r.ExpectedCell, r.ExpectedEpoch); e != nil {
			return e
		}
		return routeEvent(tx, r.ChangeID, "frozen", counts)
	})
	return out, e
}

// AcknowledgeFence is only for a runtime that has actually synchronized the
// authoritative phase and fenced its scope, including its legacy buffers. A
// 64-byte evidence hash is an audit reference, never a substitute for that work.
func (s *MigrationStore) AcknowledgeFence(ctx context.Context, tenant, device, changeID, role, instance, boot, evidenceSHA string) error {
	if !s.valid(ctx) || !routeField(tenant, 36) || !routeField(device, 36) || !routeRole(role) || !routeField(instance, 128) || !routeField(boot, 128) || !routeHash(evidenceSHA) {
		return ErrMigrationInvalid
	}
	if _, e := uuid.Parse(changeID); e != nil {
		return ErrMigrationInvalid
	}
	return s.changeTx(ctx, tenant, device, changeID, "fence_ack", func(tx *gorm.DB, c *RouteChange, b *TransportBinding) error {
		frozen := c.Phase == "frozen" && b.Phase == "draining" && b.BrokerCell == c.SourceCell && b.RouteEpoch == uint64(c.SourceEpoch)
		connecting := c.Phase == "connecting" && b.Phase == "connecting" && b.BrokerCell == c.TargetCell && b.RouteEpoch == uint64(c.SourceEpoch+1)
		if !frozen && !connecting {
			return ErrBindingConflict
		}
		var existing struct {
			EvidenceSHA256 *string
			AcknowledgedAt *time.Time
		}
		qExisting := tx.Raw(`SELECT m.evidence_sha256,m.acknowledged_at FROM broker_route_barrier_members m JOIN broker_route_members live ON live.broker_cell=? AND live.role=m.role AND live.instance_id=m.instance_id AND live.boot_id=m.boot_id AND live.generation=m.generation WHERE m.change_id=?::uuid AND m.role=? AND m.instance_id=? AND m.boot_id=? AND live.heartbeat_at>=now()-interval '30 seconds'`, c.SourceCell, changeID, role, instance, boot).Scan(&existing)
		if qExisting.Error != nil {
			return qExisting.Error
		}
		if qExisting.RowsAffected != 1 {
			return ErrBarrierIncomplete
		}
		if existing.AcknowledgedAt != nil && existing.EvidenceSHA256 != nil {
			if *existing.EvidenceSHA256 != evidenceSHA {
				return ErrBarrierIncomplete
			}
			return nil
		}
		q := tx.Exec(`UPDATE broker_route_barrier_members m SET acknowledged_at=coalesce(m.acknowledged_at,now()),evidence_sha256=? FROM broker_route_members live WHERE m.change_id=?::uuid AND m.role=? AND m.instance_id=? AND m.boot_id=? AND live.broker_cell=? AND live.role=m.role AND live.instance_id=m.instance_id AND live.boot_id=m.boot_id AND live.generation=m.generation AND live.heartbeat_at>=now()-interval '30 seconds' AND (m.evidence_sha256 IS NULL OR m.evidence_sha256=?)`, evidenceSHA, changeID, role, instance, boot, c.SourceCell, evidenceSHA)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return ErrBarrierIncomplete
		}
		return routeEvent(tx, c.ID, "fence_ack", map[string]string{"role": role, "instance_id": instance, "boot_id": boot, "evidence_sha256": evidenceSHA})
	})
}

// changeTx takes locks in cell -> tenants -> binding -> change order. All later
// operations recheck the same scoped change, protecting against stale operators.
func (s *MigrationStore) changeTx(ctx context.Context, tenant, device, id, operation string, fn func(*gorm.DB, *RouteChange, *TransportBinding) error) error {
	if !s.valid(ctx) || !routeField(tenant, 36) || !routeField(device, 36) {
		return ErrMigrationInvalid
	}
	if s.audit != nil && s.audit.operation != operation {
		return ErrMigrationInvalid
	}
	if _, e := uuid.Parse(id); e != nil {
		return ErrMigrationInvalid
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var hint RouteChange
		if e := tx.Where("id=?::uuid AND tenant_id=? AND device_id=?", id, tenant, device).Take(&hint).Error; e != nil {
			return e
		}
		// Sort both cells to avoid an A->B and B->A membership lock cycle.
		cells := []string{hint.SourceCell, hint.TargetCell}
		sort.Strings(cells)
		for _, cell := range cells {
			if e := routeCellLock(tx, cell); e != nil {
				return e
			}
		}
		if e := routeTenantLocks(tx, tenant); e != nil {
			return e
		}
		var binding TransportBinding
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND device_id=?", tenant, device).Take(&binding).Error; e != nil {
			return e
		}
		var c RouteChange
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?::uuid AND tenant_id=? AND device_id=?", id, tenant, device).Take(&c).Error; e != nil {
			return e
		}
		if err := fn(tx, &c, &binding); err != nil {
			return err
		}
		if s.audit != nil {
			return routeEvent(tx, c.ID, "operator_apply", map[string]string{
				"operation":   s.audit.operation,
				"actor":       s.audit.actor,
				"plan_sha256": s.audit.planSHA256,
			})
		}
		return nil
	})
}

func barrierTx(tx *gorm.DB, c *RouteChange) error {
	// Once acknowledged, a deliberately stopped old process need not keep
	// heartbeating. A restarted process MUST register its new generation before
	// readiness, which changes this digest and invalidates the prior barrier.
	_, digest, e := readMembersTx(tx, c.SourceCell, false)
	if e != nil {
		return e
	}
	if digest != c.MembersSHA256 {
		return ErrBarrierIncomplete
	}
	var missing int64
	if e := tx.Table("broker_route_barrier_members").Where("change_id=?::uuid AND acknowledged_at IS NULL", c.ID).Count(&missing).Error; e != nil {
		return e
	}
	if missing != 0 {
		return ErrBarrierIncomplete
	}
	return nil
}

// RebaseBarrier is an explicit recovery operation after source membership
// changed. It keeps the route paused and discards EVERY old acknowledgement.
// The caller must review the new manifest and supply a reason/evidence hash.
func (s *MigrationStore) RebaseBarrier(ctx context.Context, tenant, device, id, expectedMembersSHA, newMembersSHA, reasonSHA string) error {
	if !routeHash(expectedMembersSHA) || !routeHash(newMembersSHA) || !routeHash(reasonSHA) {
		return ErrMigrationInvalid
	}
	return s.changeTx(ctx, tenant, device, id, "rebase", func(tx *gorm.DB, c *RouteChange, b *TransportBinding) error {
		frozen := c.Phase == "frozen" && b.Phase == "draining" && b.BrokerCell == c.SourceCell && b.RouteEpoch == uint64(c.SourceEpoch)
		connecting := c.Phase == "connecting" && b.Phase == "connecting" && b.BrokerCell == c.TargetCell && b.RouteEpoch == uint64(c.SourceEpoch+1)
		if (!frozen && !connecting) || c.MembersSHA256 != expectedMembersSHA {
			return ErrBindingConflict
		}
		members, digest, e := membersTx(tx, c.SourceCell)
		if e != nil {
			return e
		}
		if digest != newMembersSHA {
			return ErrBarrierIncomplete
		}
		if e := tx.Exec("DELETE FROM broker_route_barrier_members WHERE change_id=?::uuid", id).Error; e != nil {
			return e
		}
		for _, m := range members {
			if e := tx.Exec("INSERT INTO broker_route_barrier_members(change_id,role,instance_id,boot_id,generation) VALUES(?::uuid,?,?,?,?::uuid)", id, m.Role, m.InstanceID, m.BootID, m.Generation).Error; e != nil {
				return e
			}
		}
		if e := tx.Model(c).Update("members_sha256", digest).Error; e != nil {
			return e
		}
		return routeEvent(tx, id, "barrier_rebased", map[string]string{"previous_members_sha256": expectedMembersSHA, "members_sha256": digest, "reason_sha256": reasonSHA})
	})
}

// PrepareTarget commits a new epoch only after all old members attest fencing.
// Target-ready SHA references config/storage/consumer checks by the operator;
// fresh registrations alone do not prove readiness or complete an AC test.
func (s *MigrationStore) PrepareTarget(ctx context.Context, tenant, device, id, targetReadySHA string) error {
	if !routeHash(targetReadySHA) {
		return ErrMigrationInvalid
	}
	return s.changeTx(ctx, tenant, device, id, "prepare", func(tx *gorm.DB, c *RouteChange, b *TransportBinding) error {
		if c.Phase == "connecting" && b.Phase == "connecting" && b.BrokerCell == c.TargetCell && b.RouteEpoch == uint64(c.SourceEpoch+1) && c.TargetReadySHA256 != nil && *c.TargetReadySHA256 == targetReadySHA {
			return nil
		}
		if c.Phase != "frozen" || b.Phase != "draining" || b.BrokerCell != c.SourceCell || b.RouteEpoch != uint64(c.SourceEpoch) {
			return ErrBindingConflict
		}
		if e := barrierTx(tx, c); e != nil {
			return e
		}
		if _, _, e := membersTx(tx, c.TargetCell); e != nil {
			return e
		}
		if e := tx.Model(b).Updates(map[string]any{"broker_cell": c.TargetCell, "route_epoch": c.SourceEpoch + 1, "phase": "connecting", "updated_at": gorm.Expr("now()")}).Error; e != nil {
			return e
		}
		if e := tx.Model(c).Updates(map[string]any{"phase": "connecting", "prepared_at": gorm.Expr("now()"), "target_ready_sha256": targetReadySHA}).Error; e != nil {
			return e
		}
		return routeEvent(tx, c.ID, "connecting", map[string]any{"route_epoch": c.SourceEpoch + 1, "target_ready_sha256": targetReadySHA})
	})
}

// Activate requires BOTH a fresh real connection observation and a nonempty
// trusted ingress receipt accepted in this new connecting epoch. AUTH alone,
// a changed DNS record or an old retained message cannot satisfy this check.
// Receipt admission checks the incremented binding epoch under its row lock,
// which proves causality after PrepareTarget. Its application/observed timestamp
// is deliberately not compared with PostgreSQL's prepared_at clock.
func (s *MigrationStore) Activate(ctx context.Context, tenant, device, id string) error {
	return s.changeTx(ctx, tenant, device, id, "activate", func(tx *gorm.DB, c *RouteChange, b *TransportBinding) error {
		if c.Phase == "active" && b.Phase == "active" && b.BrokerCell == c.TargetCell && b.RouteEpoch == uint64(c.SourceEpoch+1) {
			return nil
		}
		if c.Phase != "connecting" || b.Phase != "connecting" || b.BrokerCell != c.TargetCell || b.RouteEpoch != uint64(c.SourceEpoch+1) {
			return ErrBindingConflict
		}
		if e := barrierTx(tx, c); e != nil {
			return e
		}
		if _, _, e := membersTx(tx, c.TargetCell); e != nil {
			return e
		}
		var connected, ingress bool
		if e := tx.Raw(`SELECT EXISTS(SELECT 1 FROM broker_connection_observations WHERE tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=? AND online AND received_at>=? AND received_at>=now()-interval '30 seconds' AND observed_at>=now()-interval '30 seconds' AND observed_at<=now()+interval '5 seconds')`, tenant, device, c.TargetCell, c.SourceEpoch+1, c.PreparedAt).Scan(&connected).Error; e != nil {
			return e
		}
		if e := tx.Raw(`SELECT EXISTS(SELECT 1 FROM broker_ingress_receipts WHERE tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=? AND state='pending' AND octet_length(payload)>0)`, tenant, device, c.TargetCell, c.SourceEpoch+1).Scan(&ingress).Error; e != nil {
			return e
		}
		if !connected || !ingress {
			return ErrTargetUnproven
		}
		if e := tx.Model(b).Updates(map[string]any{"phase": "active", "updated_at": gorm.Expr("now()")}).Error; e != nil {
			return e
		}
		if e := tx.Model(c).Updates(map[string]any{"phase": "active", "activated_at": gorm.Expr("now()")}).Error; e != nil {
			return e
		}
		return routeEvent(tx, c.ID, "active", map[string]any{"route_epoch": c.SourceEpoch + 1, "real_connection": connected, "trusted_ingress": ingress})
	})
}
