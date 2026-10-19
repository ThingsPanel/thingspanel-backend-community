package brokercommands

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaxPlanTTL = 2 * time.Minute
const maxPendingPlans = 256
const maxPendingPlanBytes = 16 << 20

// Plan is server-generated input plus the context fingerprint to revalidate.
// No operation here invokes a model or accepts a model's approval assertion.
type Plan struct {
	ID                                                   string `gorm:"type:uuid;primaryKey"`
	TenantID, CallerID, Intent, DeviceID, TargetDeviceID string
	RouteCell                                            string
	RouteEpoch                                           int64
	CanonicalRequest, Payload, ContextSHA256, PlanSHA256 []byte
	ExpiresAt, CreatedAt                                 time.Time
	AuthorizationID                                      *string `gorm:"type:uuid"`
	AuthorizedAt, RevokedAt                              *time.Time
	CommandID                                            *string `gorm:"type:uuid"`
}

func (Plan) TableName() string { return "broker_command_plans" }

// PlanGuard must revalidate caller, model/configuration, state and policy
// context. It runs under the same route and tenant locks as command admission.
type PlanGuard func(context.Context, *gorm.DB, Plan) error

func (s *Store) CreatePlan(ctx context.Context, req Request, contextSHA []byte, guard PlanGuard) (Plan, error) {
	if s == nil || s.db == nil || ctx == nil || guard == nil || len(contextSHA) != sha256.Size {
		return Plan{}, ErrInvalid
	}
	if err := validateRequest(req); err != nil {
		return Plan{}, err
	}
	if req.TargetDeviceID == "" {
		req.TargetDeviceID = req.DeviceID
	}
	if !req.ExpiresAt.After(time.Now()) || req.ExpiresAt.After(time.Now().Add(MaxPlanTTL)) {
		return Plan{}, ErrInvalid
	}
	var p Plan
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTenant(tx, req.TenantID); err != nil {
			return err
		}
		if !req.ExpiresAt.After(time.Now()) {
			return ErrRejected
		}
		binding, err := activeBinding(tx, req.TenantID, req.TargetDeviceID)
		if err != nil {
			return err
		}
		if err := checkDevicePair(tx, req.TenantID, req.DeviceID, req.TargetDeviceID); err != nil {
			return err
		}
		var pending struct{ Count, Bytes int64 }
		if err := tx.Raw(`SELECT count(*) AS count,coalesce(sum(octet_length(canonical_request)+octet_length(payload)),0) AS bytes FROM broker_command_plans WHERE tenant_id=? AND expires_at>now() AND command_id IS NULL AND revoked_at IS NULL`, req.TenantID).Scan(&pending).Error; err != nil {
			return err
		}
		if pending.Count >= maxPendingPlans || pending.Bytes+int64(len(req.CanonicalRequest)+len(req.Payload)) > maxPendingPlanBytes {
			return ErrCapacity
		}
		p = Plan{ID: uuid.NewString(), TenantID: req.TenantID, CallerID: req.CallerID, Intent: req.Intent, DeviceID: req.DeviceID, TargetDeviceID: req.TargetDeviceID, RouteCell: binding.BrokerCell, RouteEpoch: binding.RouteEpoch, CanonicalRequest: append([]byte(nil), req.CanonicalRequest...), Payload: append([]byte(nil), req.Payload...), ContextSHA256: append([]byte(nil), contextSHA...), ExpiresAt: req.ExpiresAt.UTC(), CreatedAt: time.Now().UTC()}
		canonicalSHA, payloadSHA := sha256.Sum256(p.CanonicalRequest), sha256.Sum256(p.Payload)
		encoded, err := json.Marshal(struct {
			ID, Tenant, Caller, Intent, Device, Root, Cell string
			Epoch                                          int64
			Canonical, Payload, Context                    []byte
			Expires                                        time.Time
		}{p.ID, p.TenantID, p.CallerID, p.Intent, p.DeviceID, p.TargetDeviceID, p.RouteCell, p.RouteEpoch, canonicalSHA[:], payloadSHA[:], p.ContextSHA256, p.ExpiresAt})
		if err != nil {
			return err
		}
		digest := sha256.Sum256(encoded)
		p.PlanSHA256 = append([]byte(nil), digest[:]...)
		if err := guard(ctx, tx, p); err != nil {
			return err
		}
		return tx.Create(&p).Error
	})
	return p, err
}

