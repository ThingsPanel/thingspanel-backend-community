package initialize

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Oracle: a PostgreSQL database built by sql/1.sql..32.sql with sql/auth/001..003 applied.
func openAuthDialects(t *testing.T) (pg, my *gorm.DB) {
	t.Helper()
	pgDSN := os.Getenv("TEST_PG_AUTH_DSN")
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if pgDSN == "" || host == "" || port == "" {
		t.Skip("TEST_PG_AUTH_DSN and TP_MYSQL_TEST_* required")
	}
	pg, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pgPool, _ := pg.DB()
	portNumber, _ := strconv.Atoi(port)
	my, err = MysqlConnect(&DbConfig{Host: host, Port: portNumber, Username: os.Getenv("TP_MYSQL_TEST_USER"), Password: os.Getenv("TP_MYSQL_TEST_PASSWORD"),
		DbName: os.Getenv("TP_MYSQL_TEST_DB"), TimeZone: "UTC", LogLevel: 1, IdleConns: 4, OpenConns: 8})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"DROP TABLE IF EXISTS auth_external_bindings", "DROP TABLE IF EXISTS auth_audit_events", "DROP TABLE IF EXISTS auth_provider_settings",
		"DROP TABLE IF EXISTS devices", "DROP TABLE IF EXISTS device_id_reservations", "DROP TABLE IF EXISTS users", "DROP TABLE IF EXISTS " + mysqlLedgerTable} {
		if err := my.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := ApplyMySQLCoreSchema(context.Background(), my); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMySQLAuthSchema(context.Background(), my); err != nil {
		t.Fatalf("apply auth: %v", err)
	}
	if err := ApplyMySQLAuthSchema(context.Background(), my); err != nil {
		t.Fatalf("re-apply auth: %v", err)
	}
	t.Cleanup(func() {
		for _, statement := range []string{"DROP TABLE IF EXISTS auth_external_bindings", "DROP TABLE IF EXISTS auth_audit_events", "DROP TABLE IF EXISTS auth_provider_settings",
			"DROP TABLE IF EXISTS devices", "DROP TABLE IF EXISTS device_id_reservations", "DROP TABLE IF EXISTS users", "DROP TABLE IF EXISTS " + mysqlLedgerTable} {
			_ = my.Exec(statement).Error
		}
		if p, e := my.DB(); e == nil {
			_ = p.Close()
		}
		_ = pgPool.Close()
	})
	return pg, my
}

