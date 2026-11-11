package dal

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// BumpAutomationScopeGeneration persists the greatest generation observed for
// a scope. The upsert is atomic on both supported relational engines.
func BumpAutomationScopeGeneration(ctx context.Context, db *gorm.DB, scope string, generation int64) error {
	if db == nil || scope == "" || generation < 0 {
		return errors.New("automation generation database input is invalid")
	}
	statement := `INSERT INTO automation_scope_generation(scope_key,generation,updated_at)
		VALUES(?,?,now()) ON CONFLICT(scope_key) DO UPDATE SET
		generation=GREATEST(automation_scope_generation.generation,excluded.generation),updated_at=now()`
	if db.Dialector.Name() == "mysql" {
		statement = `INSERT INTO automation_scope_generation(scope_key,generation,updated_at)
			VALUES(?,?,now()) ON DUPLICATE KEY UPDATE
			generation=GREATEST(generation,VALUES(generation)),updated_at=now()`
	}
	return db.WithContext(ctx).Exec(statement, scope, generation).Error
}
