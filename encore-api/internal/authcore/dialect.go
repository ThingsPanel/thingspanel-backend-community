package authcore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// Dialect selects how the account queries, written for PostgreSQL, reach the database.
//
// A MySQL connection must be opened with clientFoundRows=true: the code treats RowsAffected as
// "rows matched" (PostgreSQL), but MySQL by default reports "rows changed", so an UPDATE that sets a
// column to its current value would look like a lost race.
type Dialect int

const (
	DialectPostgres Dialect = iota
	DialectMySQL
)

// unsupportedStatements counts statements that could not be translated for MySQL. It exists for
// tests: a translated code path must leave it unchanged.
var unsupportedStatements atomic.Int64

const unsupportedStatementProbe = "SELECT * FROM tp_unsupported_mysql_statement"

var (
	placeholder = regexp.MustCompile(`\$(\d+)`)
	intervalLit = regexp.MustCompile(`(?i)interval\s+'(\d+)\s+(second|minute|hour|day)s?'`)
	nowCall     = regexp.MustCompile(`(?i)\bnow\(\)`)
	regexOp     = regexp.MustCompile(`(\$\d+)\s+~\s+('(?:[^']|'')*')`)
	splitPart   = regexp.MustCompile(`(?i)split_part\(\s*([a-z_]+)\s*,\s*' '\s*,\s*2\s*\)`)
	onConflict  = regexp.MustCompile(`(?i)\s+ON\s+CONFLICT\s*(?:\([^)]*\))?\s+DO\s+NOTHING`)
	pgOnly      = regexp.MustCompile(`(?i)::|jsonb|\bON\s+CONFLICT\b|\bRETURNING\b|\bDISTINCT\s+ON\b|\bpg_[a-z_]+|\bhashtext|interval\s+'`)
)

// rewrite translates one statement. A statement it does not know how to translate is reported as
// not ok; callers must not run it.
func (d Dialect) rewrite(query string, args []any) (string, []any, bool) {
	if d == DialectPostgres {
		return query, args, true
	}
	query = onConflict.ReplaceAllString(query, " ON DUPLICATE KEY UPDATE id=id")
	query = regexOp.ReplaceAllString(query, "$1 REGEXP $2")
	query = splitPart.ReplaceAllString(query, "IF(LOCATE(' ',$1)>0,SUBSTRING_INDEX(SUBSTRING_INDEX($1,' ',2),' ',-1),'')")
	query = intervalLit.ReplaceAllStringFunc(query, func(m string) string {
		parts := intervalLit.FindStringSubmatch(m)
		return "INTERVAL " + parts[1] + " " + strings.ToUpper(parts[2])
	})
	query = nowCall.ReplaceAllString(query, "NOW(6)")
	if pgOnly.MatchString(query) {
		return "", nil, false
	}
	var out []any
	var bad bool
	var rebuilt strings.Builder
	// Placeholders inside single-quoted literals are text, not parameters.
	for _, part := range splitLiterals(query) {
		if part.literal {
			rebuilt.WriteString(part.text)
			continue
		}
		rebuilt.WriteString(placeholder.ReplaceAllStringFunc(part.text, func(m string) string {
			n, err := strconv.Atoi(m[1:])
			if err != nil || n < 1 || n > len(args) {
				bad = true
				return m
			}
			out = append(out, args[n-1])
			return "?"
		}))
	}
	query = rebuilt.String()
	if bad {
		return "", nil, false
	}
	return query, out, true
}

type queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// dialectDB applies Dialect.rewrite to every statement. For PostgreSQL it is a pass-through.
type dialectDB struct {
	inner   queryer
	db      *sql.DB
	dialect Dialect
}

func (x dialectDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	q, a, ok := x.dialect.rewrite(query, args)
	if !ok {
		unsupportedStatements.Add(1)
		return x.inner.ExecContext(ctx, unsupportedStatementProbe)
	}
	return x.inner.ExecContext(ctx, q, a...)
}

