package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"encore.app/platformconfig/app"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func postgresFixture(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("TP_PLATFORMCONFIG_TEST_DSN")
	if dsn == "" {
		t.Skip("TP_PLATFORMCONFIG_TEST_DSN is not configured")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid private dictionary fixture DSN")
	}
	loopback := func(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
	if !loopback(config.Host) || config.Database != "tp_takeover_platformconfig" || config.User != "platformconfig_fixture" {
		t.Fatal("refusing dictionary fixture outside dedicated loopback database and role")
	}
	for _, fallback := range config.Fallbacks {
		if !loopback(fallback.Host) {
			t.Fatal("dictionary fixture has non-loopback fallback")
		}
	}
	admin := stdlib.OpenDB(*config)
	admin.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var database, user string
	if err := admin.QueryRowContext(ctx, `SELECT current_database(),current_user`).Scan(&database, &user); err != nil || database != "tp_takeover_platformconfig" || user != "platformconfig_fixture" {
		t.Fatal("dictionary fixture database identity mismatch")
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "tp_takeover_dictionary_" + hex.EncodeToString(nonce[:])
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`) })
	copyConfig := config.Copy()
	copyConfig.RuntimeParams = map[string]string{}
	for key, value := range config.RuntimeParams {
		copyConfig.RuntimeParams[key] = value
	}
	copyConfig.RuntimeParams["search_path"] = schema
	copyConfig.RuntimeParams["application_name"] = schema
	db := stdlib.OpenDB(*copyConfig)
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = db.Close() })
	legacy, err := os.ReadFile(filepath.Join("..", "..", "..", "sql", "1.sql"))
	if err != nil {
		t.Fatal("legacy schema source unavailable: ", err)
	}
	for _, table := range []string{"sys_dict", "sys_dict_language"} {
		start := strings.Index(string(legacy), "CREATE TABLE public."+table+" (")
		if start < 0 {
			t.Fatal("legacy table declaration absent: ", table)
		}
		remaining := string(legacy[start:])
		end := strings.Index(remaining, "\n);")
		if end < 0 {
			t.Fatal("legacy table declaration incomplete")
		}
		ddl := strings.ReplaceAll(remaining[:end+3], "public.", "")
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatal("apply exact legacy table DDL: ", err)
		}
	}
	return New(db), db
}

func createDictionary(t *testing.T, s *Store, id, code, value string) {
	t.Helper()
	if err := s.CreateDictionary(context.Background(), app.Dictionary{ID: id, DictCode: code, DictValue: value, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}); err != nil {
		t.Fatal(err)
	}
}
func rowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if table != "sys_dict" && table != "sys_dict_language" {
		t.Fatal("invalid test table")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestDictionaryPostgresCRUDAndLegacyData(t *testing.T) {
	s, db := postgresFixture(t)
	ctx := context.Background()
	createDictionary(t, s, "opaque-old-id", "legacy", "one")
	createDictionary(t, s, "opaque-old-two", "legacy", "two")
	if err := s.CreateLanguage(ctx, app.Language{ID: "old-language-id", DictID: "opaque-old-id", LanguageCode: "en_US", Translation: "first"}); err != nil {
		t.Fatal(err)
	}
	values, err := s.Enum(ctx, "legacy", "en_US")
	if err != nil || len(values) != 2 || values[0].Translation != "first" || values[1].Translation != "" {
		t.Fatalf("legacy enum=%#v err=%v", values, err)
	}
	page, err := s.Page(ctx, app.PageInput{Page: 1, PageSize: 1})
	if err != nil || page.Total != 2 || len(page.List) != 1 || page.List[0].ID != "opaque-old-two" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	languages, err := s.Languages(ctx, "opaque-old-id")
	if err != nil || len(languages) != 1 || languages[0].ID != "old-language-id" {
		t.Fatalf("languages=%#v err=%v", languages, err)
	}
	if err := s.CreateDictionary(ctx, app.Dictionary{ID: "duplicate-id", DictCode: "legacy", DictValue: "one", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("duplicate value=%v", err)
	}
	if err := s.CreateLanguage(ctx, app.Language{ID: "duplicate-language", DictID: "opaque-old-id", LanguageCode: "en_US", Translation: "changed"}); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("duplicate translation=%v", err)
	}
	if err := s.DeleteDictionary(ctx, "opaque-old-id"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDictionary(ctx, "opaque-old-id"); err != nil {
		t.Fatal("delete replay: ", err)
	}
	if rowCount(t, db, "sys_dict_language") != 0 || rowCount(t, db, "sys_dict") != 1 {
		t.Fatal("cascade did not preserve unrelated dictionary")
	}
	if err := s.DeleteLanguage(ctx, "old-language-id"); err != nil {
		t.Fatal("child deletion replay: ", err)
	}
	if err := s.CreateLanguage(ctx, app.Language{ID: "orphan", DictID: "opaque-old-id", LanguageCode: "en_US", Translation: "bad"}); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("orphan creation=%v", err)
	}
}

func TestDictionaryPostgresWriteAcknowledgement(t *testing.T) {
	for _, table := range []string{"sys_dict", "sys_dict_language"} {
		for _, operation := range []string{"INSERT", "DELETE"} {
			for _, mode := range []string{"zero", "raise"} {
				t.Run(table+"/"+operation+"/"+mode, func(t *testing.T) {
					s, db := postgresFixture(t)
					ctx := context.Background()
					if table == "sys_dict_language" || operation == "DELETE" {
						createDictionary(t, s, "parent", "code", "value")
					}
					child := app.Language{ID: "child", DictID: "parent", LanguageCode: "en_US", Translation: "text"}
					if table == "sys_dict_language" && operation == "DELETE" {
						if err := s.CreateLanguage(ctx, child); err != nil {
							t.Fatal(err)
						}
					}
					beforeParent, beforeChild := rowCount(t, db, "sys_dict"), rowCount(t, db, "sys_dict_language")
					body := "RETURN NULL;"
					if mode == "raise" {
						body = "RAISE EXCEPTION 'fixture rejects dictionary write' USING ERRCODE='23514';"
					}
					if _, err := db.Exec(`CREATE FUNCTION reject_dictionary_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN ` + body + ` END $$; CREATE TRIGGER reject_write BEFORE ` + operation + ` ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION reject_dictionary_write()`); err != nil {
						t.Fatal(err)
					}
					var err error
					switch {
					case table == "sys_dict" && operation == "INSERT":
						err = s.CreateDictionary(ctx, app.Dictionary{ID: "new", DictCode: "new", DictValue: "value", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)})
					case table == "sys_dict" && operation == "DELETE":
						err = s.DeleteDictionary(ctx, "parent")
					case operation == "INSERT":
						err = s.CreateLanguage(ctx, child)
					default:
						err = s.DeleteLanguage(ctx, "child")
					}
					if err == nil {
						t.Fatal("suppressed or failed SQL write was acknowledged")
					}
					if mode == "zero" && !errors.Is(err, app.ErrWriteNotApplied) {
						t.Fatalf("zero-row error=%v", err)
					}
					if mode == "raise" {
						var state interface{ SQLState() string }
						if !errors.As(err, &state) || state.SQLState() != "23514" {
							t.Fatalf("unexpected SQL failure: %v", err)
						}
					}
					if rowCount(t, db, "sys_dict") != beforeParent || rowCount(t, db, "sys_dict_language") != beforeChild {
						t.Fatal("failed write changed persistent data")
					}
				})
			}
		}
	}
	for _, table := range []string{"sys_dict", "sys_dict_language"} {
		t.Run(table+"/rewrite", func(t *testing.T) {
			s, db := postgresFixture(t)
			column := "dict_value"
			if table == "sys_dict_language" {
				createDictionary(t, s, "parent", "code", "value")
				column = "translation"
			}
			if _, err := db.Exec(`CREATE FUNCTION rewrite_dictionary_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.` + column + ` := 'unexpected'; RETURN NEW; END $$; CREATE TRIGGER rewrite_write BEFORE INSERT ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION rewrite_dictionary_write()`); err != nil {
				t.Fatal(err)
			}
			var err error
			if table == "sys_dict" {
				err = s.CreateDictionary(context.Background(), app.Dictionary{ID: "new", DictCode: "code", DictValue: "expected", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)})
			} else {
				err = s.CreateLanguage(context.Background(), app.Language{ID: "child", DictID: "parent", LanguageCode: "en_US", Translation: "expected"})
			}
			if !errors.Is(err, app.ErrWriteNotApplied) || rowCount(t, db, table) != 0 {
				t.Fatalf("rewritten row was accepted: %v", err)
			}
		})
	}
}

type acknowledgementLoss struct {
	transaction
	commitLoss   bool
	rollbackLoss bool
}

func (t *acknowledgementLoss) Commit() error {
	err := t.transaction.Commit()
	if err == nil && t.commitLoss {
		return errors.New("fixture lost actual commit acknowledgement")
	}
	return err
}
func (t *acknowledgementLoss) Rollback() error {
	err := t.transaction.Rollback()
	if err == nil && t.rollbackLoss {
		return errors.New("fixture lost actual rollback acknowledgement")
	}
	return err
}

func TestDictionaryPostgresTransactionUnknown(t *testing.T) {
	t.Run("actual committed create is unknown and remains queryable", func(t *testing.T) {
		s, db := postgresFixture(t)
		original := s.begin
		s.begin = func(ctx context.Context, o *sql.TxOptions) (transaction, error) {
			tx, e := original(ctx, o)
			if e != nil {
				return nil, e
			}
			return &acknowledgementLoss{transaction: tx, commitLoss: true}, nil
		}
		err := s.CreateDictionary(context.Background(), app.Dictionary{ID: "lost-create", DictCode: "code", DictValue: "value", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)})
		if !errors.Is(err, app.ErrOutcomeUnknown) || rowCount(t, db, "sys_dict") != 1 {
			t.Fatalf("commit loss=%v", err)
		}
		values, err := New(db).Enum(context.Background(), "code", "en_US")
		if err != nil || len(values) != 1 || values[0].DictValue != "value" {
			t.Fatalf("read existing outcome=%#v/%v", values, err)
		}
	})
	t.Run("actual committed delete is unknown and replay is safe", func(t *testing.T) {
		s, db := postgresFixture(t)
		createDictionary(t, s, "parent", "code", "value")
		original := s.begin
		s.begin = func(ctx context.Context, o *sql.TxOptions) (transaction, error) {
			tx, e := original(ctx, o)
			if e != nil {
				return nil, e
			}
			return &acknowledgementLoss{transaction: tx, commitLoss: true}, nil
		}
		if err := s.DeleteDictionary(context.Background(), "parent"); !errors.Is(err, app.ErrOutcomeUnknown) || rowCount(t, db, "sys_dict") != 0 {
			t.Fatalf("delete commit loss=%v", err)
		}
		if err := New(db).DeleteDictionary(context.Background(), "parent"); err != nil {
			t.Fatal("deleted replay: ", err)
		}
	})
	t.Run("rollback acknowledgement loss never becomes confirmed failure", func(t *testing.T) {
		s, db := postgresFixture(t)
		if _, err := db.Exec(`CREATE FUNCTION reject_dictionary_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$; CREATE TRIGGER reject_write BEFORE INSERT ON sys_dict FOR EACH ROW EXECUTE FUNCTION reject_dictionary_write()`); err != nil {
			t.Fatal(err)
		}
		original := s.begin
		s.begin = func(ctx context.Context, o *sql.TxOptions) (transaction, error) {
			tx, e := original(ctx, o)
			if e != nil {
				return nil, e
			}
			return &acknowledgementLoss{transaction: tx, rollbackLoss: true}, nil
		}
		err := s.CreateDictionary(context.Background(), app.Dictionary{ID: "rejected", DictCode: "code", DictValue: "value", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)})
		if !errors.Is(err, app.ErrOutcomeUnknown) || rowCount(t, db, "sys_dict") != 0 {
			t.Fatalf("rollback loss=%v", err)
		}
	})
}

func watchTransactionPID(t *testing.T, s *Store) <-chan int {
	t.Helper()
	pids := make(chan int, 1)
	original := s.begin
	s.begin = func(ctx context.Context, o *sql.TxOptions) (transaction, error) {
		tx, e := original(ctx, o)
		if e != nil {
			return nil, e
		}
		var pid int
		if e = tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
			_ = tx.Rollback()
			return nil, e
		}
		pids <- pid
		return tx, nil
	}
	return pids
}

func waitForLock(t *testing.T, db *sql.DB, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("transaction did not reach the real PostgreSQL parent lock")
}

func TestDictionaryPostgresParentBarrierAndCancellation(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		name := "delete wins"
		if cancelRequest {
			name = "request cancellation rolls back blocked insert"
		}
		t.Run(name, func(t *testing.T) {
			s, db := postgresFixture(t)
			createDictionary(t, s, "parent", "code", "value")
			parent, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer parent.Rollback()
			if _, err := parent.Exec(`DELETE FROM sys_dict WHERE id='parent'`); err != nil {
				t.Fatal(err)
			}
			pids := watchTransactionPID(t, s)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- s.CreateLanguage(ctx, app.Language{ID: "child", DictID: "parent", LanguageCode: "en_US", Translation: "text"})
			}()
			var pid int
			select {
			case pid = <-pids:
			case <-time.After(5 * time.Second):
				t.Fatal("insert transaction did not start")
			}
			waitForLock(t, db, pid)
			if cancelRequest {
				cancel()
			} else {
				if err := parent.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("insert succeeded after parent deletion/cancellation")
				}
				if !cancelRequest && !errors.Is(err, app.ErrNotFound) {
					t.Fatalf("deleted parent error=%v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("blocked insert did not finish")
			}
			if cancelRequest {
				if err := parent.Rollback(); err != nil {
					t.Fatal(err)
				}
			}
			if rowCount(t, db, "sys_dict_language") != 0 {
				t.Fatal("orphan or canceled insertion committed")
			}
		})
	}

	t.Run("insert holds parent until commit and delete cascades", func(t *testing.T) {
		s, db := postgresFixture(t)
		createDictionary(t, s, "parent", "code", "value")
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		releaseCommit := func() { releaseOnce.Do(func() { close(release) }) }
		defer releaseCommit()
		original := s.begin
		s.begin = func(ctx context.Context, o *sql.TxOptions) (transaction, error) {
			tx, e := original(ctx, o)
			if e != nil {
				return nil, e
			}
			return &heldCommit{transaction: tx, entered: entered, release: release}, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		insertDone := make(chan error, 1)
		go func() {
			insertDone <- s.CreateLanguage(ctx, app.Language{ID: "child", DictID: "parent", LanguageCode: "en_US", Translation: "text"})
		}()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			releaseCommit()
			t.Fatal("insert did not reach commit")
		}
		deleter := New(db)
		pids := watchTransactionPID(t, deleter)
		deleteDone := make(chan error, 1)
		go func() { deleteDone <- deleter.DeleteDictionary(ctx, "parent") }()
		var pid int
		select {
		case pid = <-pids:
		case <-time.After(5 * time.Second):
			releaseCommit()
			t.Fatal("delete did not begin")
		}
		waitForLock(t, db, pid)
		releaseCommit()
		if err := <-insertDone; err != nil {
			t.Fatal(err)
		}
		if err := <-deleteDone; err != nil {
			t.Fatal(err)
		}
		if rowCount(t, db, "sys_dict") != 0 || rowCount(t, db, "sys_dict_language") != 0 {
			t.Fatal("parent/child ownership was not serialized")
		}
	})
}

type heldCommit struct {
	transaction
	entered chan struct{}
	release chan struct{}
}

func (t *heldCommit) Commit() error { close(t.entered); <-t.release; return t.transaction.Commit() }
