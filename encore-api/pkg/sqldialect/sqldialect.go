// Package sqldialect lets stores written for PostgreSQL ($n placeholders, a few PostgreSQL idioms) run on
// MySQL 8 through a small, fail-closed translator. A statement it cannot translate is never sent: the
// caller receives an error from a probe statement that cannot succeed, and Unsupported counts it so tests
// can assert that a translated code path never hit one.
//
// A MySQL connection used with this package must be opened with parseTime=true and clientFoundRows=true:
// stores treat RowsAffected as "rows matched", as PostgreSQL reports it.
package sqldialect

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// Dialect selects the translation. The zero value is PostgreSQL and passes statements through untouched.
type Dialect int

const (
	Postgres Dialect = iota
	MySQL
)

func (d Dialect) String() string {
	if d == MySQL {
		return "mysql"
	}
	return "postgres"
}

// Unsupported counts statements that could not be translated.
var Unsupported atomic.Int64

const probe = "SELECT * FROM tp_unsupported_mysql_statement"

var (
	placeholder    = regexp.MustCompile(`\$(\d+)`)
	paramCast      = regexp.MustCompile(`(?i)(\$\d+)::(?:text|bigint|int|integer|smallint|numeric|jsonb|json|timestamptz|timestamp|varchar|boolean)\b`)
	emptyJSON      = regexp.MustCompile(`(?i)'\{\}'::jsonb`)
	emptyJSONArray = regexp.MustCompile(`(?i)'\[\]'::jsonb`)
	nullJSON       = regexp.MustCompile(`(?i)'null'::jsonb`)
	nullifCast     = regexp.MustCompile(`(?i)(\bNULLIF\(\$\d+\s*,\s*''\))::(?:text|uuid|jsonb|json)\b`)
	// key and signal are reserved words in MySQL but plain column names in the PostgreSQL-style statements.
	reservedColumn    = regexp.MustCompile("`?\\b(?:key|signal)\\b`?")
	identCast         = regexp.MustCompile(`(?i)\b([A-Za-z_][A-Za-z0-9_.]*)::(?:text|uuid|jsonb|json)\b`)
	jsonText          = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*)\s*->>\s*'([A-Za-z0-9_]+)'`)
	jsonValue         = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*)\s*->\s*'([A-Za-z0-9_]+)'`)
	upsertSet         = regexp.MustCompile(`(?is)\s+ON\s+CONFLICT\s*\([^)]*\)\s+DO\s+UPDATE\s+SET\s+(.*?)\s*$`)
	excludedRef       = regexp.MustCompile(`(?i)\bEXCLUDED\.([A-Za-z_][A-Za-z0-9_]*)`)
	intervalLit       = regexp.MustCompile(`(?i)interval\s+'(\d+)\s+(second|minute|hour|day)s?'`)
	nowCall           = regexp.MustCompile(`(?i)\bnow\(\)`)
	insertFirstColumn = regexp.MustCompile(`(?is)INSERT\s+INTO\s+\w+\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)
	onConflict        = regexp.MustCompile(`(?i)\s+ON\s+CONFLICT\s*(?:\([^)]*\))?\s+DO\s+NOTHING`)
	pgOnly            = regexp.MustCompile(`(?i)::|jsonb|\bON\s+CONFLICT\b|\bRETURNING\b|\bDISTINCT\s+ON\b|\bpg_[a-z_]+|\bhashtext|interval\s+'|\bILIKE\b|\bcurrent_schema\(|=\s*ANY\s*\(|\bunnest\(|\barray_agg\(|\bstring_agg\(|\bgenerate_series\(|\bjsonb_[a-z_]+\(|\bto_jsonb\(`)
)

// Rewrite translates one statement for the dialect. ok is false when it cannot be translated.
func (d Dialect) Rewrite(query string, args []any) (string, []any, bool) {
	q, idx, ok := d.RewriteIndexed(query, len(args))
	if !ok {
		return "", nil, false
	}
	if d == Postgres {
		return q, args, true
	}
	out := make([]any, len(idx))
	for i, n := range idx {
		out[i] = args[n]
	}
	return q, out, true
}

