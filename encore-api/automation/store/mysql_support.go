package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"

	"encore.app/pkg/sqldialect"
)

// MySQL has neither UPDATE ... FROM nor RETURNING, so the claim statements lock the candidate rows
// with SKIP LOCKED, change exactly those rows, and read them back inside one transaction.

// lockIDs runs a SELECT of one id column ... FOR UPDATE SKIP LOCKED.
func lockIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// placeholders renders "$start,$start+1,..." for n values and appends them to args.
func placeholders(args []any, values []string) (string, []any) {
	marks := make([]string, len(values))
	for i, v := range values {
		args = append(args, v)
		marks[i] = fmt.Sprintf("$%d", len(args))
	}
	return strings.Join(marks, ","), args
}

// isDuplicateKey reports MySQL error 1062 (a unique key already holds the row).
func isDuplicateKey(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

// onceResult is the affected-row answer of an INSERT ... ON CONFLICT DO NOTHING on MySQL.
type onceResult int64

func (onceResult) LastInsertId() (int64, error)   { return 0, errors.New("not supported") }
func (r onceResult) RowsAffected() (int64, error) { return int64(r), nil }

// execOnce runs an INSERT ... ON CONFLICT DO NOTHING and reports through RowsAffected whether a row was created
// (1) or already existed (0). PostgreSQL does that natively. MySQL cannot (see sqldialect.WithoutOnConflictDoNothing),
// so the conflict clause is dropped and error 1062 means "already there". The duplicate may be on any unique key
// of the table, not only the PostgreSQL conflict target; the automation tables that use this have just one.
func (s *Store) execOnce(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	if !s.MySQL {
		return s.exec(ctx, tx, query, args...)
	}
	plain, ok := sqldialect.WithoutOnConflictDoNothing(query)
	if !ok {
		return s.exec(ctx, tx, query, args...)
	}
	if _, err := s.exec(ctx, tx, plain, args...); err != nil {
		if isDuplicateKey(err) {
			return onceResult(0), nil
		}
		return nil, err
	}
	return onceResult(1), nil
}
