package brokercommands

import (
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"project/internal/brokerroute"
)

// CapturePluginRoute freezes both routes during preparation. It performs no
// admission: Submit/Claim/Dispatch must Lock the snapshot in their transaction.
func CapturePluginRoute(tx *gorm.DB, tenant, target string) (*brokerroute.Snapshot, error) {
	id := target
	seen := map[string]bool{}
	root := ""
	for depth := 0; depth < 8; depth++ {
		if seen[id] {
			return nil, ErrFenced
		}
		seen[id] = true
		var device struct{ ParentID *string }
		e := tx.Table("devices").Select("parent_id").Where("tenant_id=? AND id=? AND is_enabled='enabled' AND activate_flag='active'", tenant, id).Take(&device).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, ErrFenced
		}
		if e != nil {
			return nil, e
		}
		if device.ParentID == nil || *device.ParentID == "" {
			root = id
			break
		}
		id = *device.ParentID
	}
	if root == "" {
		return nil, ErrFenced
	}
	var targetBinding, rootBinding bindingRow
	for _, entry := range []struct {
		id  string
		row *bindingRow
	}{{target, &targetBinding}, {root, &rootBinding}} {
		e := tx.Where("tenant_id=? AND device_id=?", tenant, entry.id).Take(entry.row).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, ErrFenced
		}
		if e != nil {
			return nil, e
		}
		if entry.row.Phase != "active" || entry.row.RouteEpoch <= 0 {
			return nil, ErrFenced
		}
	}
	snapshot := &brokerroute.Snapshot{TenantID: tenant, TargetID: target, TargetEpoch: uint64(targetBinding.RouteEpoch), TransportID: root, TransportEpoch: uint64(rootBinding.RouteEpoch), Cell: targetBinding.BrokerCell}
	if rootBinding.BrokerCell != snapshot.Cell || snapshot.Validate() != nil {
		return nil, ErrFenced
	}
	return snapshot, nil
}

func lockPreparedRoute(tx *gorm.DB, tenant, target string, payload []byte) error {
	// Store still accepts opaque native payloads. A supplied transport proof is
	// never ignored, even when malformed; it must agree with the admitted target.
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil {
		return nil
	}
	raw, exists := fields["transport_route"]
	if !exists {
		return nil
	}
	var scope brokerroute.Snapshot
	if json.Unmarshal(raw, &scope) != nil || scope.TenantID != tenant || scope.TargetID != target || scope.Validate() != nil {
		return ErrFenced
	}
	e := brokerroute.Lock(tx, scope, false)
	if errors.Is(e, brokerroute.ErrFenced) || errors.Is(e, brokerroute.ErrConnecting) {
		return ErrFenced
	}
	return e
}
