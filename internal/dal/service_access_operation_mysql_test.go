package dal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"project/internal/model"
	"project/internal/query"
	componentsql "project/sql"
)

func TestServiceAccessOperationsBehaveTheSameOnMySQLAsOnPostgres(t *testing.T) {
	pgDSN := os.Getenv("TEST_PG_SCHEMA_DSN")
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if pgDSN == "" || host == "" {
		t.Skip("TEST_PG_SCHEMA_DSN (service_access and its receipts built) and TP_MYSQL_TEST_* required")
	}
	pg, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pgPool, _ := pg.DB()
	pgPool.SetMaxOpenConns(8)
	cfg := mysqldriver.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr = os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), "tcp", host+":"+port
	cfg.DBName, cfg.ParseTime, cfg.Loc, cfg.ClientFoundRows = os.Getenv("TP_MYSQL_TEST_DB"), true, time.UTC, true
	my, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"service_access_create_receipts", "service_access"} {
		_ = my.Exec("DROP TABLE IF EXISTS " + table).Error
	}
	for _, name := range componentsql.MySQLServiceAccessMigrationNames() {
		statements, err := componentsql.MySQLMigrationStatements(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range statements {
			if err := my.Exec(s).Error; err != nil {
				t.Fatalf("apply %s: %v", name, err)
			}
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"service_access_create_receipts", "service_access"} {
			_ = my.Exec("DROP TABLE IF EXISTS " + table).Error
		}
	})
	suffix := uuid.NewString()[:8]
	tenant := "sa-" + suffix
	hash := func(c string) string { return strings.Repeat(c, 64) }
	config := "{\"b\": 1,   \"a\": [1, 2]}" // spacing and key order must survive the round trip
	moment := time.Date(2026, 11, 3, 1, 2, 3, 123456000, time.UTC)

	run := func(db *gorm.DB) []string {
		query.SetDefault(db)
		var out []string
		note := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
		kind := func(err error) string {
			switch {
			case err == nil:
				return "ok"
			case errors.Is(err, ErrServiceAccessOperationConflict):
				return "conflict"
			case errors.Is(err, ErrServiceAccessResourceChanged):
				return "changed"
			case errors.Is(err, ErrServiceAccessOperationUnknown):
				return "unknown"
			case errors.Is(err, gorm.ErrRecordNotFound):
				return "not-found"
			}
			return "error"
		}
		ctx := context.Background()
		access := func(id string) *model.ServiceAccess {
			desc, cfg := "d", config
			return &model.ServiceAccess{ID: id + "-" + suffix, Name: "n", ServicePluginID: "plugin", Voucher: "v-" + id + suffix, Description: &desc, ServiceAccessConfig: &cfg, TenantID: tenant, CreateAt: moment, UpdateAt: moment}
		}
		obs, err := CreateServiceAccessWithOperation(ctx, access("one"), "subject", "key-1", hash("a"))
		note("create: %s state=%v replayed=%v", kind(err), obsState(obs), obsReplayed(obs))
		obs, err = CreateServiceAccessWithOperation(ctx, access("one"), "subject", "key-1", hash("a"))
		note("same key and request: %s state=%v replayed=%v", kind(err), obsState(obs), obsReplayed(obs))
		_, err = CreateServiceAccessWithOperation(ctx, access("one"), "subject", "key-1", hash("b"))
		note("same key, other request hash: %s", kind(err))
		_, err = CreateServiceAccessWithOperation(ctx, access("one"), "someone-else", "key-1", hash("a"))
		note("same key, other subject: %s", kind(err))
		_, err = CreateServiceAccessWithOperation(ctx, access("one"), "subject", "key-2", hash("a"))
		note("new key for an existing resource id fails: %v", kind(err) != "ok")
		var receipts int64
		db.Model(&ServiceAccessCreateReceipt{}).Where("tenant_id = ?", tenant).Count(&receipts)
		note("receipts after the failed second creation (atomic): %d", receipts)
		obs, err = ReadServiceAccessCreateOperation(ctx, tenant, "subject", "key-1")
		note("read: %s state=%v", kind(err), obsState(obs))
		obs, err = ReadServiceAccessCreateOperation(ctx, tenant, "subject", "never-used")
		note("read unknown key: %s state=%v", kind(err), obsState(obs))
		_, err = ReadServiceAccessCreateOperation(ctx, tenant, "someone-else", "key-1")
		note("read as another subject: %s", kind(err))
		// Concurrent replays of one key.
		var wg sync.WaitGroup
		results := make(chan string, 6)
		for i := 0; i < cap(results); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				o, err := CreateServiceAccessWithOperation(ctx, access("race"), "subject", "key-race", hash("c"))
				if err != nil {
					results <- "error:" + kind(err)
					return
				}
				results <- fmt.Sprintf("replayed=%v", o.Replayed)
			}()
		}
		wg.Wait()
		close(results)
		counts := map[string]int{}
		for r := range results {
			counts[r]++
		}
		note("concurrent creation of one key: fresh=%d replayed=%d errors=%d", counts["replayed=false"], counts["replayed=true"], len(counts)-boolToInt(counts["replayed=false"] > 0)-boolToInt(counts["replayed=true"] > 0))
		// Change the resource behind the receipt.
		if err := db.Exec("UPDATE service_access SET name = 'renamed' WHERE id = ?", "one-"+suffix).Error; err != nil {
			t.Fatal(err)
		}
		obs, err = ReadServiceAccessCreateOperation(ctx, tenant, "subject", "key-1")
		note("read after the resource changed: %s state=%v reason=%v", kind(err), obsState(obs), obsReason(obs))
		_, err = CreateServiceAccessWithOperation(ctx, access("one"), "subject", "key-1", hash("a"))
		note("replay after the resource changed: %s", kind(err))
		if err := db.Exec("DELETE FROM service_access WHERE id = ?", "one-"+suffix).Error; err != nil {
			t.Fatal(err)
		}
		obs, err = ReadServiceAccessCreateOperation(ctx, tenant, "subject", "key-1")
		note("read after the resource was deleted: %s state=%v reason=%v", kind(err), obsState(obs), obsReason(obs))
		var stored string
		db.Raw("SELECT service_access_config FROM service_access WHERE id = ?", "race-"+suffix).Scan(&stored)
		note("configuration kept byte for byte: %v", stored == config)
		return out
	}
	t.Cleanup(func() {
		_ = pg.Exec("DELETE FROM service_access_create_receipts WHERE tenant_id = ?", tenant).Error
		_ = pg.Exec("DELETE FROM service_access WHERE tenant_id = ?", tenant).Error
	})
	// The oracle's foreign key to service_plugins is not what is compared here.
	if err := pg.Exec("SET session_replication_role = replica").Error; err != nil {
		t.Fatal(err)
	}
	pgPool.SetMaxOpenConns(1)
	a := run(pg)
	b := run(my)
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
	for _, want := range []string{"create: ok state=confirmed replayed=false", "same key and request: ok state=confirmed replayed=true", "conflict", "receipts after the failed second creation (atomic): 1", "configuration kept byte for byte: true", "reason=RESOURCE_CHANGED", "reason=RESOURCE_MISSING"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the oracle scenario lacks %q:\n%s", want, joined)
		}
	}
}

func obsState(o *model.ServiceAccessCreateObservation) any {
	if o == nil {
		return nil
	}
	return o.State
}
func obsReplayed(o *model.ServiceAccessCreateObservation) any {
	if o == nil {
		return nil
	}
	return o.Replayed
}
func obsReason(o *model.ServiceAccessCreateObservation) any {
	if o == nil {
		return nil
	}
	return o.Reason
}
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
