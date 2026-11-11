package dal

import (
	"fmt"
	"strings"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InsertIfAbsent inserts value and reports whether it was created. Unique key
// collisions are a normal outcome for callers that implement idempotency or
// reservations; other database failures are returned unchanged.
func InsertIfAbsent(tx *gorm.DB, value any, conflictColumns ...string) (bool, error) {
	if tx.Dialector.Name() == "mysql" {
		if err := tx.Create(value).Error; err != nil {
			if relational.Is(err, relational.KindUnique) {
				matches, matchErr := mysqlConflictTargetMatches(tx, value, conflictColumns, err)
				if matchErr != nil {
					return false, fmt.Errorf("identify MySQL insert conflict target: %w (insert: %v)", matchErr, err)
				}
				if matches {
					return false, nil
				}
			}
			return false, err
		}
		return true, nil
	}
	onConflict := clause.OnConflict{DoNothing: true}
	for _, column := range conflictColumns {
		onConflict.Columns = append(onConflict.Columns, clause.Column{Name: column})
	}
	result := tx.Clauses(onConflict).Create(value)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func mysqlConflictTargetMatches(tx *gorm.DB, value any, conflictColumns []string, insertErr error) (bool, error) {
	if len(conflictColumns) == 0 {
		return true, nil
	}
	indexes, err := tx.Migrator().GetIndexes(value)
	if err != nil {
		return false, err
	}
	for _, index := range indexes {
		primary, _ := index.PrimaryKey()
		unique, _ := index.Unique()
		if !primary && !unique || !sameColumns(index.Columns(), conflictColumns) {
			continue
		}
		return relational.IsUnique(insertErr, index.Name()), nil
	}
	return false, nil
}

func sameColumns(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	remaining := make(map[string]int, len(left))
	for _, column := range left {
		remaining[strings.ToLower(column)]++
	}
	for _, column := range right {
		key := strings.ToLower(column)
		if remaining[key] == 0 {
			return false
		}
		remaining[key]--
	}
	return true
}
