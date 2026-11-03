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
	"sync"
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
	paramCast      = regexp.MustCompile(`(?i)(\$\d+|\?)::(?:text|bigint|int|integer|smallint|numeric|jsonb|json|timestamptz|timestamp|varchar|boolean)\b`)
	emptyJSON      = regexp.MustCompile(`(?i)'\{\}'::jsonb`)
	emptyJSONArray = regexp.MustCompile(`(?i)'\[\]'::jsonb`)
	nullJSON       = regexp.MustCompile(`(?i)'null'::jsonb`)
	nullifCast     = regexp.MustCompile(`(?i)(\bNULLIF\((?:\$\d+|\?)\s*,\s*''\))::(?:text|uuid|jsonb|json)\b`)
	// key and signal are reserved words in MySQL but plain column names in the PostgreSQL-style statements.
	reservedColumn    = regexp.MustCompile("`?\\b(?:key|signal)\\b`?")
	identCast         = regexp.MustCompile(`(?i)\b([A-Za-z_][A-Za-z0-9_.]*)::(?:text|uuid|jsonb|json)\b`)
	jsonText          = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*)\s*->>\s*'([A-Za-z0-9_]+)'`)
	jsonValue         = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*)\s*->\s*'([A-Za-z0-9_]+)'`)
	upsertSet         = regexp.MustCompile(`(?is)\s+ON\s+CONFLICT\s*\([^)]*\)\s+DO\s+UPDATE\s+SET\s+(.*?)\s*$`)
	excludedRef       = regexp.MustCompile(`(?i)\bEXCLUDED\.([A-Za-z_][A-Za-z0-9_]*)`)
	intervalLit       = regexp.MustCompile(`(?i)interval\s+'(\d+)\s+(second|minute|hour|day)s?'`)
	makeInterval      = regexp.MustCompile(`(?i)make_interval\(\s*(secs|mins|hours|days)\s*=>\s*(\?|\$\d+)\s*\)`)
	noopDuplicate     = regexp.MustCompile("(?i)\\s+ON\\s+DUPLICATE\\s+KEY\\s+UPDATE\\s+`?(\\w+)`?\\s*=\\s*`?(\\w+)`?\\s*$")
	advisoryLock      = regexp.MustCompile(`(?is)^\s*SELECT\s+pg_advisory_xact_lock\((.*)\)\s*;?\s*$`)
	nowCall           = regexp.MustCompile(`(?i)\bnow\(\)`)
	insertFirstColumn = regexp.MustCompile(`(?is)INSERT\s+INTO\s+\w+\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)
	onConflict        = regexp.MustCompile(`(?i)\s+ON\s+CONFLICT\s*(?:\([^)]*\))?\s+DO\s+NOTHING`)
	pgOnly            = regexp.MustCompile(`(?i)::|jsonb|\bON\s+CONFLICT\b|\bRETURNING\b|\bDISTINCT\s+ON\b|\bpg_[a-z_]+|\bhashtext|interval\s+'|\bILIKE\b|\bcurrent_schema\(|=\s*ANY\s*\(|\bunnest\(|\barray_agg\(|\bstring_agg\(|\bgenerate_series\(|\bjsonb_[a-z_]+\(|\bto_jsonb\(|\bFILTER\s*\(`)
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
	t, ok := d.Translate(query, argCount)
	return t.Query, t.Idx, ok
}

// Translation is one translated statement.
type Translation struct {
	Query string
	// Idx maps each placeholder of Query to the caller's argument (0-based).
	Idx []int
	// IgnoreDuplicate is set for a single-row INSERT whose PostgreSQL form was ON CONFLICT DO NOTHING. Query is then
	// a plain INSERT, and the caller must report a duplicate-key error (1062) as "0 rows affected, no error": with
	// clientFoundRows an ON DUPLICATE KEY no-op would report 1 row, indistinguishable from a real insert.
	IgnoreDuplicate bool
}

