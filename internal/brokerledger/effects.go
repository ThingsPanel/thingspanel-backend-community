package brokerledger

import "gorm.io/gorm"

const MaxPendingEffects = 10000
const MaxPendingEffectBytes = 64 << 20

// CheckEffectCapacity is called inside the business transaction, after adding
// its intent. Writers must acquire the tenant admission advisory lock BEFORE
// transport bindings so all effect admission shares the same lock order.
func CheckEffectCapacity(tx *gorm.DB, tenant string) error {
	var usage struct{ Count, Bytes int64 }
	if e := tx.Table("broker_ingress_effects").Select("count(*) AS count,coalesce(sum(octet_length(payload::text)),0) AS bytes").Where("tenant_id=? AND state='pending'", tenant).Scan(&usage).Error; e != nil {
		return e
	}
	if usage.Count > MaxPendingEffects || usage.Bytes > MaxPendingEffectBytes {
		return ErrTenantCapacity
	}
	return nil
}
