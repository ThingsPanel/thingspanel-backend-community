package plugininstall

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func pgEnv(t *testing.T) *env {
	dsn := os.Getenv("TEST_PG_SCHEMA_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_SCHEMA_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := os.ReadFile(filepath.Join("..", "..", "sql", "34.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(ddl)).Error; err != nil {
		t.Fatal(err)
	}
	v := newEnv(t)
	v.db = db
	v.e.Repo = NewRepo(func() *gorm.DB { return db })
	return v
}

// The lease and the one-row-per-plugin rule must hold on the real engine, not only in SQLite.
func TestPostgresLeaseAndSingleRow(t *testing.T) {
	v := pgEnv(t)
	tenant := "t" + uuid.NewString()[:12]
	t.Cleanup(func() { v.db.Where("tenant_id = ?", tenant).Delete(&Installation{}) })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = v.e.Submit(context.Background(), tenant, "homeassistant", "", fullConfig())
		}()
	}
	wg.Wait()
	var n int64
	v.db.Model(&Installation{}).Where("tenant_id = ?", tenant).Count(&n)
	if n != 1 {
		t.Fatalf("%d rows", n)
	}
	row, _ := v.e.Repo.ByPlugin(tenant, "homeassistant")
	won := make(chan bool, 8)
	now := time.Now().UTC()
	for i := 0; i < 8; i++ {
		go func(i int) {
			ok, _ := v.e.Repo.Claim(row.ID, "w"+string(rune('a'+i)), now, now.Add(time.Minute))
			won <- ok
		}(i)
	}
	winners := 0
	for i := 0; i < 8; i++ {
		if <-won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d workers own the lease", winners)
	}
}
