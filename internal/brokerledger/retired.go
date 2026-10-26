package brokerledger

import "gorm.io/gorm"

// Audited irreversible route retirement differs from an unexplained inactive
// binding or dependency failure. Only the former can be durably quarantined and
// transport-ACKed, avoiding an endless replay loop for old retained/offline data.
// The caller holds the authoritative binding lock; no old payload is relabelled
// with a current epoch and no accepted business receipt is manufactured.
func auditedRouteRetired(tx *gorm.DB, tenant, device, cell string, epoch int64, current bindingRow) (bool, error) {
	if current.RouteEpoch < epoch || (current.RouteEpoch == epoch && (current.BrokerCell != cell || current.Phase != "draining")) {
		return false, nil
	}
	var available bool
	if e := tx.Raw("SELECT to_regclass('broker_route_changes') IS NOT NULL").Scan(&available).Error; e != nil {
		return false, e
	}
	if !available {
		return false, nil
	}
	var retired bool
	query := tx.Table("broker_route_changes").Select("count(*)>0").Where("tenant_id=? AND device_id=? AND source_cell=? AND source_epoch=?", tenant, device, cell, epoch)
	if current.RouteEpoch == epoch {
		query = query.Where("phase='frozen'")
	} else {
		query = query.Where("phase IN ('frozen','connecting','active','superseded')")
	}
	e := query.Scan(&retired).Error
	return retired, e
}
