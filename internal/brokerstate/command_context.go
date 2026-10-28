package brokerstate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/internal/model"
)

// CommandContext is for the existing command owner's locked transaction. Its
// caller MUST hold the current root binding FOR UPDATE until acceptance or
// dispatch completes; this serializes telemetry/state commits and route CAS.
// It only reads evidence, never schedules a projection or writes is_online.
func CommandContext(ctx context.Context, tx *gorm.DB, tenant, deviceID, root string) ([]byte, error) {
	if ctx == nil || tx == nil || tenant == "" || deviceID == "" || root == "" {
		return nil, errors.New("command state context is incomplete")
	}
	tx = tx.WithContext(ctx)
	var device model.Device
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND id=? AND is_enabled='enabled' AND activate_flag='active'", tenant, deviceID).Take(&device).Error; err != nil {
		return nil, err
	}
	var route routeRow
	if err := tx.Raw(`SELECT broker_cell,route_epoch,phase FROM broker_transport_bindings WHERE tenant_id=? AND device_id=?`, tenant, root).Scan(&route).Error; err != nil {
		return nil, err
	}
	if route.Phase != "active" || route.BrokerCell == "" || route.RouteEpoch == 0 {
		return nil, ErrFenced
	}
	decision, err := evaluateCurrent(tx, device, root, route, false)
	if err != nil {
		return nil, err
	}
	// The first controlled-action profile requires fresh positive liveness.
	// Unknown, stale and disconnected devices remain diagnostic-only.
	if !decision.Fresh || decision.Status != "online" {
		return nil, ErrFenced
	}
	var activity activityRow
	err = tx.Where("tenant_id=? AND device_id=?", tenant, deviceID).Take(&activity).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// Include current root connection versions, including fresh tombstones. A
	// later close/replacement changes the fingerprint. Historical tombstones
	// remain in the state owner for anti-replay but cannot exhaust plan reads.
	var connections []struct {
		BrokerInstance, NodeBootID, ConnectionID string
		EventSeq                                 uint64
		Online                                   bool
		ObservedAt, ReceivedAt                   time.Time
	}
	now := time.Now().UTC()
	if err := tx.Table("broker_connection_observations").Select("broker_instance,node_boot_id,connection_id,event_seq,online,observed_at,received_at").Where("tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=? AND received_at>=? AND observed_at>=? AND observed_at<=?", tenant, root, route.BrokerCell, route.RouteEpoch, now.Add(-60*time.Second), now.Add(-60*time.Second), now.Add(5*time.Second)).Order("broker_instance,node_boot_id,connection_id").Limit(257).Find(&connections).Error; err != nil {
		return nil, err
	}
	if len(connections) > 256 {
		return nil, ErrFenced
	}
	encoded, err := json.Marshal(struct {
		Tenant, Device, Root, Cell string
		Epoch                      uint64
		Status                     string
		Receipt                    string
		Observed                   time.Time
		Connections                any
	}{tenant, deviceID, root, route.BrokerCell, route.RouteEpoch, decision.Status, activity.ReceiptID, activity.ObservedAt, connections})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	return digest[:], nil
}
