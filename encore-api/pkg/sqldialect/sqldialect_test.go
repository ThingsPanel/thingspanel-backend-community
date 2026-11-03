package sqldialect

import "testing"

func TestRewriteTranslatesKnownIdiomsAndRefusesTheRest(t *testing.T) {
	q, args, ok := MySQL.Rewrite(`SELECT x FROM t WHERE ($1::text IS NULL OR c=$1) AND d='$9 it''s' AND e<now()+interval '10 minutes' AND f=$2`, []any{"a", 7})
	const want = `SELECT x FROM t WHERE (? IS NULL OR c=?) AND d='$9 it''s' AND e<NOW(6)+INTERVAL 10 MINUTE AND f=?`
	if !ok || q != want || len(args) != 3 || args[0] != "a" || args[1] != "a" || args[2] != 7 {
		t.Fatalf("ok=%v\n got %q %v\nwant %q", ok, q, args, want)
	}
	for _, bad := range []string{`SELECT a::int FROM t`, `SELECT jsonb_agg(x) FROM t`, `DELETE FROM t RETURNING id`,
		`SELECT current_schema()`, `SELECT $3 FROM t`, `SELECT a FROM t WHERE b ILIKE $1`} {
		if _, _, ok := MySQL.Rewrite(bad, []any{1}); ok {
			t.Errorf("must not translate: %s", bad)
		}
	}
	if q, a, ok := Postgres.Rewrite(`SELECT $1::int`, []any{1}); !ok || q != `SELECT $1::int` || len(a) != 1 {
		t.Fatalf("PostgreSQL statements pass through untouched: %q", q)
	}
	cases := map[string]string{
		`SELECT g.id FROM groups g JOIN device_groups d ON d.x = g.id WHERE g.name = 'groups'`: "SELECT g.id FROM `groups` g JOIN device_groups d ON d.x = g.id WHERE g.name = 'groups'",
		`SELECT count(*) FILTER (WHERE status IN ('a','b')), count(*) FILTER (WHERE NOT enabled), count(*) FROM t`: `SELECT count(CASE WHEN status IN ('a','b') THEN 1 END), count(CASE WHEN NOT enabled THEN 1 END), count(*) FROM t`,
		"CREATE TABLE t (`signal` INT, `key` INT)": "CREATE TABLE t (`signal` INT, `key` INT)",
		`SELECT coalesce(report,'null'::jsonb) FROM t WHERE key=$1 AND signal='key' AND x.key=$2 AND idempotency_key=$3`:    "SELECT coalesce(report,CAST('null' AS JSON)) FROM t WHERE `key`=? AND `signal`='key' AND x.`key`=? AND idempotency_key=?",
		`UPDATE t SET d=NULLIF($1,'')::jsonb, r=coalesce(NULLIF($2,'')::uuid, r) WHERE id=$3`:                               `UPDATE t SET d=NULLIF(?,''), r=coalesce(NULLIF(?,''), r) WHERE id=?`,
		`SELECT id::text, coalesce(binding_params,'{}'::jsonb), coalesce(a,'[]'::jsonb) FROM t WHERE r.rule_id::uuid=$1`:    `SELECT id, coalesce(binding_params,JSON_OBJECT()), coalesce(a,JSON_ARRAY()) FROM t WHERE r.rule_id=?`,
		`INSERT INTO auto_requests (tenant_id, request_key) VALUES ($1,$2) ON CONFLICT (tenant_id, request_key) DO NOTHING`: `INSERT INTO auto_requests (tenant_id, request_key) VALUES (?,?)`,
		`INSERT INTO t(id,a,b) VALUES($1,$2,$3) ON CONFLICT (id) DO UPDATE SET a=EXCLUDED.a, b = excluded.b`:                `INSERT INTO t(id,a,b) VALUES(?,?,?) ON DUPLICATE KEY UPDATE a=VALUES(a), b = VALUES(b)`,
		`UPDATE t SET p=$1::jsonb, q=$2::bigint WHERE id=$3`:                                                                `UPDATE t SET p=?, q=? WHERE id=?`,
		`SELECT payload->>'name', payload->'meta' FROM t`:                                                                   `SELECT JSON_UNQUOTE(JSON_EXTRACT(payload, '$.name')), JSON_EXTRACT(payload, '$.meta') FROM t`,
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
	if q, _, ok := MySQL.Rewrite(`INSERT INTO t(id) VALUES($1) ON CONFLICT DO NOTHING`, []any{1}); !ok || q != `INSERT INTO t(id) VALUES(?)` {
		t.Fatalf("DO NOTHING: ok=%v %q", ok, q)
	}
}

func TestWithoutOnConflictDoNothing(t *testing.T) {
	got, ok := WithoutOnConflictDoNothing("INSERT INTO t(a) VALUES($1) ON CONFLICT (a) DO NOTHING")
	if !ok || got != "INSERT INTO t(a) VALUES($1)" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := WithoutOnConflictDoNothing("INSERT INTO t(a) VALUES($1) ON CONFLICT (a) DO UPDATE SET a=EXCLUDED.a"); ok {
		t.Fatal("DO UPDATE must not be stripped")
	}
	if _, ok := WithoutOnConflictDoNothing("INSERT INTO t(a) VALUES($1)"); ok {
		t.Fatal("a plain insert has nothing to strip")
	}
}

func TestGormStyleStatementsTranslate(t *testing.T) {
	check := func(in string, argCount int, want string, wantIdx []int, ignore bool) {
		t.Helper()
		got, ok := MySQL.Translate(in, argCount)
		if !ok || got.Query != want || got.IgnoreDuplicate != ignore || len(got.Idx) != len(wantIdx) {
			t.Fatalf("in:   %s\ngot:  %q idx=%v ignore=%v ok=%v\nwant: %q idx=%v ignore=%v", in, got.Query, got.Idx, got.IgnoreDuplicate, ok, want, wantIdx, ignore)
		}
		for i := range wantIdx {
			if got.Idx[i] != wantIdx[i] {
				t.Fatalf("idx %v, want %v", got.Idx, wantIdx)
			}
		}
	}
	// ? placeholders pass through in order, casts on them vanish.
	check(`INSERT INTO t(a,b) VALUES(?,?::jsonb)`, 2, `INSERT INTO t(a,b) VALUES(?,?)`, []int{0, 1}, false)
	check("INSERT INTO t (`id`) VALUES (?) ON DUPLICATE KEY UPDATE `id`=`id`", 1, "INSERT INTO t (`id`) VALUES (?)", []int{0}, true)
	check("INSERT INTO t (`id`) VALUES (?),(?) ON DUPLICATE KEY UPDATE `id`=`id`", 2, "INSERT INTO t (`id`) VALUES (?),(?) ON DUPLICATE KEY UPDATE `id`=`id`", []int{0, 1}, false)
	check(`INSERT INTO t(a) VALUES(?),(?) ON CONFLICT DO NOTHING`, 2, `INSERT INTO t(a) VALUES(?),(?) ON DUPLICATE KEY UPDATE a=a`, []int{0, 1}, false)
	check(`INSERT INTO t(a,l) VALUES(?, now() + make_interval(secs => ?))`, 2, `INSERT INTO t(a,l) VALUES(?, NOW(6) + INTERVAL ? SECOND)`, []int{0, 1}, false)
	check(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, 1,
		`INSERT INTO tp_advisory_locks (lock_key) VALUES (SHA2(?, 256)) ON DUPLICATE KEY UPDATE lock_key = lock_key`, []int{0}, false)
	check(`SELECT pg_advisory_xact_lock(hashtext('brokerquarantine'),hashtext(?))`, 1,
		`INSERT INTO tp_advisory_locks (lock_key) VALUES (SHA2(CONCAT_WS(CHAR(31), 'brokerquarantine', ?), 256)) ON DUPLICATE KEY UPDATE lock_key = lock_key`, []int{0}, false)
	check(`SELECT pg_advisory_xact_lock(716238940)`, 0,
		`INSERT INTO tp_advisory_locks (lock_key) VALUES (SHA2('716238940', 256)) ON DUPLICATE KEY UPDATE lock_key = lock_key`, []int{}, false)
	if _, ok := MySQL.Translate(`SELECT $1, ?`, 2); ok {
		t.Fatal("mixed placeholder styles must be refused")
	}
}
