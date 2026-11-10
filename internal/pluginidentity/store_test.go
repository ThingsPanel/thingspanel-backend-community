package pluginidentity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"project/sql"

	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The same behaviour must hold on PostgreSQL (sql/33.sql) and MySQL (sql/mysql/015_plugin_runtime_identity.sql).
func openBackends(t *testing.T) map[string]*gorm.DB {
	backends := map[string]*gorm.DB{}
	if dsn := os.Getenv("TEST_PG_SCHEMA_DSN"); dsn != "" {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		ddl, err := os.ReadFile(filepath.Join("..", "..", "sql", "33.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(ddl)).Error; err != nil {
			t.Fatal(err)
		}
		backends["postgres"] = db
	}
	if host := os.Getenv("TP_MYSQL_TEST_HOST"); host != "" {
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=UTC&multiStatements=true", os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"),
			host, os.Getenv("TP_MYSQL_TEST_PORT"), os.Getenv("TP_MYSQL_TEST_DB"))
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range sql.MySQLPluginRuntimeIdentityMigrationNames() {
			statements, err := sql.MySQLFamilyMigrationStatements(os.Getenv("TP_MYSQL_TEST_FLAVOR"), name)
			if err != nil {
				t.Fatal(err)
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
		backends["mysql"] = db
	}
	if len(backends) == 0 {
		t.Skip("neither TEST_PG_SCHEMA_DSN nor TP_MYSQL_TEST_HOST is configured (scripts/dev-databases.sh)")
	}
	return backends
}

func forEachBackend(t *testing.T, fn func(t *testing.T, s *Store, tenant string, db *gorm.DB)) {
	for name, db := range openBackends(t) {
		db := db
		t.Run(name, func(t *testing.T) {
			tenant := "t" + uuid.NewString()[:12]
			t.Cleanup(func() { db.Where("tenant_id LIKE ?", tenant+"%").Delete(&Record{}) })
			fn(t, NewStore(func() *gorm.DB { return db }), tenant, db)
		})
	}
}

func TestIssueAuthenticateRevoke(t *testing.T) {
	forEachBackend(t, func(t *testing.T, s *Store, tenant string, db *gorm.DB) {
		now := time.Now().UTC()
		token, rec, err := s.Issue(tenant, "inst", "svc", 1, time.Hour, now)
		if err != nil {
			t.Fatal(err)
		}
		var stored Record
		db.First(&stored, "instance_id = ?", rec.InstanceID)
		if stored.SecretSHA256 == "" || len(token) < 60 || stored.SecretSHA256 == token {
			t.Fatal("only a hash of the secret may be stored")
		}
		id, err := s.Authenticate(token, now.Add(time.Minute))
		if err != nil || id.TenantID != tenant || id.ServiceIdentifier != "svc" || id.Generation != 1 {
			t.Fatalf("authenticate: %v %+v", err, id)
		}
		n, err := s.Revoke(tenant, "inst", now.Add(2*time.Minute))
		if err != nil || n != 1 {
			t.Fatalf("revoke: %d %v", n, err)
		}
		if _, err := s.Authenticate(token, now.Add(3*time.Minute)); !errors.Is(err, ErrDenied) {
			t.Fatalf("revoked identity must be denied, got %v", err)
		}
	})
}

func TestHigherGenerationRevokesLowerAndExpiryDenies(t *testing.T) {
	forEachBackend(t, func(t *testing.T, s *Store, tenant string, db *gorm.DB) {
		now := time.Now().UTC()
		t1, _, _ := s.Issue(tenant, "inst", "svc", 1, time.Hour, now)
		t2, _, err := s.Issue(tenant, "inst", "svc", 2, time.Hour, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Authenticate(t1, now); !errors.Is(err, ErrDenied) {
			t.Fatalf("generation 1 must be revoked, got %v", err)
		}
		if _, err := s.Authenticate(t2, now); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Issue(tenant, "inst", "svc", 2, time.Hour, now); !errors.Is(err, ErrGenerationNotHigher) {
			t.Fatalf("same generation must conflict, got %v", err)
		}
		if _, _, err := s.Issue(tenant, "inst", "svc", 1, time.Hour, now); !errors.Is(err, ErrGenerationNotHigher) {
			t.Fatalf("lower generation must conflict, got %v", err)
		}
		if _, err := s.Authenticate(t2, now.Add(time.Hour+time.Second)); !errors.Is(err, ErrDenied) {
			t.Fatalf("an expired lease must be denied, got %v", err)
		}
	})
}

func TestConcurrentIssueOfTheSameGenerationYieldsExactlyOne(t *testing.T) {
	forEachBackend(t, func(t *testing.T, s *Store, tenant string, db *gorm.DB) {
		now := time.Now().UTC()
		var wg sync.WaitGroup
		results := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, err := s.Issue(tenant, "race", "svc", 1, time.Hour, now)
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		ok := 0
		for err := range results {
			if err == nil {
				ok++
			}
		}
		var live int64
		db.Model(&Record{}).Where("tenant_id = ? AND plugin_instance = ? AND revoked_at IS NULL", tenant, "race").Count(&live)
		if ok != 1 || live != 1 {
			t.Fatalf("exactly one concurrent issue of a generation may succeed, ok=%d live=%d", ok, live)
		}
	})
}

func TestMalformedTokensAreDenied(t *testing.T) {
	forEachBackend(t, func(t *testing.T, s *Store, tenant string, db *gorm.DB) {
		for _, token := range []string{"", ".", "a.b", "pri_0000000000000000", "pri_0000000000000000.", "x.y.z", "eyJhbGciOiJIUzI1NiJ9.e30.c2ln"} {
			if _, err := s.Authenticate(token, time.Now().UTC()); !errors.Is(err, ErrDenied) {
				t.Fatalf("%q must be denied, got %v", token, err)
			}
		}
	})
}

func TestRenewExtendsLiveIdentityOnly(t *testing.T) {
	forEachBackend(t, func(t *testing.T, s *Store, tenant string, db *gorm.DB) {
		now := time.Now().UTC()
		token, _, err := s.Issue(tenant, "inst", "svc", 1, time.Hour, now)
		if err != nil {
			t.Fatal(err)
		}
		n, err := s.Renew(tenant, "inst", 24*time.Hour, now.Add(30*time.Minute))
		if err != nil || n != 1 {
			t.Fatalf("renew: %d %v", n, err)
		}
		if _, err := s.Authenticate(token, now.Add(5*time.Hour)); err != nil {
			t.Fatalf("the same token must work after renewal: %v", err)
		}
		// expired identities are never revived, revoked ones neither
		if n, _ := s.Renew(tenant, "inst", time.Hour, now.Add(48*time.Hour)); n != 0 {
			t.Fatal("an expired identity must not be renewed")
		}
		s.Revoke(tenant, "inst", now.Add(time.Hour))
		if n, _ := s.Renew(tenant, "inst", time.Hour, now.Add(2*time.Hour)); n != 0 {
			t.Fatal("a revoked identity must not be renewed")
		}
	})
}