type cacheKey struct {
	query    string
	argCount int
}

type cacheValue struct {
	t  Translation
	ok bool
}

var (
	translationCache     sync.Map
	translationCacheSize atomic.Int64
)

const translationCacheLimit = 4096

// Translate is RewriteIndexed with the extra information the driver needs. Results are cached: statements
// are a small, fixed set, but a dynamic IN list produces many, so the cache is dropped when it grows large.
func (d Dialect) Translate(query string, argCount int) (Translation, bool) {
	if d == Postgres {
		return Translation{Query: query}, true
	}
	key := cacheKey{query, argCount}
	if v, ok := translationCache.Load(key); ok {
		c := v.(cacheValue)
		return c.t, c.ok
	}
	t, ok := d.translate(query, argCount)
	if translationCacheSize.Add(1) > translationCacheLimit {
		translationCache.Range(func(k, _ any) bool { translationCache.Delete(k); return true })
		translationCacheSize.Store(1)
	}
	translationCache.Store(key, cacheValue{t, ok})
	return t, ok
}

func (d Dialect) translate(query string, argCount int) (Translation, bool) {
	fail := Translation{}
	ignoreDuplicate := false
	rewritten := rewriteAdvisoryLock(query)
	isLock := rewritten != query
	query = rewritten
	// DO NOTHING on a single-row insert becomes a plain INSERT whose duplicate-key error the driver turns into
	// "0 rows". Anything else becomes a no-op assignment of the first inserted column (the table may have no "id").
	if loc := onConflict.FindStringIndex(query); loc != nil {
		first := insertFirstColumn.FindStringSubmatch(query)
		if first == nil {
			return fail, false
		}
		if strings.TrimSpace(query[loc[1]:]) == "" && singleRowInsert(query[:loc[0]]) {
			query = query[:loc[0]]
			ignoreDuplicate = true
		} else {
			query = query[:loc[0]] + " ON DUPLICATE KEY UPDATE " + first[1] + "=" + first[1] + query[loc[1]:]
		}
	} else if m := noopDuplicate.FindStringSubmatchIndex(query); !isLock && m != nil && query[m[2]:m[3]] == query[m[4]:m[5]] && singleRowInsert(query[:m[0]]) {
		// gorm renders clause.OnConflict{DoNothing: true} for MySQL as ON DUPLICATE KEY UPDATE `c`=`c`.
		query = query[:m[0]]
		ignoreDuplicate = true
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
	query = rewriteCountFilter(query)
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
	query = makeInterval.ReplaceAllStringFunc(query, func(m string) string {
		parts := makeInterval.FindStringSubmatch(m)
		unit := map[string]string{"secs": "SECOND", "mins": "MINUTE", "hours": "HOUR", "days": "DAY"}[strings.ToLower(parts[1])]
		return "INTERVAL " + parts[2] + " " + unit
	})
	query = nowCall.ReplaceAllString(query, "NOW(6)")
	if pgOnly.MatchString(query) {
		return fail, false
	}
	var idx []int
	bad := false
	dollars, questions := 0, 0
	var rebuilt strings.Builder
	for _, part := range splitLiterals(query) {
		if part.literal {
			rebuilt.WriteString(part.text)
			continue
		}
		questions += strings.Count(part.text, "?")
		rebuilt.WriteString(placeholder.ReplaceAllStringFunc(quoteReserved(part.text), func(m string) string {
			n, err := strconv.Atoi(m[1:])
			if err != nil || n < 1 || n > argCount {
				bad = true
				return m
			}
			dollars++
			idx = append(idx, n-1)
			return "?"
		}))
	}
	if bad || (dollars > 0 && questions > 0) {
		return fail, false
	}
	if dollars == 0 {
		// MySQL-style placeholders (gorm): the arguments pass through in order.
		idx = make([]int, questions)
		for i := range idx {
			idx[i] = i
		}
	}
	if idx == nil {
		idx = []int{}
	}
	return Translation{Query: rebuilt.String(), Idx: idx, IgnoreDuplicate: ignoreDuplicate}, true
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

var countFilter = regexp.MustCompile(`(?i)\bcount\(\*\)\s+FILTER\s*\(\s*WHERE\s+`)

// rewriteCountFilter turns count(*) FILTER (WHERE cond) into count(CASE WHEN cond THEN 1 END), which both
// databases evaluate the same way. Any other use of FILTER stays and is refused.
func rewriteCountFilter(query string) string {
	for {
		loc := countFilter.FindStringIndex(query)
		if loc == nil {
			return query
		}
		depth, end := 1, -1
		for i := loc[1]; i < len(query) && end < 0; i++ {
			switch query[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i
				}
			}
		}
		if end < 0 {
			return query
		}
		query = query[:loc[0]] + "count(CASE WHEN " + query[loc[1]:end] + " THEN 1 END)" + query[end+1:]
	}
}

// singleRowInsert reports whether the INSERT has exactly one VALUES tuple (and is not INSERT ... SELECT).
func singleRowInsert(insert string) bool {
	loc := regexp.MustCompile(`(?i)\bVALUES\s*`).FindStringIndex(insert)
	if loc == nil {
		return false
	}
	rest := insert[loc[1]:]
	tuples, depth, inLiteral := 0, 0, false
	for i := 0; i < len(rest); i++ {
		switch c := rest[i]; {
		case c == '\'':
			if inLiteral && i+1 < len(rest) && rest[i+1] == '\'' {
				i++
			} else {
				inLiteral = !inLiteral
			}
		case inLiteral:
		case c == '(':
			if depth == 0 {
				tuples++
			}
			depth++
		case c == ')':
			depth--
		case depth == 0 && c != ' ' && c != '\n' && c != '\t' && c != ',' && c != '\r':
			return tuples == 1 // anything else after the tuples (a clause) ends the VALUES list
		}
	}
	return tuples == 1
}

// rewriteAdvisoryLock turns pg_advisory_xact_lock(hashtext(k)) (any of its spellings) into a statement that
// takes an exclusive row lock in tp_advisory_locks. The row lock is held until the transaction ends, exactly
// like the PostgreSQL lock, and is taken by one statement so Exec/Raw callers need no change.
func rewriteAdvisoryLock(query string) string {
	m := advisoryLock.FindStringSubmatch(query)
	if m == nil {
		return query
	}
	var keys []string
	for _, arg := range splitTopLevel(m[1]) {
		arg = strings.TrimSpace(arg)
		lower := strings.ToLower(arg)
		switch {
		case strings.HasPrefix(lower, "hashtextextended(") && strings.HasSuffix(arg, ")"):
			inner := splitTopLevel(arg[len("hashtextextended(") : len(arg)-1])
			keys = append(keys, strings.TrimSpace(inner[0]))
		case strings.HasPrefix(lower, "hashtext(") && strings.HasSuffix(arg, ")"):
			keys = append(keys, strings.TrimSpace(arg[len("hashtext("):len(arg)-1]))
		default:
			keys = append(keys, arg)
		}
	}
	for i, k := range keys {
		if regexp.MustCompile(`^-?\d+$`).MatchString(k) {
			keys[i] = "'" + k + "'"
		}
	}
	key := keys[0]
	if len(keys) > 1 {
		key = "CONCAT_WS(CHAR(31), " + strings.Join(keys, ", ") + ")"
	}
	return "INSERT INTO tp_advisory_locks (lock_key) VALUES (SHA2(" + key + ", 256)) ON DUPLICATE KEY UPDATE lock_key = lock_key"
}

func splitTopLevel(s string) []string {
	var parts []string
	depth, start, inLiteral := 0, 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'':
			inLiteral = !inLiteral
		case inLiteral:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}
