// Package reltest runs one scenario against PostgreSQL and MySQL in lockstep and compares every step.
//
// A scenario is a function over *Scenario. Each Step runs the same Go closure on both engines (the closure may switch
// on relational.DialectOf(db) when the code under test has two implementations) and the results are compared after
// JSON normalisation; a returned error is compared by relational.Classify kind, not by message. Table snapshots
// compare final state. A mismatch fails the test and the report lists every step with its outcome on both engines.
//
// Each scenario gets a fresh database on each server, so scenarios cannot see each other's tables. The servers come
// from the environment (see TargetsFromEnv); when either is missing the test is skipped with the reason, because a
// differential result needs both.
package reltest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
)

// TargetsFromEnv reads the administrative connection of each engine:
//
//	PostgreSQL: TEST_PG_SCHEMA_DSN            (postgres://user:pw@host:port/db?sslmode=disable; needs CREATE DATABASE)
//	MySQL-family: TP_MYSQL_TEST_HOST/PORT/USER/PASSWORD/DB and optional TP_MYSQL_TEST_FLAVOR
func TargetsFromEnv() map[relational.Dialect]relational.Target {
	out := map[relational.Dialect]relational.Target{}
	if dsn := os.Getenv("TEST_PG_SCHEMA_DSN"); dsn != "" {
		out[relational.Postgres] = relational.Target{Name: "reltest_pg", Dialect: relational.Postgres, DSN: dsn}
	}
	if host := os.Getenv("TP_MYSQL_TEST_HOST"); host != "" {
		port, _ := strconv.Atoi(os.Getenv("TP_MYSQL_TEST_PORT"))
		out[relational.MySQL] = relational.Target{Name: "reltest_my", Dialect: relational.MySQL, Host: host, Port: port,
			Database: os.Getenv("TP_MYSQL_TEST_DB"), Username: os.Getenv("TP_MYSQL_TEST_USER"),
			Password: os.Getenv("TP_MYSQL_TEST_PASSWORD"), TimeZone: "UTC", Flavor: relational.Flavor(os.Getenv("TP_MYSQL_TEST_FLAVOR"))}
	}
	return out
}

var dbCounter atomic.Int64

// Env is a pair of fresh databases, one per engine.
type Env struct {
	DB map[relational.Dialect]*gorm.DB
}

// NewEnv creates one scratch database per requested engine (default: both) and drops them, and closes the pools, with
// t.Cleanup. It skips the test when a requested engine is not configured.
func NewEnv(t testing.TB, engines ...relational.Dialect) *Env {
	t.Helper()
	if len(engines) == 0 {
		engines = []relational.Dialect{relational.Postgres, relational.MySQL}
	}
	targets := TargetsFromEnv()
	for _, d := range engines {
		if _, ok := targets[d]; !ok {
			t.Skipf("differential test needs both engines; %s is not configured (TEST_PG_SCHEMA_DSN, TP_MYSQL_TEST_HOST/PORT/USER/PASSWORD/DB)", d)
		}
	}
	env := &Env{DB: map[relational.Dialect]*gorm.DB{}}
	ctx := context.Background()
	for _, d := range engines {
		admin := targets[d]
		adminDB, err := relational.Open(ctx, admin)
		if err != nil {
			t.Fatalf("reltest: open %s admin connection: %v", d, err)
		}
		name := fmt.Sprintf("reltest_%d_%d", time.Now().UnixNano()%1_000_000_000, dbCounter.Add(1))
		if err := adminDB.Exec("CREATE DATABASE " + name).Error; err != nil {
			_ = relational.CloseDB(adminDB)
			t.Fatalf("reltest: create database on %s: %v", d, err)
		}
		scratch := admin
		scratch.Name = name
		scratch.Database = name
		if d == relational.Postgres {
			u, err := url.Parse(admin.DSN)
			if err != nil {
				t.Fatalf("reltest: TEST_PG_SCHEMA_DSN: %v", err)
			}
			u.Path = "/" + name
			scratch.DSN = u.String()
		}
		db, err := relational.Open(ctx, scratch)
		if err != nil {
			_ = adminDB.Exec("DROP DATABASE " + name).Error
			_ = relational.CloseDB(adminDB)
			t.Fatalf("reltest: open scratch database on %s: %v", d, err)
		}
		env.DB[d] = db
		d := d
		t.Cleanup(func() {
			_ = relational.CloseDB(db)
			drop := "DROP DATABASE IF EXISTS " + name
			if d == relational.Postgres {
				drop += " WITH (FORCE)"
			}
			if err := adminDB.Exec(drop).Error; err != nil {
				t.Logf("reltest: drop %s database %s: %v", d, name, err)
			}
			_ = relational.CloseDB(adminDB)
		})
	}
	return env
}

// Outcome is what one engine produced for one step.
type Outcome struct {
	Value string // canonical JSON of the returned value
	Err   string // "" or the error kind, e.g. "unique", "other"
	Raw   error  `json:"-"`
}

// StepReport is one compared step.
type StepReport struct {
	Name     string
	Postgres Outcome
	MySQL    Outcome
	Match    bool
	Detail   string
}

