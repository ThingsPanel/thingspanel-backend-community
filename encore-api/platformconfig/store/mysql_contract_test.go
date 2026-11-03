package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"encore.app/pkg/sqldialect"
	"encore.app/platformconfig/app"
)

func readMySQLMigration(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "sql", "mysql", DictionaryMySQLMigrationFile))
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	for _, part := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n-- @@\n") {
		var kept []string
		for _, line := range strings.Split(part, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				kept = append(kept, line)
			}
		}
		if statement := strings.TrimSpace(strings.Join(kept, "\n")); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

// The pinned checksum must describe the migration file that is actually shipped.
func TestMySQLMigrationChecksumMatchesTheShippedFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "sql", "mysql", DictionaryMySQLMigrationFile))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(strings.ReplaceAll(string(raw), "\r\n", "\n")))
	if hex.EncodeToString(sum[:]) != DictionaryMySQLMigrationChecksum {
		t.Fatalf("DictionaryMySQLMigrationChecksum is %s, the file hashes to %s", DictionaryMySQLMigrationChecksum, hex.EncodeToString(sum[:]))
	}
}

func contractStores(t *testing.T) (pg, my *Store, mysqlDB *sql.DB) {
	t.Helper()
	pgDSN, myDSN := os.Getenv("TP_DICT_CONTRACT_PG_DSN"), os.Getenv("TP_DICT_CONTRACT_MYSQL_DSN")
	if pgDSN == "" || myDSN == "" {
		t.Skip("TP_DICT_CONTRACT_PG_DSN and TP_DICT_CONTRACT_MYSQL_DSN required")
	}
	config, err := pgx.ParseConfig(pgDSN)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*config)
	schema := "dict_contract_" + strings.ReplaceAll(fmt.Sprint(time.Now().UnixNano()), "-", "")
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	config.RuntimeParams["search_path"] = schema
	pgDB := stdlib.OpenDB(*config)
	pgDB.SetMaxOpenConns(8)
	t.Cleanup(func() { pgDB.Close() })
	legacy, err := os.ReadFile(filepath.Join("..", "..", "..", "sql", "1.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sys_dict", "sys_dict_language"} {
		start := strings.Index(string(legacy), "CREATE TABLE public."+table+" (")
		remaining := string(legacy[start:])
		end := strings.Index(remaining, "\n);")
		if _, err := pgDB.Exec(strings.ReplaceAll(remaining[:end+3], "public.", "")); err != nil {
			t.Fatal("legacy DDL: ", err)
		}
	}

	for _, statement := range []string{
		`INSERT INTO sys_dict (id, dict_code, dict_value, created_at, remark) VALUES ('0013fb9e-e3be-95d4-9c96-f18d1f9ddfcd','GATEWAY_PROTOCOL','MQTT','2024-01-18 15:39:38.469+00',NULL),('7162fb9e-e3be-95d4-9c96-f18d1f9ddfcd','DRIECT_ATTACHED_PROTOCOL','MQTT','2024-01-18 15:39:38.469+00',NULL)`,
		`INSERT INTO sys_dict_language (id, dict_id, language_code, translation) VALUES ('001c3960-3067-536d-5c97-7645351a687c','7162fb9e-e3be-95d4-9c96-f18d1f9ddfcd','zh_CN','MQTT协议'),('002c3960-3067-536d-5c97-7645351a687b','0013fb9e-e3be-95d4-9c96-f18d1f9ddfcd','zh_CN','MQTT协议(网关)'),('7162fb9e-e3be-95d4-9c96-f18d1f9ddfff','0013fb9e-e3be-95d4-9c96-f18d1f9ddfcd','en_US','MQTT Protocol(Gateway)'),('7162fb9e-e3be-95d4-9c96-f18d1f9ddfss','7162fb9e-e3be-95d4-9c96-f18d1f9ddfcd','en_US','MQTT Protocol')`,
	} {
		if _, err := pgDB.Exec(statement); err != nil {
			t.Fatal("seed: ", err)
		}
	}

	mysqlDB, err = sql.Open("mysql", myDSN)
	if err != nil {
		t.Fatal(err)
	}
	mysqlDB.SetMaxOpenConns(8)
	for _, table := range []string{"sys_dict_language", "sys_dict", "native_component_migration_ledger"} {
		_, _ = mysqlDB.Exec("DROP TABLE IF EXISTS " + table)
	}
	t.Cleanup(func() {
		for _, table := range []string{"sys_dict_language", "sys_dict", "native_component_migration_ledger"} {
			_, _ = mysqlDB.Exec("DROP TABLE IF EXISTS " + table)
		}
		mysqlDB.Close()
	})
	for _, statement := range readMySQLMigration(t) {
		if _, err := mysqlDB.Exec(statement); err != nil {
			t.Fatalf("apply the MySQL migration: %v\n%s", err, statement)
		}
	}
	return New(pgDB), NewMySQL(mysqlDB), mysqlDB
}

