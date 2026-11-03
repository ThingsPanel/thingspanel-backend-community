package sqldialect

import "testing"

func TestRewriteTranslatesKnownIdiomsAndRefusesTheRest(t *testing.T) {
	q, args, ok := MySQL.Rewrite(`SELECT x FROM t WHERE ($1::text IS NULL OR c=$1) AND d='$9 it''s' AND e<now()+interval '10 minutes' AND f=$2`, []any{"a", 7})
	const want = `SELECT x FROM t WHERE (? IS NULL OR c=?) AND d='$9 it''s' AND e<NOW(6)+INTERVAL 10 MINUTE AND f=?`
	if !ok || q != want || len(args) != 3 || args[0] != "a" || args[1] != "a" || args[2] != 7 {
		t.Fatalf("ok=%v\n got %q %v\nwant %q", ok, q, args, want)
	}
	for _, bad := range []string{`SELECT a::int FROM t`, `SELECT jsonb_agg(x) FROM t`, `INSERT INTO t VALUES($1) ON CONFLICT(id) DO UPDATE SET a=1`, `DELETE FROM t RETURNING id`,
		`SELECT pg_advisory_xact_lock(1)`, `SELECT current_schema()`, `SELECT $3 FROM t`, `SELECT a FROM t WHERE b ILIKE $1`} {
		if _, _, ok := MySQL.Rewrite(bad, []any{1}); ok {
			t.Errorf("must not translate: %s", bad)
		}
	}
	if q, a, ok := Postgres.Rewrite(`SELECT $1::int`, []any{1}); !ok || q != `SELECT $1::int` || len(a) != 1 {
		t.Fatalf("PostgreSQL statements pass through untouched: %q", q)
	}
	if q, _, ok := MySQL.Rewrite(`INSERT INTO t(id) VALUES($1) ON CONFLICT DO NOTHING`, []any{1}); !ok || q != `INSERT INTO t(id) VALUES(?) ON DUPLICATE KEY UPDATE id=id` {
		t.Fatalf("DO NOTHING: ok=%v %q", ok, q)
	}
}