// Scenario drives both engines. Create it with Run; do not construct it directly.
type Scenario struct {
	t      testing.TB
	env    *Env
	ctx    context.Context
	Report []StepReport
	failed bool
}

// Ctx is the context for the scenario.
func (s *Scenario) Ctx() context.Context { return s.ctx }

// DB returns the handle of one engine, for work that must not be compared (for example a concurrent worker).
func (s *Scenario) DB(d relational.Dialect) *gorm.DB { return s.env.DB[d] }

// Exec runs one statement per engine (pass the same text twice when it is portable). It is a step: an error on
// either engine fails the scenario.
func (s *Scenario) Exec(name, pgSQL, mySQL string) {
	s.t.Helper()
	s.Step(name, func(ctx context.Context, db *gorm.DB) (any, error) {
		q := pgSQL
		if relational.MustDialectOf(db) == relational.MySQL {
			q = mySQL
		}
		return nil, db.WithContext(ctx).Exec(q).Error
	})
}

// Step runs fn on both engines and compares value and error kind. fn must return JSON-marshalable values; time values
// should be formatted by the caller when the engines' precision differs.
func (s *Scenario) Step(name string, fn func(ctx context.Context, db *gorm.DB) (any, error)) {
	s.t.Helper()
	r := StepReport{Name: name}
	for _, d := range []relational.Dialect{relational.Postgres, relational.MySQL} {
		v, err := fn(s.ctx, s.env.DB[d])
		o := Outcome{Value: canonical(v), Raw: err}
		if err != nil {
			o.Err = kindName(relational.Classify(err))
		}
		if d == relational.Postgres {
			r.Postgres = o
		} else {
			r.MySQL = o
		}
	}
	r.Match, r.Detail = Compare(r.Postgres, r.MySQL)
	s.record(r)
}

// Table compares the rows of a query on both engines (ORDER BY is the caller's job). Cast so the engines return the
// same types: booleans as 0/1 CASE expressions, timestamps as text.
func (s *Scenario) Table(name, query string, args ...any) {
	s.t.Helper()
	s.Step("table:"+name, func(ctx context.Context, db *gorm.DB) (any, error) {
		return Rows(ctx, db, query, args...)
	})
}

func (s *Scenario) record(r StepReport) {
	s.t.Helper()
	s.Report = append(s.Report, r)
	if !r.Match {
		s.failed = true
		s.t.Errorf("step %q differs between PostgreSQL and MySQL: %s", r.Name, r.Detail)
	}
}

// Rows runs a query and returns generic rows ([]byte as string).
func Rows(ctx context.Context, db *gorm.DB, query string, args ...any) ([]map[string]any, error) {
	var rows []map[string]any
	if err := db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		for k, v := range row {
			if b, ok := v.([]byte); ok {
				row[k] = string(b)
			}
		}
	}
	return rows, nil
}

// Compare decides whether two outcomes are the same: same error kind (when either failed) and identical canonical value.
func Compare(pg, my Outcome) (bool, string) {
	if pg.Err != my.Err {
		return false, fmt.Sprintf("error kind: postgres=%q (%v) mysql=%q (%v)", pg.Err, pg.Raw, my.Err, my.Raw)
	}
	if pg.Err != "" {
		if pg.Err == "other" {
			return false, fmt.Sprintf("both failed with an unclassified error: postgres: %v; mysql: %v", pg.Raw, my.Raw)
		}
		return true, ""
	}
	if pg.Value != my.Value {
		return false, fmt.Sprintf("value: postgres=%s mysql=%s", pg.Value, my.Value)
	}
	return true, ""
}

func kindName(k relational.Kind) string {
	switch k {
	case relational.KindUnique:
		return "unique"
	case relational.KindNotFound:
		return "not_found"
	case relational.KindTimeout:
		return "timeout"
	case relational.KindDeadlock:
		return "deadlock"
	case relational.KindForeignKey:
		return "foreign_key"
	}
	return "other"
}

// canonical marshals v with sorted map keys (encoding/json does that) and no HTML escaping.
func canonical(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "<unmarshalable: " + err.Error() + ">"
	}
	return strings.TrimSpace(buf.String())
}

// Run creates the scratch databases and runs the scenario. On completion it logs the report (all steps, both
// outcomes) so a passing run documents what was compared.
func Run(t *testing.T, name string, scenario func(s *Scenario)) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		env := NewEnv(t)
		s := &Scenario{t: t, env: env, ctx: context.Background()}
		scenario(s)
		t.Log(FormatReport(name, s.Report))
	})
}

// FormatReport renders the step list.
func FormatReport(name string, steps []StepReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "differential scenario %s: %d steps\n", name, len(steps))
	for i, st := range steps {
		mark := "ok  "
		if !st.Match {
			mark = "DIFF"
		}
		fmt.Fprintf(&b, "  %2d %s %-40s pg=%s my=%s\n", i+1, mark, st.Name, short(st.Postgres), short(st.MySQL))
	}
	return b.String()
}

func short(o Outcome) string {
	if o.Err != "" {
		return "err:" + o.Err
	}
	if len(o.Value) > 40 {
		return o.Value[:37] + "..."
	}
	return o.Value
}
