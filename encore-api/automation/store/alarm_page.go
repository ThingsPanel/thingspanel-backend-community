package store

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// EpisodeFilter applies before counting and paginating. Tenant scope is always
// supplied separately by the authenticated service, never by the request body.
type EpisodeFilter struct {
	PolicyID, SubjectID, State, Level string
	StartTime, EndTime                *time.Time
	Limit, Offset                     int
}

func (s *Store) ListEpisodesPage(ctx context.Context, tenantID string, f EpisodeFilter) ([]*Episode, int64, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	var dialect gorm.Dialector = postgres.New(postgres.Config{Conn: s.DB})
	if s.MySQL {
		dialect = mysql.New(mysql.Config{Conn: s.DB, SkipInitializeWithVersion: true})
	}
	db, err := gorm.Open(dialect, &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, 0, err
	}
	items := make([]*Episode, 0)
	var total int64
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		base := func() *gorm.DB {
			q := tx.Table("alarm_episodes").Where("tenant_id = ?", tenantID)
			if f.PolicyID != "" {
				q = q.Where("(policy_id = ? OR policy_key = ?)", f.PolicyID, f.PolicyID)
			}
			if f.SubjectID != "" {
				q = q.Where("subject_id = ?", f.SubjectID)
			}
			if f.State != "" {
				q = q.Where("state = ?", f.State)
			}
			if f.Level != "" {
				q = q.Where("level = ?", f.Level)
			}
			if f.StartTime != nil {
				q = q.Where("opened_at >= ?", *f.StartTime)
			}
			if f.EndTime != nil {
				q = q.Where("opened_at <= ?", *f.EndTime)
			}
			return q
		}
		if err := base().Count(&total).Error; err != nil {
			return err
		}
		rows, err := base().Select(episodeColumns).Order("opened_at DESC").Order("id DESC").Limit(f.Limit).Offset(f.Offset).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEpisode(rows)
			if err != nil {
				return err
			}
			items = append(items, e)
		}
		return rows.Err()
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	return items, total, err
}
