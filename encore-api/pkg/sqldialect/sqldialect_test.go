package sqldialect

import "testing"

func TestRewriteTranslatesKnownIdiomsAndRefusesTheRest(t *testing.T) {
	q, args, ok := MySQL.Rewrite(`SELECT x FROM t WHERE ($1::text IS NULL OR c=$1) AND d='$9 it''s' AND e<now()+interval '10 minutes' AND f=$2`, []any{"a", 7})
	const want = `SELECT x FROM t WHERE (? IS NULL OR c=?) AND d='$9 it''s' AND e<NOW(6)+INTERVAL 10 MINUTE AND f=?`
	if !ok || q != want || len(args) != 3 || args[0] != "a" || args[1] != "a" || args[2] != 7 {
		t.Fatalf("ok=%v\n got %q %v\nwant %q", ok, q, args, want)
	}
	for _, bad := range []string{`SELECT a::int FROM t`, `SELECT jsonb_agg(x) FROM t`, `DELETE FROM t RETURNING id`,
		`SELECT pg_advisory_xact_lock(1)`, `SELECT current_schema()`, `SELECT $3 FROM t`, `SELECT a FROM t WHERE b ILIKE $1`} {
		if _, _, ok := MySQL.Rewrite(bad, []any{1}); ok {
			t.Errorf("must not translate: %s", bad)
		}
	}
	if q, a, ok := Postgres.Rewrite(`SELECT $1::int`, []any{1}); !ok || q != `SELECT $1::int` || len(a) != 1 {
		t.Fatalf("PostgreSQL statements pass through untouched: %q", q)
	}
	cases := map[string]string{
		`INSERT INTO t(id,a,b) VALUES($1,$2,$3) ON CONFLICT (id) DO UPDATE SET a=EXCLUDED.a, b = excluded.b`: `INSERT INTO t(id,a,b) VALUES(?,?,?) ON DUPLICATE KEY UPDATE a=VALUES(a), b = VALUES(b)`,
		`UPDATE t SET p=$1::jsonb, q=$2::bigint WHERE id=$3`:                                                 `UPDATE t SET p=?, q=? WHERE id=?`,
		`SELECT payload->>'name', payload->'meta' FROM t`:                                                    `SELECT JSON_UNQUOTE(JSON_EXTRACT(payload, '$.name')), JSON_EXTRACT(payload, '$.meta') FROM t`,
	}
	for in, want := range cases {
		if got, _, ok := MySQL.Rewrite(in, []any{1, 2, 3}); !ok || got != want {
			t.Errorf("rewrite\n in: %s\n got: %s (ok=%v)\nwant: %s", in, got, ok, want)
		}
	}
	for _, bad := range []string{
		`INSERT INTO t(id) VALUES($1) ON CONFLICT (id) DO UPDATE SET a=EXCLUDED.a WHERE t.ts <= EXCLUDED.ts`,
		`UPDATE t SET a=1 WHERE id=$1 RETURNING a`, `SELECT a FROM t WHERE id = ANY($1)`, `SELECT a::int FROM t`,
	} {
		if _, _, ok := MySQL.Rewrite(bad, []any{1}); ok {
			t.Errorf("must stay untranslated: %s", bad)
		}
	}
	if q, _, ok := MySQL.Rewrite(`INSERT INTO t(id) VALUES($1) ON CONFLICT DO NOTHING`, []any{1}); !ok || q != `INSERT INTO t(id) VALUES(?) ON DUPLICATE KEY UPDATE id=id` {
		t.Fatalf("DO NOTHING: ok=%v %q", ok, q)
	}
}
