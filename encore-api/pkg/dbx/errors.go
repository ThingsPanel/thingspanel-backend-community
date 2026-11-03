package dbx

import (
	"errors"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"encore.dev/storage/sqldb"
	"encore.dev/storage/sqldb/sqlerr"
)

// pgCode and pgConstraint read a PostgreSQL error from either source: a pgx error (a plain pgx/stdlib pool, which
// is also what tests outside the Encore runtime use) or Encore's own error type. Encore's helpers panic outside
// its runtime, so they are only reached for errors that are not pgx errors, and guarded.
func pgError(err error) (code, constraint string, ok bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code, pgErr.ConstraintName, true
	}
	defer func() {
		if recover() != nil {
			code, constraint, ok = "", "", false
		}
	}()
	switch sqldb.ErrCode(err) {
	case sqlerr.UniqueViolation:
		code = "23505"
	case sqlerr.ForeignKeyViolation:
		code = "23503"
	default:
		return "", "", false
	}
	var sqlErr *sqldb.Error
	if errors.As(err, &sqlErr) {
		constraint = sqlErr.ConstraintName
	}
	return code, constraint, true
}

// IsUniqueViolation reports whether err is a duplicate-key error. When constraint is not empty it must also name the
// violated constraint (PostgreSQL) or index (MySQL, in the "table.index" form of its message).
func IsUniqueViolation(err error, constraint string) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		if mysqlErr.Number != 1062 {
			return false
		}
		return constraint == "" || strings.Contains(mysqlErr.Message, "."+constraint+"'") || strings.Contains(mysqlErr.Message, "'"+constraint+"'")
	}
	code, name, ok := pgError(err)
	if !ok || code != "23505" {
		return false
	}
	return constraint == "" || name == constraint
}

// IsForeignKeyViolation reports a violated foreign key on either database.
func IsForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1451 || mysqlErr.Number == 1452
	}
	code, _, ok := pgError(err)
	return ok && code == "23503"
}
