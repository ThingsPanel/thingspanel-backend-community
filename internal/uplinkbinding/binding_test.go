package uplinkbinding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/brokersemantic"
	"project/internal/uplinkcontract"
)

func bindingDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_BROKER_DATABASE_DSN required for real PostgreSQL fencing")
	}
	if strings.Contains(dsn, "://") {
		t.Fatal("use libpq keyword DSN for isolated search_path")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := "qa_uplink_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err = base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sql, _ := db.DB()
		sql.Close()
		base.Exec("DROP SCHEMA " + schema + " CASCADE")
		sql, _ = base.DB()
		sql.Close()
	})
	for _, sql := range []string{
		`CREATE TABLE devices(id text PRIMARY KEY, tenant_id text NOT NULL,is_enabled text NOT NULL,device_config_id text,parent_id text,sub_device_addr text)`,
		`CREATE TABLE device_configs(id text PRIMARY KEY,tenant_id text NOT NULL,device_template_id text,native_thing_model_id text,native_thing_model_snapshot_id text,native_thing_model_revision integer,native_binding_revision integer NOT NULL DEFAULT 0)`,
		`INSERT INTO devices VALUES('d','a','enabled','c',NULL,NULL),('legacy','a','enabled',NULL,NULL,NULL),('foreign','b','enabled',NULL,NULL,NULL)`,
		`INSERT INTO device_configs VALUES('c','a',NULL,'m','s',1,1)`,
	} {
		if err = db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestPostgresUplinkBindingFencesMutations(t *testing.T) {
	db := bindingDB(t)
	ctx := context.Background()
	s, err := Read(ctx, db, "a", "d")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Check(ctx, db); !errors.Is(err, ErrFenced) {
		t.Fatalf("bare DB must not pass: %v", err)
	}
	if err = db.Transaction(func(tx *gorm.DB) error { return s.Check(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
	if _, err = Read(ctx, db, "a", "foreign"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross tenant: %v", err)
	}
	for _, tc := range []struct{ name, sql string }{
		{"pin", `UPDATE device_configs SET native_thing_model_snapshot_id='other',native_binding_revision=2 WHERE id='c'`},
		{"unbinding", `UPDATE device_configs SET native_thing_model_id=NULL,native_thing_model_snapshot_id=NULL,native_thing_model_revision=NULL,native_binding_revision=2 WHERE id='c'`},
		{"device config", `UPDATE devices SET device_config_id=NULL WHERE id='d'`},
		{"parent", `UPDATE devices SET parent_id='gateway',sub_device_addr='02' WHERE id='d'`},
		{"disabled", `UPDATE devices SET is_enabled='disabled' WHERE id='d'`},
		{"tenant", `UPDATE devices SET tenant_id='b' WHERE id='d'`},
		{"partial binding", `UPDATE device_configs SET native_thing_model_snapshot_id=NULL WHERE id='c'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := db.Begin()
			defer tx.Rollback()
			if err := tx.Exec(tc.sql).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.Check(ctx, tx); err == nil {
				t.Fatal("stale publication allowed write")
			}
		})
	}
	legacy, err := Read(ctx, db, "a", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err = legacy.Validate(ctx, nil, "telemetry", []byte("raw legacy")); err != nil {
		t.Fatalf("legacy contract changed: %v", err)
	}
	if err = db.Exec(`UPDATE devices SET device_config_id='c' WHERE id='legacy'`).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Transaction(func(tx *gorm.DB) error { return legacy.Check(ctx, tx) }); !errors.Is(err, ErrFenced) {
		t.Fatalf("unbound to bound not fenced: %v", err)
	}
}

func TestPostgresUplinkBindingColdHTTPDoesNotHoldWriteLock(t *testing.T) {
	db := bindingDB(t)
	ctx := context.Background()
	s, err := Read(ctx, db, "a", "d")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	const raw = `{"name":"fixture","fields":[{"kind":"TELEMETRY","identifier":"temperature","nameI18n":{"default":"T"},"access":{"read":true},"valueType":{"kind":"NUMBER"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		h := sha256.Sum256([]byte(raw))
		json.NewEncoder(w).Encode(map[string]any{"schema_version": 1, "tenant_id": "a", "model_id": "m", "snapshot_id": "s", "revision": 1, "contract_sha256": hex.EncodeToString(h[:]), "model": json.RawMessage(raw)})
	}))
	defer server.Close()
	client, err := uplinkcontract.NewClient(uplinkcontract.Config{BaseURL: server.URL, Token: strings.Repeat("x", 32), Deployment: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	validated := make(chan error, 1)
	go func() { validated <- s.Validate(ctx, client, "telemetry", []byte(`{"temperature":12}`)) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("owner request missing")
	}
	// This separate connection must be able to change the publication while the
	// remote cold load is blocked. Validation itself never opens a transaction.
	updateErr := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout='250ms'").Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE device_configs SET native_thing_model_snapshot_id='new',native_binding_revision=2 WHERE id='c'`).Error
	})
	close(release)
	if updateErr != nil {
		t.Fatalf("network held DB lock: %v", updateErr)
	}
	if err = <-validated; err != nil {
		t.Fatal(err)
	}
	if err = db.Transaction(func(tx *gorm.DB) error { return s.Check(ctx, tx) }); !errors.Is(err, ErrFenced) {
		t.Fatalf("cold load binding race accepted: %v", err)
	}
	if err = s.Validate(ctx, client, "telemetry", []byte(`{"temperature":"12"}`)); !errors.Is(err, brokersemantic.ErrUplinkInvalid) {
		t.Fatalf("strict typed failure: %v", err)
	}
}

func TestPostgresUplinkBindingWriteFenceHoldsUntilCommit(t *testing.T) {
	db := bindingDB(t)
	ctx := context.Background()
	s, err := Read(ctx, db, "a", "d")
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	defer tx.Rollback()
	if err = s.Check(ctx, tx); err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(other *gorm.DB) error {
		if e := other.Exec("SET LOCAL lock_timeout='100ms'").Error; e != nil {
			return e
		}
		return other.Exec(`UPDATE device_configs SET native_binding_revision=2 WHERE id='c'`).Error
	})
	if err == nil {
		t.Fatal("concurrent rebinding was not fenced through commit")
	}
	if !strings.Contains(fmt.Sprint(err), "55P03") {
		t.Fatalf("expected real row lock timeout: %v", err)
	}
	if err = tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`UPDATE device_configs SET native_binding_revision=2 WHERE id='c'`).Error; err != nil {
		t.Fatal(err)
	}
}