func (s *Store) GetPlan(ctx context.Context, tenant, caller, id string) (Plan, error) {
	if s == nil || s.db == nil || ctx == nil || tenant == "" || caller == "" {
		return Plan{}, ErrInvalid
	}
	if _, err := uuid.Parse(id); err != nil {
		return Plan{}, ErrNotFound
	}
	var p Plan
	err := s.db.WithContext(ctx).Where("tenant_id=? AND caller_id=? AND id=?", tenant, caller, id).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Plan{}, ErrNotFound
	}
	return p, err
}

// AuthorizePlan is for an authenticated human confirmation handler or a
// separately audited policy owner. It must not be included in model tools.
func (s *Store) AuthorizePlan(ctx context.Context, tenant, caller, id string, hash []byte, guard PlanGuard) (string, error) {
	if s == nil || s.db == nil || ctx == nil || guard == nil || tenant == "" || caller == "" || len(hash) != 32 {
		return "", ErrInvalid
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", ErrNotFound
	}
	var authorization string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTenant(tx, tenant); err != nil {
			return err
		}
		// Follow dispatcher lock order: binding before plan/command rows. A
		// final dispatch guard may inspect the same plan while holding binding.
		var initial Plan
		if err := tx.Where("tenant_id=? AND caller_id=? AND id=?", tenant, caller, id).Take(&initial).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		binding, err := activeBinding(tx, tenant, initial.TargetDeviceID)
		if err != nil {
			return err
		}
		if binding.BrokerCell != initial.RouteCell || binding.RouteEpoch != initial.RouteEpoch {
			return ErrFenced
		}
		var p Plan
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND caller_id=? AND id=?", tenant, caller, id).Take(&p).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !equalDigest(hash, p.PlanSHA256) {
			return ErrConflict
		}
		if p.RevokedAt != nil || p.CommandID != nil || !p.ExpiresAt.After(time.Now()) {
			return ErrRejected
		}
		if err := guard(ctx, tx, p); err != nil {
			return err
		}
		if p.AuthorizationID != nil {
			authorization = *p.AuthorizationID
			return nil
		}
		authorization = uuid.NewString()
		now := time.Now().UTC()
		return tx.Model(&Plan{}).Where("id=?", id).Updates(map[string]any{"authorization_id": authorization, "authorized_at": now}).Error
	})
	return authorization, err
}

// SubmitPlan consumes approval and creates the command/log in one transaction.
// A changed idempotency key can never turn one approved plan into two actions.
func (s *Store) SubmitPlan(ctx context.Context, tenant, caller, id string, hash []byte, authorization, key string, guard PlanGuard, persist func(*gorm.DB, Row) error) (Row, bool, error) {
	if len(hash) != 32 || authorization == "" || key == "" || guard == nil || persist == nil {
		return Row{}, false, ErrInvalid
	}
	p, err := s.GetPlan(ctx, tenant, caller, id)
	if err != nil {
		return Row{}, false, err
	}
	if !equalDigest(hash, p.PlanSHA256) {
		return Row{}, false, ErrConflict
	}
	if p.RevokedAt != nil || p.AuthorizationID == nil || *p.AuthorizationID != authorization {
		return Row{}, false, ErrRejected
	}
	// Bind retries to this specific plan, including after the plan has expired.
	canonical, err := json.Marshal(struct {
		PlanID     string
		PlanSHA256 []byte
	}{p.ID, p.PlanSHA256})
	if err != nil {
		return Row{}, false, err
	}
	req := Request{TenantID: p.TenantID, CallerID: p.CallerID, Intent: p.Intent, IdempotencyKey: key, DeviceID: p.DeviceID, TargetDeviceID: p.TargetDeviceID, CanonicalRequest: canonical, Payload: p.Payload, ExpiresAt: p.ExpiresAt}
	return s.Submit(ctx, req, func(tx *gorm.DB, row Row) error {
		var current Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND caller_id=? AND id=?", tenant, caller, id).Take(&current).Error; err != nil {
			return err
		}
		if current.RevokedAt != nil || current.CommandID != nil || current.AuthorizationID == nil || *current.AuthorizationID != authorization || !current.ExpiresAt.After(time.Now()) || !equalDigest(hash, current.PlanSHA256) {
			return ErrRejected
		}
		if row.RouteCell != current.RouteCell || row.RouteEpoch != current.RouteEpoch {
			return ErrFenced
		}
		if err := guard(ctx, tx, current); err != nil {
			return err
		}
		if err := persist(tx, row); err != nil {
			return err
		}
		return tx.Model(&Plan{}).Where("id=? AND command_id IS NULL", id).Update("command_id", row.ID).Error
	})
}