// RewriteIndexed translates one statement and reports which of the caller's arguments (0-based) each
// placeholder of the result takes, in order. For PostgreSQL the statement is returned untouched and idx is nil.
func (d Dialect) RewriteIndexed(query string, argCount int) (string, []int, bool) {
	if d == Postgres {
		return query, nil, true
	}
	// DO NOTHING becomes a no-op assignment of the first inserted column (the table may have no "id").
	if loc := onConflict.FindStringIndex(query); loc != nil {
		first := insertFirstColumn.FindStringSubmatch(query)
		if first == nil {
			return "", nil, false
		}
		query = query[:loc[0]] + " ON DUPLICATE KEY UPDATE " + first[1] + "=" + first[1] + query[loc[1]:]
	}
	// ON CONFLICT (...) DO UPDATE SET a=EXCLUDED.a becomes ON DUPLICATE KEY UPDATE a=VALUES(a). A conditional
	// update (WHERE) or anything else after the assignments stays untranslated and is refused below.
	query = upsertSet.ReplaceAllStringFunc(query, func(m string) string {
		set := upsertSet.FindStringSubmatch(m)[1]
		if regexp.MustCompile(`(?i)\bWHERE\b|\bRETURNING\b`).MatchString(set) {
			return m
		}
		return " ON DUPLICATE KEY UPDATE " + excludedRef.ReplaceAllString(set, "VALUES($1)")
	})
	query = nullJSON.ReplaceAllString(query, "CAST('null' AS JSON)")
	query = nullifCast.ReplaceAllString(query, "$1")
	query = emptyJSON.ReplaceAllString(query, "JSON_OBJECT()")
	query = emptyJSONArray.ReplaceAllString(query, "JSON_ARRAY()")
	query = paramCast.ReplaceAllString(query, "$1")
	// uuid columns are CHAR(36) and jsonb columns JSON in the MySQL schema, so casting a column to them changes nothing.
	query = identCast.ReplaceAllString(query, "$1")
	query = jsonText.ReplaceAllString(query, "JSON_UNQUOTE(JSON_EXTRACT($1, '$$.$2'))")
	query = jsonValue.ReplaceAllString(query, "JSON_EXTRACT($1, '$$.$2')")
	query = intervalLit.ReplaceAllStringFunc(query, func(m string) string {
		parts := intervalLit.FindStringSubmatch(m)
		return "INTERVAL " + parts[1] + " " + strings.ToUpper(parts[2])
	})
	query = nowCall.ReplaceAllString(query, "NOW(6)")
	if pgOnly.MatchString(query) {
		return "", nil, false
	}
	var idx []int
	bad := false
	var rebuilt strings.Builder
	for _, part := range splitLiterals(query) {
		if part.literal {
			rebuilt.WriteString(part.text)
			continue
		}
		rebuilt.WriteString(placeholder.ReplaceAllStringFunc(quoteReserved(part.text), func(m string) string {
			n, err := strconv.Atoi(m[1:])
			if err != nil || n < 1 || n > argCount {
				bad = true
				return m
			}
			idx = append(idx, n-1)
			return "?"
		}))
	}
	if bad {
		return "", nil, false
	}
	if idx == nil {
		idx = []int{}
	}
	return rebuilt.String(), idx, true
}

type part struct {
	text    string
	literal bool
}

// splitLiterals separates single-quoted literals (” is an escaped quote) from the rest.
func splitLiterals(query string) []part {
	var parts []part
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
				parts = append(parts, part{text: query[start:i]})
			}
			start, inLiteral = i, true
		} else {
			parts = append(parts, part{text: query[start : i+1], literal: true})
			start, inLiteral = i+1, false
		}
	}
	if start < len(query) {
		parts = append(parts, part{text: query[start:], literal: inLiteral})
	}
	return parts
}

// Queryer is what *sql.DB, *sql.Tx and *sql.Conn offer.
type Queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// DB translates every statement before it reaches Inner.
type DB struct {
	Inner   Queryer
	Dialect Dialect
}

func (x DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	q, a, ok := x.Dialect.Rewrite(query, args)
	if !ok {
		Unsupported.Add(1)
		return x.Inner.ExecContext(ctx, probe)
	}
	return x.Inner.ExecContext(ctx, q, a...)
}

func (x DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q, a, ok := x.Dialect.Rewrite(query, args)
	if !ok {
		Unsupported.Add(1)
		return x.Inner.QueryContext(ctx, probe)
	}
	return x.Inner.QueryContext(ctx, q, a...)
}

func (x DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	q, a, ok := x.Dialect.Rewrite(query, args)
	if !ok {
		Unsupported.Add(1)
		return x.Inner.QueryRowContext(ctx, probe)
	}
	return x.Inner.QueryRowContext(ctx, q, a...)
}

// Tx is a transaction whose statements are translated.
type Tx struct {
	DB
	tx *sql.Tx
}

func (t Tx) Commit() error   { return t.tx.Commit() }
func (t Tx) Rollback() error { return t.tx.Rollback() }

// Begin starts a transaction on db.
func Begin(ctx context.Context, db *sql.DB, d Dialect, opts *sql.TxOptions) (Tx, error) {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return Tx{}, err
	}
	return Tx{DB: DB{Inner: tx, Dialect: d}, tx: tx}, nil
}

// quoteReserved backquotes bare key/signal column names and leaves already quoted ones alone.
func quoteReserved(text string) string {
	return reservedColumn.ReplaceAllStringFunc(text, func(m string) string {
		if strings.HasPrefix(m, "`") {
			return m
		}
		return "`" + m + "`"
	})
}

// WithoutOnConflictDoNothing removes a trailing "ON CONFLICT [(cols)] DO NOTHING" so the caller can run a plain
// INSERT and read the duplicate-key error. With clientFoundRows an ON DUPLICATE KEY no-op reports one affected
// row, exactly as a real insert does, so the affected-row count cannot tell "created" from "already there".
func WithoutOnConflictDoNothing(query string) (string, bool) {
	loc := onConflict.FindStringIndex(query)
	if loc == nil || strings.TrimSpace(query[loc[1]:]) != "" {
		return query, false
	}
	return query[:loc[0]], true
}
