package automationguard

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational/reltest"
)

func TestAutomationGuardPostgresMySQL(t *testing.T) {
	reltest.Run(t, "automation-guard-concurrency", func(s *reltest.Scenario) {
		s.Step("create isolated guard schema", func(ctx context.Context, db *gorm.DB) (any, error) {
			if err := db.WithContext(ctx).Exec(`CREATE TABLE automation_scope_generation (
				scope_key VARCHAR(255) PRIMARY KEY, generation BIGINT NOT NULL, updated_at TIMESTAMP NOT NULL)`).Error; err != nil {
				return nil, err
			}
			leaseType := "TIMESTAMP"
			if relational.MustDialectOf(db) == relational.MySQL {
				leaseType = "DATETIME(6)"
			}
			return nil, db.WithContext(ctx).Exec(`CREATE TABLE automation_command_guard (
				idempotency_key VARCHAR(255) PRIMARY KEY, scope_key VARCHAR(255) NOT NULL, generation BIGINT NOT NULL,
				action_kind VARCHAR(32) NOT NULL, device_id VARCHAR(64) NOT NULL, payload_hash CHAR(64) NOT NULL,
				state VARCHAR(24) NOT NULL, lease_until ` + leaseType + ` NULL, created_at ` + leaseType + ` NOT NULL,
				updated_at ` + leaseType + ` NOT NULL)`).Error
		})
		s.Step("generation remains monotonic and one worker reserves the key", func(ctx context.Context, db *gorm.DB) (any, error) {
			db = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
			var wg sync.WaitGroup
			bumpErrors := make(chan error, 8)
			for generation := int64(1); generation <= 8; generation++ {
				wg.Add(1)
				go func(value int64) {
					defer wg.Done()
					if _, err := bumpScopeGeneration(db, "scope-a2", value); err != nil {
						bumpErrors <- err
					}
				}(generation)
			}
			wg.Wait()
			close(bumpErrors)
			for err := range bumpErrors {
				return nil, fmt.Errorf("bump generation: %w", err)
			}
			var generation int64
			if err := db.Raw(`SELECT generation FROM automation_scope_generation WHERE scope_key = ?`, "scope-a2").Scan(&generation).Error; err != nil {
				return nil, err
			}
			if generation != 8 {
				return nil, fmt.Errorf("maximum generation = %d, want 8", generation)
			}
			type reservationResult struct {
				inserted bool
				err      error
			}
			results := make(chan reservationResult, 8)
			for worker := 0; worker < 8; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					inserted, err := reserveKey(db, "same-key", "scope-a2", 8, "command", "device-a2", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
					results <- reservationResult{inserted: inserted, err: err}
				}()
			}
			wg.Wait()
			close(results)
			fresh, duplicate, failures := 0, 0, 0
			for result := range results {
				if result.err != nil {
					failures++
				} else if result.inserted {
					fresh++
				} else {
					duplicate++
				}
			}
			if fresh != 1 || duplicate != 7 || failures != 0 {
				return nil, fmt.Errorf("concurrent reserve: fresh=%d duplicate=%d failures=%d, want 1/7/0", fresh, duplicate, failures)
			}
			var rows int64
			if err := db.Table("automation_command_guard").Where("idempotency_key = ?", "same-key").Count(&rows).Error; err != nil {
				return nil, err
			}
			if rows != 1 {
				return nil, fmt.Errorf("persisted reservation rows=%d, want 1", rows)
			}
			return fmt.Sprintf("generation=%d fresh=%d duplicate=%d failures=%d rows=%d", generation, fresh, duplicate, failures, rows), nil
		})
	})
}