func (x dialectDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q, a, ok := x.dialect.rewrite(query, args)
	if !ok {
		unsupportedStatements.Add(1)
		return x.inner.QueryContext(ctx, unsupportedStatementProbe)
	}
	return x.inner.QueryContext(ctx, q, a...)
}

func (x dialectDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	q, a, ok := x.dialect.rewrite(query, args)
	if !ok {
		unsupportedStatements.Add(1)
		return x.inner.QueryRowContext(ctx, unsupportedStatementProbe)
	}
	return x.inner.QueryRowContext(ctx, q, a...)
}

// dialectTx is a transaction whose statements are translated.
type dialectTx struct {
	dialectDB
	tx *sql.Tx
}

func (t dialectTx) Commit() error   { return t.tx.Commit() }
func (t dialectTx) Rollback() error { return t.tx.Rollback() }

func (x dialectDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (dialectTx, error) {
	tx, err := x.db.BeginTx(ctx, opts)
	if err != nil {
		return dialectTx{}, err
	}
	return dialectTx{dialectDB: dialectDB{inner: tx, db: x.db, dialect: x.dialect}, tx: tx}, nil
}

type queryPart struct {
	text    string
	literal bool
}

// splitLiterals separates single-quoted literals (” is an escaped quote) from the rest.
func splitLiterals(query string) []queryPart {
	var parts []queryPart
	start, inLiteral := 0, false
	for i := 0; i < len(query); i++ {
		if query[i] != '\'' {
			continue
		}
		if inLiteral && i+1 < len(query) && query[i+1] == '\'' {
			i++
			continue
		}
		if !inLiteral {
			if i > start {
				parts = append(parts, queryPart{text: query[start:i]})
			}
			start, inLiteral = i, true
		} else {
			parts = append(parts, queryPart{text: query[start : i+1], literal: true})
			start, inLiteral = i+1, false
		}
	}
	if start < len(query) {
		parts = append(parts, queryPart{text: query[start:], literal: inLiteral})
	}
	return parts
}

// beginLocked starts a transaction that holds a named mutex until release is called.
//
// PostgreSQL: pgLockSQL (one "$1" placeholder) runs once per name inside the transaction, as before; the
// locks end with the transaction. MySQL has no transaction-scoped lock, so each name becomes a GET_LOCK on
// the connection that carries the transaction (names are hashed to MySQL's 64-character limit), released
// by release after the transaction has ended. release must always be called, also after a commit.
func (x dialectDB) beginLocked(ctx context.Context, pgLockSQL string, names ...string) (tx dialectTx, release func(), err error) {
	if x.dialect == DialectPostgres {
		tx, err = x.BeginTx(ctx, nil)
		if err != nil {
			return dialectTx{}, nil, err
		}
		for _, name := range names {
			if _, err = tx.tx.ExecContext(ctx, pgLockSQL, name); err != nil {
				_ = tx.Rollback()
				return dialectTx{}, nil, err
			}
		}
		return tx, func() { _ = tx.Rollback() }, nil
	}
	conn, err := x.db.Conn(ctx)
	if err != nil {
		return dialectTx{}, nil, err
	}
	var held []string
	releaseAll := func() {
		for _, name := range held {
			_, _ = conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", name)
		}
		_ = conn.Close()
	}
	for _, name := range names {
		digest := sha256.Sum256([]byte(name))
		lock := hex.EncodeToString(digest[:]) // 64 characters
		var got sql.NullInt64
		if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 10)", lock).Scan(&got); err != nil {
			releaseAll()
			return dialectTx{}, nil, err
		}
		if !got.Valid || got.Int64 != 1 {
			releaseAll()
			return dialectTx{}, nil, errors.New("named lock not obtained")
		}
		held = append(held, lock)
	}
	raw, err := conn.BeginTx(ctx, nil)
	if err != nil {
		releaseAll()
		return dialectTx{}, nil, err
	}
	tx = dialectTx{dialectDB: dialectDB{inner: raw, db: x.db, dialect: x.dialect}, tx: raw}
	return tx, func() { _ = raw.Rollback(); releaseAll() }, nil
}
