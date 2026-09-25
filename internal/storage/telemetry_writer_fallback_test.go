package storage

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestFallbackInsertMismatchedSliceLengths reproduces the panic reported when a
// flush batch contains several timestamps for the same device: historyData keeps
// every (device_id, key, ts) row while currentData is deduplicated to one row per
// (device_id, key), so len(historyData) > len(currentData). The previous single
// loop indexed currentData with the historyData index and panicked with
// "index out of range". fallbackInsert must walk each slice independently.
func TestFallbackInsertMismatchedSliceLengths(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&TelemetryData{}, &TelemetryCurrentData{}))

	w := &telemetryWriter{db: db}

	num := 1.0
	history := []TelemetryData{
		{DeviceID: "dev-1", Key: "temp", TS: 1, NumberV: &num, TenantID: "t1"},
		{DeviceID: "dev-1", Key: "temp", TS: 2, NumberV: &num, TenantID: "t1"},
		{DeviceID: "dev-1", Key: "temp", TS: 3, NumberV: &num, TenantID: "t1"},
	}
	current := []TelemetryCurrentData{
		{DeviceID: "dev-1", Key: "temp", TS: time.Unix(0, 3*int64(time.Millisecond)), NumberV: &num, TenantID: "t1"},
	}

	written, failed := w.fallbackInsert(history, current)

	require.Equal(t, len(history), written)
	require.Equal(t, 0, failed)

	var historyCount, currentCount int64
	require.NoError(t, db.Model(&TelemetryData{}).Count(&historyCount).Error)
	require.NoError(t, db.Model(&TelemetryCurrentData{}).Count(&currentCount).Error)
	require.Equal(t, int64(len(history)), historyCount)
	require.Equal(t, int64(len(current)), currentCount)
}
