// Package brokerroute fences a delegated logical device and its physical
// transport together. Callers must keep the transaction open through effects.
package brokerroute

import (
	"errors"
	"math"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrConnecting = errors.New("delegated route admission is still connecting")

var ErrFenced = errors.New("delegated target or transport route is fenced")

// Snapshot is owner-derived. TargetEpoch and TransportEpoch are independent;
// equal values never imply equal identities or shared migration lifetimes.
type Snapshot struct {
	TenantID       string `json:"tenant_id"`
	TargetID       string `json:"target_device_id"`
	TargetEpoch    uint64 `json:"target_route_epoch"`
	TransportID    string `json:"transport_device_id"`
	TransportEpoch uint64 `json:"transport_route_epoch"`
	Cell           string `json:"broker_cell"`
}

func (s Snapshot) Validate() error {
	if s.TenantID == "" || s.TargetID == "" || s.TransportID == "" || s.Cell == "" || s.TargetEpoch == 0 || s.TransportEpoch == 0 || s.TargetEpoch > math.MaxInt64 || s.TransportEpoch > math.MaxInt64 || (s.TargetID == s.TransportID && s.TargetEpoch != s.TransportEpoch) {
		return ErrFenced
	}
	return nil
}

// Lock takes the common tenant admission lock, then binding locks in lexical
// order. The live ancestry is locked as well, so reparent/disable cannot race a
// successful validation. No binding is created or reassigned here.
func Lock(tx *gorm.DB, s Snapshot, admission bool) error { return check(tx, s, admission, true) }

// Check inspects the same scope without acquiring row locks, for evidence only.
// It must never authorize a write; writers use Lock in their transaction.
func Check(tx *gorm.DB, s Snapshot, admission bool) error { return check(tx, s, admission, false) }
func check(tx *gorm.DB, s Snapshot, admission, locked bool) error {
	if tx == nil || s.Validate() != nil {
		return ErrFenced
	}
	if locked {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", s.TenantID).Error; err != nil {
			return err
		}
	}
	ids := []string{s.TargetID}
	if s.TransportID != s.TargetID {
		ids = append(ids, s.TransportID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var row struct {
			BrokerCell string
			RouteEpoch uint64
			Phase      string
		}
		query := tx.Table("broker_transport_bindings")
		if locked {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		r := query.Where("tenant_id=? AND device_id=?", s.TenantID, id).Take(&row)
		if errors.Is(r.Error, gorm.ErrRecordNotFound) {
			return ErrFenced
		}
		if r.Error != nil {
			return r.Error
		}
		epoch := s.TargetEpoch
		if id == s.TransportID {
			epoch = s.TransportEpoch
		}
		if !admission && row.Phase == "connecting" && row.BrokerCell == s.Cell && row.RouteEpoch == epoch {
			return ErrConnecting
		}
		if row.BrokerCell != s.Cell || row.RouteEpoch != epoch || (row.Phase != "active" && !(admission && row.Phase == "connecting")) {
			return ErrFenced
		}
	}
	return ancestry(tx, s.TenantID, s.TargetID, s.TransportID, locked)
}

// LockAncestry proves an active, tenant-local path ending at an actual root.
// Traversal is bounded and rejects cycles and missing/cross-tenant parents.
func LockAncestry(tx *gorm.DB, tenant, target, root string) error {
	return ancestry(tx, tenant, target, root, true)
}
func ancestry(tx *gorm.DB, tenant, target, root string, locked bool) error {
	if tx == nil || tenant == "" || target == "" || root == "" {
		return ErrFenced
	}
	seen := map[string]bool{}
	for depth, id := 0, target; depth < 8; depth++ {
		if seen[id] {
			return ErrFenced
		}
		seen[id] = true
		var row struct{ ParentID *string }
		query := tx.Table("devices").Select("parent_id")
		if locked {
			query = query.Clauses(clause.Locking{Strength: "SHARE"})
		}
		r := query.Where("tenant_id=? AND id=? AND is_enabled='enabled' AND activate_flag='active'", tenant, id).Take(&row)
		if errors.Is(r.Error, gorm.ErrRecordNotFound) {
			return ErrFenced
		}
		if r.Error != nil {
			return r.Error
		}
		if id == root {
			if row.ParentID != nil && *row.ParentID != "" {
				return ErrFenced
			}
			return nil
		}
		if row.ParentID == nil || *row.ParentID == "" {
			return ErrFenced
		}
		id = *row.ParentID
	}
	return ErrFenced
}
