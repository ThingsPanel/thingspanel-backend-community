package initialize

import (
	"gorm.io/gorm"
	componentsql "project/sql"
)

const historyIntentMigrationModule = "history-intent"

// ApplyHistoryIntentSchema registers the storage module's own ledger entry in the
// component runner. It does not adopt or change historical migrations.
func ApplyHistoryIntentSchema(db *gorm.DB) error {
	files, err := componentsql.HistoryIntentMigrations()
	if err != nil {
		return err
	}
	return applyComponentMigrations(db, componentMigration{
		module: historyIntentMigrationModule, description: "telemetry history intents",
		lockKey: int64(0x4849535445494e54), files: files, verify: verifyHistoryIntentSchema,
	})
}

func verifyHistoryIntentSchema(tx *gorm.DB) error {
	for name, expected := range map[string]expectedNativeColumn{
		"tenant_id":     {dataType: "text", nullable: "NO"},
		"device_id":     {dataType: "text", nullable: "NO"},
		"key":           {dataType: "text", nullable: "NO"},
		"ts":            {dataType: "bigint", nullable: "NO"},
		"generation":    {dataType: "bigint", nullable: "NO"},
		"state":         {dataType: "text", nullable: "NO"},
		"attempts":      {dataType: "integer", nullable: "NO"},
		"claimed_until": {dataType: "timestamp with time zone", nullable: "YES"},
		"applied_at":    {dataType: "timestamp with time zone", nullable: "YES"},
	} {
		if err := verifyNativeColumn(tx, "telemetry_history_intents", name, expected); err != nil {
			return err
		}
	}
	return verifyNativeConstraint(tx, "telemetry_history_intents", "telemetry_history_intents_identity", `UNIQUE (device_id, key, ts, generation)`, "u")
}
