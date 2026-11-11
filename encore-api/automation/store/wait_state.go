package store

import (
	"context"
	"errors"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// GetRunTimer returns the durable wait state without consuming or rearming it.
// A fired timer remains evidence that recovery can advance beyond this wait.
func (s *Store) GetRunTimer(ctx context.Context, runID string, pc int) (*RunTimer, error) {
	var dialect gorm.Dialector = postgres.New(postgres.Config{Conn: s.DB})
	if s.MySQL {
		dialect = mysql.New(mysql.Config{Conn: s.DB, SkipInitializeWithVersion: true})
	}
	db, err := gorm.Open(dialect, &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	var timer RunTimer
	err = db.WithContext(ctx).Table("auto_run_timers").Where("run_id = ? AND pc = ?", runID, pc).Take(&timer).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &timer, err
}