func kindOf(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, app.ErrConflict):
		return "conflict"
	case errors.Is(err, app.ErrNotFound):
		return "not-found"
	case errors.Is(err, app.ErrWriteNotApplied):
		return "not-applied"
	case errors.Is(err, app.ErrUnavailable):
		return "unavailable"
	}
	return "other"
}

func TestDictionaryStoreBehavesTheSameOnMySQLAsOnPostgres(t *testing.T) {
	pg, my, _ := contractStores(t)
	ctx := context.Background()
	moment := time.Date(2026, 11, 3, 1, 2, 3, 123456000, time.UTC)
	run := func(s *Store) []string {
		var out []string
		note := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		remark := "first"
		for i, row := range []app.Dictionary{
			{ID: "d1", DictCode: "COLOR", DictValue: "red", CreatedAt: moment, Remark: &remark},
			{ID: "d2", DictCode: "COLOR", DictValue: "green", CreatedAt: moment.Add(time.Minute)},
			{ID: "d3", DictCode: "SIZE", DictValue: "large", CreatedAt: moment.Add(2 * time.Minute)},
			{ID: "d4", DictCode: "COLOR", DictValue: "red", CreatedAt: moment},   // same code and value
			{ID: "d5", DictCode: "color", DictValue: "red", CreatedAt: moment},   // differs only by case
			{ID: "d1", DictCode: "SHAPE", DictValue: "round", CreatedAt: moment}, // same id
		} {
			note("create dictionary %d: %s", i, kindOf(s.CreateDictionary(ctx, row)))
		}
		for i, row := range []app.Language{
			{ID: "l1", DictID: "d1", LanguageCode: "zh_CN", Translation: "红"},
			{ID: "l2", DictID: "d1", LanguageCode: "en_US", Translation: "red"},
			{ID: "l3", DictID: "d2", LanguageCode: "en_US", Translation: "green"},
			{ID: "l4", DictID: "d1", LanguageCode: "zh_CN", Translation: "again"}, // duplicate language for the parent
			{ID: "l5", DictID: "missing", LanguageCode: "en_US", Translation: "x"},
			{ID: "l1", DictID: "d3", LanguageCode: "en_US", Translation: "big"}, // duplicate id
		} {
			note("create language %d: %s", i, kindOf(s.CreateLanguage(ctx, row)))
		}
		enum, err := s.Enum(ctx, "COLOR", "en_US")
		note("enum COLOR en_US: %s %v", kindOf(err), enum)
		enum, err = s.Enum(ctx, "COLOR", "fr_FR")
		note("enum COLOR fr_FR (untranslated): %s %v", kindOf(err), enum)
		enum, err = s.Enum(ctx, "NOPE", "en_US")
		note("enum unknown code: %s %v", kindOf(err), enum == nil)
		all, err := s.Page(ctx, app.PageInput{Page: 1, PageSize: 10})
		note("page all: %s total=%d ids=%v", kindOf(err), all.Total, ids(all.List))
		code := "COLOR"
		filtered, err := s.Page(ctx, app.PageInput{Page: 1, PageSize: 2, DictCode: &code})
		note("page COLOR size 2: %s total=%d ids=%v", kindOf(err), filtered.Total, ids(filtered.List))
		second, err := s.Page(ctx, app.PageInput{Page: 2, PageSize: 2, DictCode: &code})
		note("page COLOR page 2: %s total=%d ids=%v", kindOf(err), second.Total, ids(second.List))
		langs, err := s.Languages(ctx, "d1")
		note("languages of d1: %s %v", kindOf(err), langs)
		protocols, err := s.Protocols(ctx, "zh_CN")
		note("protocols zh_CN: %s %d", kindOf(err), len(protocols))
		note("delete language l2: %s", kindOf(s.DeleteLanguage(ctx, "l2")))
		note("delete language l2 again: %s", kindOf(s.DeleteLanguage(ctx, "l2")))
		note("delete dictionary d1 (cascades): %s", kindOf(s.DeleteDictionary(ctx, "d1")))
		note("delete dictionary d1 again: %s", kindOf(s.DeleteDictionary(ctx, "d1")))
		after, _ := s.Languages(ctx, "d1")
		note("languages of d1 after the cascade: %d", len(after))
		remaining, _ := s.Page(ctx, app.PageInput{Page: 1, PageSize: 10})
		note("dictionaries left: %v", ids(remaining.List))
		return out
	}
	a, b := run(pg), run(my)
	if !reflect.DeepEqual(a, b) {
		for i := range a {
			if i >= len(b) || a[i] != b[i] {
				bb := ""
				if i < len(b) {
					bb = b[i]
				}
				t.Errorf("step %d differs\n  pg: %s\n  my: %s", i, a[i], bb)
			}
		}
	}
	joined := strings.Join(a, "\n")
	for _, want := range []string{"create dictionary 0: ok", "conflict", "enum COLOR en_US: ok", "dictionaries left"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the oracle scenario lacks %q:\n%s", want, joined)
		}
	}
}

