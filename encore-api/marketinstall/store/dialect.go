package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Dialect selects SQL mechanics only. Operation, step and business ownership do
// not change with the database. A zero value retains the established PG owner.
type Dialect uint8

const (
	PostgreSQL Dialect = iota
	MySQL
)

func (s *Store) Dialect() Dialect { return s.dialect }

func (s *Store) begin(ctx context.Context) (*sql.Tx, error) {
	// A duplicate-key waiter must read the committed winner, and a queue worker
	// must not keep a transaction-wide stale snapshot after acquiring its lock.
	return s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

type queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type rowError struct{ err error }

func (r rowError) Scan(...any) error { return r.err }

var placeholder = regexp.MustCompile(`\$([1-9][0-9]*)`)

// bind accepts only this package's constant SQL. It is not a SQL parser and is
// never exposed to caller-provided SQL. MySQL uses positional question marks,
// so arguments are reordered (and repeated) along with each numbered token.
func (s *Store) bind(query string, args []any) (string, []any, error) {
	if s.dialect == PostgreSQL {
		return query, args, nil
	}
	if s.dialect != MySQL {
		return "", nil, fmt.Errorf("unsupported installation ledger dialect")
	}
	var bound []any
	var failure error
	query = placeholder.ReplaceAllStringFunc(query, func(token string) string {
		n, err := strconv.Atoi(token[1:])
		if err != nil || n < 1 || n > len(args) {
			failure = fmt.Errorf("invalid internal ledger SQL parameter")
			return "?"
		}
		bound = append(bound, args[n-1])
		return "?"
	})
	return query, bound, failure
}

func (s *Store) exec(ctx context.Context, db queryer, query string, args ...any) (sql.Result, error) {
	query, args, err := s.bind(query, args)
	if err != nil {
		return nil, err
	}
	return db.ExecContext(ctx, query, args...)
}

func (s *Store) query(ctx context.Context, db queryer, query string, args ...any) (*sql.Rows, error) {
	query, args, err := s.bind(query, args)
	if err != nil {
		return nil, err
	}
	return db.QueryContext(ctx, query, args...)
}

func (s *Store) queryRow(ctx context.Context, db queryer, query string, args ...any) scanner {
	query, args, err := s.bind(query, args)
	if err != nil {
		return rowError{err}
	}
	return db.QueryRowContext(ctx, query, args...)
}

func (s *Store) jsonParameter(parameter string) string {
	if s.dialect == MySQL {
		return parameter
	}
	return parameter + "::jsonb"
}

func (s *Store) insertOperationSQL() string {
	query := `INSERT INTO market_install_operations(id,tenant_id,subject_id,authorization_ref,request_key,request_digest,artifact_digest,plan,plan_encoding,plan_exact,state`
	if s.dialect == MySQL {
		// MySQL evaluates the generated claim_ready_at before the dynamic
		// created_at default on INSERT. Supply the server time explicitly.
		query += `,bundle_key,bundle_version,created_at`
	}
	// PG infers one shared parameter's type from its JSONB use. Reusing that
	// same parameter for the text column would normalize the exact plan before
	// storage. The compatibility projection and exact text need distinct binds.
	exact := "$9"
	if s.dialect == PostgreSQL {
		exact += "::text"
	}
	query += `) VALUES($1,$2,$3,$4,$5,$6,$7,` + s.jsonParameter("$8") + `,'` + PlanEncodingV1 + `',` + exact + `,'queued'`
	if s.dialect == MySQL {
		query += `,'','',CURRENT_TIMESTAMP(6)`
	}
	query += `)`
	if s.dialect == PostgreSQL {
		query += ` ON CONFLICT(tenant_id,request_key) DO NOTHING`
	}
	return query
}

func (s *Store) claimSQL() string {
	if s.dialect == MySQL {
		// A filesort must not first lock every eligible row and leave another
		// worker with no candidate. The generated due-time index orders the
		// queue and expired leases in the same bounded locking range scan.
		return `SELECT ` + s.operationColumns() + ` FROM market_install_operations FORCE INDEX(market_install_ready)
 WHERE claim_ready_at<=CURRENT_TIMESTAMP(6) ORDER BY claim_ready_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`
	}
	query := `SELECT ` + s.operationColumns() + ` FROM market_install_operations
 WHERE state='queued' OR(state='running' AND lease_until<CURRENT_TIMESTAMP(6)) ORDER BY created_at,id`
	return query + ` FOR UPDATE SKIP LOCKED LIMIT 1`
}

func (s *Store) claimUpdateSQL() string {
	query := `UPDATE market_install_operations SET state='running',generation=$2,lease_until=CURRENT_TIMESTAMP(6)+($3 * INTERVAL '1 second'),updated_at=CURRENT_TIMESTAMP(6) WHERE id=$1 AND generation=$4`
	if s.dialect == MySQL {
		query = strings.Replace(query, `CURRENT_TIMESTAMP(6)+($3 * INTERVAL '1 second')`, `TIMESTAMPADD(SECOND,$3,CURRENT_TIMESTAMP(6))`, 1)
	}
	return query
}
