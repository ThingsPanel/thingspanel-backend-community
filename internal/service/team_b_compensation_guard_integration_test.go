package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"project/internal/model"
	"project/internal/repo"
	"project/pkg/global"
)

// This test only accepts the dedicated loopback database created for this fixture.
func TestTeamBCompensationGuard(t *testing.T) {
	dsn := os.Getenv("TP_B_COMPENSATION_DSN")
	if dsn == "" {
		t.Skip("dedicated fixture DSN not supplied")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/tp_b_comp_test" || u.User == nil || u.User.Username() != "tp_b_fixture" {
		t.Fatal("refusing non-fixture database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var dbname, username string
	if err = db.Raw("SELECT current_database(), current_user").Row().Scan(&dbname, &username); err != nil || dbname != "tp_b_comp_test" || username != "tp_b_fixture" {
		t.Fatal("unexpected database identity", err)
	}
	migration, err := os.ReadFile("../../migrations/008_create_market_bundle_install_tables.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	oldDB := global.DB
	global.DB = db
	defer func() { global.DB = oldDB }()
	var calls atomic.Int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "simulated remote failure", 500)
	}))
	defer fake.Close()
	service := &MarketBundleInstallService{installRepo: repo.NewMarketBundleInstallRepo(), thingsVis: NewThingsVisClientWithBaseURL(fake.URL)}
	ctx := context.Background()
	for i, tt := range []struct{ name, status, code, tenant string }{
		{"foreign tenant", "FAILED", "CREATE_FAILED", "tenant-b"},
		{"failed dashboard", "FAILED", "CREATE_FAILED", "tenant-a"},
		{"unknown mapping", "COMPENSATION_REQUIRED", "CREATE_FAILED", "tenant-a"},
		{"legacy claimed success", "FAILED", "COMPENSATED", "tenant-a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
			mid := fmt.Sprintf("10000000-0000-4000-8000-%012d", i+1)
			if err := db.Exec("INSERT INTO market_bundle_installations (id,idempotency_key,bundle_key,bundle_version,tenant_id,status,error_code,error_message) VALUES (?,?,?,?,?,?,?,?)", id, tt.name, "fixture", "1", "tenant-a", tt.status, tt.code, "original evidence").Error; err != nil {
				t.Fatal(err)
			}
			typ := "dashboard"
			if tt.name == "unknown mapping" {
				typ = "unknown"
			}
			if err := db.Exec("INSERT INTO market_resource_mappings (id,installation_id,tenant_id,resource_type,market_resource_key,market_version,local_id,status) VALUES (?,?,?,?,?,?,?,?)", mid, id, "tenant-a", typ, "key", "1", "remote-fixture", "active").Error; err != nil {
				t.Fatal(err)
			}
			for n := 0; n < 2; n++ {
				if err := service.CompensateInstallation(ctx, id, tt.tenant); err == nil {
					t.Fatal("compensation falsely succeeded")
				}
			}
			if _, err := service.RetryInstallation(ctx, id, tt.tenant, &model.RetryInstallationRequest{}); err == nil {
				t.Fatal("retry falsely succeeded")
			}
			var status, code, message, mapping string
			if err := db.Raw("SELECT status,error_code,error_message FROM market_bundle_installations WHERE id=?", id).Row().Scan(&status, &code, &message); err != nil {
				t.Fatal(err)
			}
			if err := db.Raw("SELECT status FROM market_resource_mappings WHERE id=?", mid).Row().Scan(&mapping); err != nil {
				t.Fatal(err)
			}
			expected := "COMPENSATION_REQUIRED"
			if tt.tenant == "tenant-b" {
				expected = tt.status
			}
			if status != expected || code != tt.code || message != "original evidence" || mapping != "active" {
				t.Fatalf("unexpected state %q %q %q %q", status, code, message, mapping)
			}
			var audits int64
			db.Raw("SELECT count(*) FROM market_installation_audit WHERE installation_id=?", id).Scan(&audits)
			if audits != 0 {
				t.Fatal("unexpected audit", audits)
			}
		})
	}
	t.Run("concurrent guard calls", func(t *testing.T) {
		id := "00000000-0000-4000-8000-000000000099"
		if err := db.Exec("INSERT INTO market_bundle_installations (id,idempotency_key,bundle_key,bundle_version,tenant_id,status) VALUES (?,?,?,?,?,?)", id, "concurrent", "fixture", "1", "tenant-a", "FAILED").Error; err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var successes atomic.Int64
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if service.CompensateInstallation(ctx, id, "tenant-a") == nil {
					successes.Add(1)
				}
			}()
		}
		wg.Wait()
		var status string
		if err := db.Raw("SELECT status FROM market_bundle_installations WHERE id=?", id).Scan(&status).Error; err != nil {
			t.Fatal(err)
		}
		if status != "COMPENSATION_REQUIRED" || successes.Load() != 0 {
			t.Fatal(status, successes.Load())
		}
	})
	if calls.Load() != 0 {
		t.Fatal("guard made a remote request", calls.Load())
	}
}
