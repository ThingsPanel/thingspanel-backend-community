package brokercontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
)

// SyncFinalSQLRuntime is exclusive to the ingress_v1 implementation whose final
// business/state/automation/command commits lock and verify the SQL binding.
// It MUST NOT be used to acknowledge legacy workers or a broker's local gate.
func (s *MigrationStore) SyncFinalSQLRuntime(ctx context.Context, cell, instance, boot string, roles []string) error {
	if !s.valid(ctx) || len(roles) == 0 || len(roles) > 3 {
		return ErrMigrationInvalid
	}
	seen := map[string]bool{}
	for _, role := range roles {
		if (role != "adapter" && role != "effects" && role != "dispatcher") || seen[role] {
			return ErrMigrationInvalid
		}
		seen[role] = true
		if _, e := s.RegisterMember(ctx, cell, role, instance, boot); e != nil {
			return e
		}
	}
	fences, e := s.RuntimeFences(ctx, cell)
	if e != nil {
		return e
	}
	if fences.Overflow {
		return ErrBarrierIncomplete
	}
	for _, f := range fences.Fences {
		if !f.NeedsAck {
			continue
		}
		// Validate the actual frozen scope in a transaction using the SAME lock
		// order as final commits. Freeze retired all old pending receipts/effects
		// and unsent/uncertain commands, so none may remain eligible here.
		e := s.changeTx(ctx, f.TenantID, f.DeviceID, f.ChangeID, func(tx *gorm.DB, c *RouteChange, b *TransportBinding) error {
			frozen := c.Phase == "frozen" && b.Phase == "draining" && b.BrokerCell == c.SourceCell && b.RouteEpoch == uint64(c.SourceEpoch)
			connecting := c.Phase == "connecting" && b.Phase == "connecting" && b.BrokerCell == c.TargetCell && b.RouteEpoch == uint64(c.SourceEpoch+1)
			if !frozen && !connecting {
				return ErrBindingConflict
			}
			var pending int64
			if e := tx.Raw(`SELECT (SELECT count(*) FROM broker_ingress_receipts WHERE tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=? AND state='pending')+(SELECT count(*) FROM broker_ingress_effects e JOIN broker_ingress_receipts r ON r.id=e.receipt_id WHERE r.tenant_id=? AND r.device_id=? AND r.broker_cell=? AND r.route_epoch=? AND e.state IN ('pending','dispatching'))+(SELECT count(*) FROM broker_command_requests WHERE tenant_id=? AND target_device_id=? AND route_cell=? AND route_epoch=? AND state IN ('accepted','dispatching','broker_acknowledged'))`, f.TenantID, f.DeviceID, cell, f.DeniedEpoch, f.TenantID, f.DeviceID, cell, f.DeniedEpoch, f.TenantID, f.DeviceID, cell, f.DeniedEpoch).Scan(&pending).Error; e != nil {
				return e
			}
			if pending != 0 {
				return ErrBarrierIncomplete
			}
			return nil
		})
		if errors.Is(e, ErrBindingConflict) {
			continue
		}
		if e != nil {
			return e
		}
		for _, role := range roles {
			proof, _ := json.Marshal(struct {
				Version, Cell, Instance, Boot, Role, Change string
				Epoch                                       uint64
			}{"ingress_v1-final-sql-fence-v1", cell, instance, boot, role, f.ChangeID, f.DeniedEpoch})
			sum := sha256.Sum256(proof)
			e := s.AcknowledgeFence(ctx, f.TenantID, f.DeviceID, f.ChangeID, role, instance, boot, hex.EncodeToString(sum[:]))
			if errors.Is(e, ErrBindingConflict) || errors.Is(e, ErrBarrierIncomplete) {
				continue
			}
			if e != nil {
				return e
			}
		}
	}
	return nil
}