func TestMysqlAuthSchemaMatchesPostgresBehaviour(t *testing.T) {
	pg, my := openAuthDialects(t)
	suffix := uuid.NewString()[:8]
	user := "au-" + suffix
	nul := []byte{0, 1, 2, 0, 255}
	type step struct {
		name string
		do   func(db *gorm.DB) error
	}
	exec := func(query string, args ...interface{}) func(db *gorm.DB) error {
		return func(db *gorm.DB) error { return db.Exec(query, args...).Error }
	}
	binding := func(id, userID, issuer, subject string) func(db *gorm.DB) error {
		return exec("INSERT INTO auth_external_bindings (id, user_id, issuer, subject, provider, client_id) VALUES (?, ?, ?, ?, 'oidc', 'client')", id, userID, issuer, subject)
	}
	settings := func(id string, extra string) func(db *gorm.DB) error {
		return exec("INSERT INTO auth_provider_settings (id, draft, draft_secret"+extra+") VALUES (?, '{\"a\": 1}', ?"+strings.Repeat(", ?", strings.Count(extra, ","))+")", id, nul)
	}
	steps := []step{
		{"create user", exec("INSERT INTO users (id, phone_number, email, password) VALUES (?, '1', ?, 'p')", user, user+"@x.com")},
		{"bind identity", binding("b1-"+suffix, user, "https://idp.example", "sub-1")},
		{"duplicate issuer+subject", binding("b2-"+suffix, user, "https://idp.example", "sub-1")},
		{"same subject, other issuer", binding("b3-"+suffix, user, "https://other.example", "sub-1")},
		{"subject differing only by case", binding("b4-"+suffix, user, "https://idp.example", "SUB-1")},
		{"binding for an unknown user", binding("b5-"+suffix, "no-such-user", "https://idp.example", "sub-9")},
		{"255-character subject", binding("b6-"+suffix, user, "https://idp.example", strings.Repeat("s", 255))},
		{"negative auth_version", exec("UPDATE users SET auth_version = -1 WHERE id = ?", user)},
		{"positive auth_version", exec("UPDATE users SET auth_version = 5 WHERE id = ?", user)},
		{"delete a user that has bindings", exec("DELETE FROM users WHERE id = ?", user)},
		{"audit event", exec("INSERT INTO auth_audit_events (actor_id, user_id, event, provider) VALUES ('a', ?, 'bind', 'oidc')", user)},
		{"audit event without provider", exec("INSERT INTO auth_audit_events (actor_id, user_id, event) VALUES ('a', ?, 'bind')", user)},
		{"settings with defaults", settings("s1-"+suffix, "")},
		{"enabled without a published payload", exec("INSERT INTO auth_provider_settings (id, draft, draft_secret, enabled) VALUES (?, '{}', ?, TRUE)", "s2-"+suffix, nul)},
		{"revision 0", exec("INSERT INTO auth_provider_settings (id, draft, draft_secret, revision) VALUES (?, '{}', ?, 0)", "s3-"+suffix, nul)},
		{"negative published revision", exec("INSERT INTO auth_provider_settings (id, draft, draft_secret, published_revision) VALUES (?, '{}', ?, -1)", "s4-"+suffix, nul)},
		{"enabled with a published payload", exec("INSERT INTO auth_provider_settings (id, draft, draft_secret, published, published_secret, published_revision, enabled) VALUES (?, '{}', ?, '{\"p\": true}', ?, 2, TRUE)", "s5-"+suffix, nul, nul)},
		{"disable it again", exec("UPDATE auth_provider_settings SET enabled = FALSE WHERE id = ?", "s5-"+suffix)},
		{"drop the published payload while enabled", exec("UPDATE auth_provider_settings SET enabled = TRUE, published = NULL WHERE id = ?", "s5-"+suffix)},
	}
	record := func(db *gorm.DB) []string {
		var out []string
		for _, s := range steps {
			verdict := "ok"
			if err := s.do(db); err != nil {
				verdict = "rejected"
			}
			out = append(out, s.name+": "+verdict)
		}
		return out
	}
	t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM auth_external_bindings WHERE user_id = '" + user + "'", "DELETE FROM auth_audit_events WHERE user_id = '" + user + "'",
			"DELETE FROM auth_provider_settings WHERE id LIKE '%-" + suffix + "'", "DELETE FROM users WHERE id = '" + user + "'"} {
			_ = pg.Exec(q).Error
		}
	})
	a, b := record(pg), record(my)
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("step differs\n  pg: %s\n  my: %s", a[i], b[i])
		}
	}
	joined := strings.Join(a, "\n")
	if strings.Count(joined, ": rejected") < 8 || strings.Count(joined, ": ok") < 8 {
		t.Fatalf("the oracle did not exercise both outcomes:\n%s", joined)
	}

	// Stored values read back the same.
	var pgBytes, myBytes []byte
	if err := pg.Raw("SELECT draft_secret FROM auth_provider_settings WHERE id = ?", "s5-"+suffix).Row().Scan(&pgBytes); err != nil {
		t.Fatal(err)
	}
	if err := my.Raw("SELECT draft_secret FROM auth_provider_settings WHERE id = ?", "s5-"+suffix).Row().Scan(&myBytes); err != nil {
		t.Fatal(err)
	}
	if string(pgBytes) != string(nul) || string(myBytes) != string(nul) {
		t.Fatalf("binary secrets must round trip including NUL bytes: pg=%v my=%v", pgBytes, myBytes)
	}
	var pgJSON, myJSON string
	if err := pg.Raw("SELECT draft::text FROM auth_provider_settings WHERE id = ?", "s1-"+suffix).Scan(&pgJSON).Error; err != nil {
		t.Fatal(err)
	}
	if err := my.Raw("SELECT CAST(draft AS CHAR) FROM auth_provider_settings WHERE id = ?", "s1-"+suffix).Scan(&myJSON).Error; err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(pgJSON, " ", "") != strings.ReplaceAll(myJSON, " ", "") {
		t.Fatalf("draft JSON differs: pg=%q my=%q", pgJSON, myJSON)
	}
	var pgEnabled, myEnabled bool
	var pgUnlinked, myUnlinked bool
	_ = pg.Raw("SELECT enabled, self_unlinked FROM auth_external_bindings WHERE id = ?", "b1-"+suffix).Row().Scan(&pgEnabled, &pgUnlinked)
	_ = my.Raw("SELECT enabled, self_unlinked FROM auth_external_bindings WHERE id = ?", "b1-"+suffix).Row().Scan(&myEnabled, &myUnlinked)
	if !pgEnabled || !myEnabled || pgUnlinked || myUnlinked {
		t.Fatalf("defaults differ: enabled pg=%v my=%v, self_unlinked pg=%v my=%v", pgEnabled, myEnabled, pgUnlinked, myUnlinked)
	}
}

// Documented divergence: PostgreSQL stores unbounded TEXT subjects, MySQL's key-length limit caps them.
func TestMysqlAuthSubjectLengthDivergenceIsExplicit(t *testing.T) {
	pg, my := openAuthDialects(t)
	suffix := uuid.NewString()[:8]
	user := "al-" + suffix
	for _, db := range []*gorm.DB{pg, my} {
		if err := db.Exec("INSERT INTO users (id, phone_number, email, password) VALUES (?, '1', ?, 'p')", user, user+"@x.com").Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = pg.Exec("DELETE FROM auth_external_bindings WHERE user_id = ?", user).Error
		_ = pg.Exec("DELETE FROM users WHERE id = ?", user).Error
	})
	insert := func(db *gorm.DB) error {
		return db.Exec("INSERT INTO auth_external_bindings (id, user_id, issuer, subject, provider, client_id) VALUES (?, ?, 'https://i.example', ?, 'oidc', 'c')", "x-"+suffix, user, strings.Repeat("s", 256)).Error
	}
	if err := insert(pg); err != nil {
		t.Fatalf("PostgreSQL accepts a 256-character subject: %v", err)
	}
	if err := insert(my); err == nil {
		t.Fatal("MySQL must reject a subject over 255 characters rather than truncate it")
	}
}
