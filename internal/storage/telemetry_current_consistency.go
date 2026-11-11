package storage

import (
	"fmt"
	"math"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// verifyTelemetryCurrentWinners runs before the caller commits history,
// receipts or automation effects. A zero-row upsert can be legitimate when a
// newer snapshot already exists; affected-row counts alone cannot prove this.
func verifyTelemetryCurrentWinners(tx *gorm.DB, winners []TelemetryCurrentData) error {
	type identity struct{ device, key string }
	expected := make(map[identity]TelemetryCurrentData, len(winners))
	seen := make(map[identity]bool, len(winners))
	for _, winner := range winners {
		expected[identity{winner.DeviceID, winner.Key}] = winner
	}
	for offset := 0; offset < len(winners); offset += 1000 {
		end := offset + 1000
		if end > len(winners) {
			end = len(winners)
		}
		keys := make([][]interface{}, 0, end-offset)
		for _, winner := range winners[offset:end] {
			keys = append(keys, []interface{}{winner.DeviceID, winner.Key})
		}
		var stored []TelemetryCurrentData
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("(device_id, "+flavorOf(tx).key()+") IN ?", keys).Order("device_id").Order(flavorOf(tx).key()).Find(&stored).Error; err != nil {
			return err
		}
		for _, current := range stored {
			id := identity{current.DeviceID, current.Key}
			winner, ok := expected[id]
			if !ok || seen[id] {
				return fmt.Errorf("unexpected or duplicate telemetry current key")
			}
			seen[id] = true
			if current.TenantID != winner.TenantID || current.TS.Before(winner.TS) {
				return fmt.Errorf("telemetry current does not include the accepted history winner")
			}
			if current.TS.Equal(winner.TS) && !sameTelemetryCurrentValues(current, winner) {
				return fmt.Errorf("telemetry current value differs from its accepted history winner")
			}
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("telemetry current winner missing")
	}
	return nil
}

func sameTelemetryCurrentValues(a, b TelemetryCurrentData) bool {
	if (a.BoolV == nil) != (b.BoolV == nil) || (a.NumberV == nil) != (b.NumberV == nil) || (a.StringV == nil) != (b.StringV == nil) {
		return false
	}
	if a.BoolV != nil && *a.BoolV != *b.BoolV {
		return false
	}
	if a.StringV != nil && *a.StringV != *b.StringV {
		return false
	}
	if a.NumberV != nil && *a.NumberV != *b.NumberV && !(math.IsNaN(*a.NumberV) && math.IsNaN(*b.NumberV)) {
		return false
	}
	return true
}
