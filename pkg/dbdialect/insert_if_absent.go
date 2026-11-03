// Package dbdialect holds the few statements whose meaning differs between PostgreSQL and MySQL
// in ways a driver cannot hide.
package dbdialect

import (
	"errors"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const mysqlDuplicateEntry = 1062

// InsertIfAbsent inserts value and reports whether a row was created. An existing row that collides on
// a unique key is not an error: it returns (false, nil), and the caller decides what a replay means.
//
// PostgreSQL uses ON CONFLICT DO NOTHING and the affected-row count. MySQL cannot: gorm renders
// DO NOTHING as ON DUPLICATE KEY UPDATE x=x, and on a connection opened with clientFoundRows (which
// the account queries need so that UPDATE reports matched rows) that statement reports 1 affected row
// for an existing row, the same as for a new one. So MySQL runs a plain INSERT and reads the
// duplicate-key error (1062). A failed INSERT does not abort a MySQL transaction.
//
// conflictColumns names the PostgreSQL conflict target; empty means any unique key.
func InsertIfAbsent(tx *gorm.DB, value any, conflictColumns ...string) (bool, error) {
	if tx.Dialector.Name() == "mysql" {
		if err := tx.Create(value).Error; err != nil {
			var mysqlErr *mysqldriver.MySQLError
			if errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlDuplicateEntry {
				return false, nil
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