func ids(list []app.Dictionary) []string {
	var out []string
	for _, d := range list {
		out = append(out, d.ID)
	}
	return out
}

func TestMySQLReadinessFollowsTheReceiptAndTheLiveSchema(t *testing.T) {
	_, my, db := contractStores(t)
	ctx := context.Background()
	if err := my.ValidateReady(ctx); !errors.Is(err, app.ErrUnavailable) {
		t.Fatalf("without a migration receipt the store must be unavailable: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE native_component_migration_ledger (module_name VARCHAR(128) NOT NULL, file_name VARCHAR(255) NOT NULL, checksum CHAR(64) NOT NULL, applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), PRIMARY KEY (module_name, file_name))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO native_component_migration_ledger (module_name, file_name, checksum) VALUES (?,?,?)`, DictionaryMigrationModule, DictionaryMySQLMigrationFile, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if err := my.ValidateReady(ctx); !errors.Is(err, app.ErrUnavailable) {
		t.Fatalf("a receipt with another checksum must be refused: %v", err)
	}
	if _, err := db.Exec(`UPDATE native_component_migration_ledger SET checksum=?`, DictionaryMySQLMigrationChecksum); err != nil {
		t.Fatal(err)
	}
	if err := my.ValidateReady(ctx); err != nil {
		t.Fatalf("the exact receipt and schema must be ready: %v", err)
	}
	for name, alter := range map[string][2][]string{
		"an extra column":          {{"ALTER TABLE sys_dict ADD COLUMN extra INT NULL"}, {"ALTER TABLE sys_dict DROP COLUMN extra"}},
		"a widened column":         {{"ALTER TABLE sys_dict MODIFY dict_value VARCHAR(300) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL"}, {"ALTER TABLE sys_dict MODIFY dict_value VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL"}},
		"a nullable column":        {{"ALTER TABLE sys_dict_language MODIFY translation VARCHAR(255) NULL"}, {"ALTER TABLE sys_dict_language MODIFY translation VARCHAR(255) NOT NULL"}},
		"a default value":          {{"ALTER TABLE sys_dict MODIFY dict_code VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT 'x'"}, {"ALTER TABLE sys_dict MODIFY dict_code VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL"}},
		"a dropped unique key":     {{"ALTER TABLE sys_dict DROP INDEX sys_dict_dict_code_dict_value_key"}, {"ALTER TABLE sys_dict ADD CONSTRAINT sys_dict_dict_code_dict_value_key UNIQUE (dict_code, dict_value)"}},
		"a restricted foreign key": {{"ALTER TABLE sys_dict_language DROP FOREIGN KEY sys_dict_language_dict_id_fkey", "ALTER TABLE sys_dict_language ADD CONSTRAINT sys_dict_language_dict_id_fkey FOREIGN KEY (dict_id) REFERENCES sys_dict (id)"}, {"ALTER TABLE sys_dict_language DROP FOREIGN KEY sys_dict_language_dict_id_fkey", "ALTER TABLE sys_dict_language ADD CONSTRAINT sys_dict_language_dict_id_fkey FOREIGN KEY (dict_id) REFERENCES sys_dict (id) ON DELETE CASCADE"}},
	} {
		for _, statement := range alter[0] {
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		if err := my.ValidateReady(ctx); !errors.Is(err, app.ErrUnavailable) {
			t.Errorf("%s must make the store unavailable, got %v", name, err)
		}
		for _, statement := range alter[1] {
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("restoring after %s: %v", name, err)
			}
		}
		if err := my.ValidateReady(ctx); err != nil {
			t.Fatalf("after restoring (%s) the store must be ready again: %v", name, err)
		}
	}
	_ = sqldialect.MySQL
}
