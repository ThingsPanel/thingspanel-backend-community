// Package brokerdelegate revalidates immutable plugin authority at each durable
// effect boundary. It never grants authority from a payload's device selector.
package brokerdelegate

import (
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	bc "project/internal/brokercontract"
	"project/internal/brokerroute"
)

// Lock verifies the Owner context before locking both live routes. The caller
// must use this same transaction for the protected write.
func Lock(tx *gorm.DB, tenant string, d *bc.Delegation, admission bool) error {
	return check(tx, tenant, d, admission, true)
}
func Check(tx *gorm.DB, tenant string, d *bc.Delegation, admission bool) error {
	return check(tx, tenant, d, admission, false)
}
func check(tx *gorm.DB, tenant string, d *bc.Delegation, admission, locked bool) error {
	if tx == nil || d == nil || d.Validate() != nil {
		return brokerroute.ErrFenced
	}
	ctx, err := readContext(tx, d.ContextID)
	if err != nil {
		return err
	}
	if ctx.Principal != d.Actor || ctx.ConnectionID != d.ConnectionID || ctx.BrokerCell != d.BrokerCell || ctx.BrokerInstance != d.BrokerInstance || d.ObservedAt.Before(ctx.IssuedAt.Add(-5*time.Second)) || d.ObservedAt.After(ctx.ExpiresAt.Add(5*time.Second)) {
		return brokerroute.ErrFenced
	}
	matched := false
	for _, target := range ctx.Targets {
		root, epoch := target.Transport()
		if target.TenantID == tenant && target.DeviceID == d.TargetDeviceID && target.RouteEpoch == d.TargetRouteEpoch && root == d.TransportDeviceID && epoch == d.TransportRouteEpoch && target.MappingVersion == d.MappingVersion {
			matched = true
			break
		}
	}
	if !matched {
		return brokerroute.ErrFenced
	}
	scope := brokerroute.Snapshot{TenantID: tenant, TargetID: d.TargetDeviceID, TargetEpoch: d.TargetRouteEpoch, TransportID: d.TransportDeviceID, TransportEpoch: d.TransportRouteEpoch, Cell: d.BrokerCell}
	if locked {
		return brokerroute.Lock(tx, scope, admission)
	}
	return brokerroute.Check(tx, scope, admission)
}

type pluginContext struct {
	ID                                       string
	Principal                                bc.Principal
	BrokerCell, BrokerInstance, ConnectionID string
	Targets                                  []bc.PluginTarget
	IssuedAt, ExpiresAt                      time.Time
}

func readContext(tx *gorm.DB, id string) (pluginContext, error) {
	var row struct {
		ID                                       string
		PrincipalJSON, TargetsJSON               []byte
		BrokerCell, BrokerInstance, ConnectionID string
		IssuedAt, ExpiresAt                      time.Time
	}
	err := tx.Table("broker_plugin_contexts").Where("id=?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return pluginContext{}, brokerroute.ErrFenced
	}
	if err != nil {
		return pluginContext{}, err
	}
	var out pluginContext
	if json.Unmarshal(row.PrincipalJSON, &out.Principal) != nil || json.Unmarshal(row.TargetsJSON, &out.Targets) != nil || bc.ValidatePrincipal(out.Principal) != nil || len(out.Targets) == 0 || len(out.Targets) > bc.MaxPluginTargets {
		return out, brokerroute.ErrFenced
	}
	if bc.ValidatePluginTargets(out.Targets) != nil {
		return out, brokerroute.ErrFenced
	}
	for _, target := range out.Targets {
		if target.Validate() != nil {
			return out, brokerroute.ErrFenced
		}
	}
	expected, err := bc.PluginContextID(out.Principal, row.BrokerCell, row.BrokerInstance, row.ConnectionID, out.Targets)
	if err != nil || expected != row.ID || !row.ExpiresAt.After(row.IssuedAt) {
		return out, brokerroute.ErrFenced
	}
	out.ID = row.ID
	out.BrokerCell = row.BrokerCell
	out.BrokerInstance = row.BrokerInstance
	out.ConnectionID = row.ConnectionID
	out.IssuedAt = row.IssuedAt
	out.ExpiresAt = row.ExpiresAt
	return out, nil
}
