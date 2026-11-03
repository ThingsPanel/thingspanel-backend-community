package initialize

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	componentsql "project/sql"

	"project/internal/model"
)

// The oracle here is the real PostgreSQL device identity fence (sql/31.sql) with its trigger
// active; the subject is the MySQL translation in sql/mysql/002_device_identity.sql.
func openIdentityDialects(t *testing.T) (pg, my *gorm.DB) {
	t.Helper()
	pgDSN := os.Getenv("TEST_PG_SCHEMA_DSN")
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if pgDSN == "" || host == "" || port == "" {
		t.Skip("TEST_PG_SCHEMA_DSN and TP_MYSQL_TEST_* required")
	}
	pg, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	portNumber, _ := strconv.Atoi(port)
	my, err = MysqlConnect(&DbConfig{Host: host, Port: portNumber, Username: os.Getenv("TP_MYSQL_TEST_USER"), Password: os.Getenv("TP_MYSQL_TEST_PASSWORD"),
		DbName: os.Getenv("TP_MYSQL_TEST_DB"), TimeZone: "UTC", LogLevel: 1, IdleConns: 2, OpenConns: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range componentsql.MySQLMigrationNames() {
		statements, err := componentsql.MySQLMigrationStatements(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range statements {
			if err := my.Exec(statement).Error; err != nil {
				t.Fatalf("apply %s: %v\n%s", name, err, statement)
			}
		}
	}
	// Applying the migration again must be harmless (backfill is INSERT IGNORE, triggers are dropped first).
	for _, name := range componentsql.MySQLMigrationNames() {
		statements, _ := componentsql.MySQLMigrationStatements(name)
		for _, statement := range statements {
			if err := my.Exec(statement).Error; err != nil {
				t.Fatalf("re-apply %s: %v", name, err)
			}
		}
	}
	t.Cleanup(func() {
		_ = my.Exec("DROP TABLE IF EXISTS device_id_reservations").Error
		_ = my.Exec("DROP TABLE IF EXISTS devices, users").Error
		if p, e := my.DB(); e == nil {
			_ = p.Close()
		}
		if p, e := pg.DB(); e == nil {
			_ = p.Close()
		}
	})
	return pg, my
}

func TestMysqlDeviceIdentityFenceMatchesPostgres(t *testing.T) {
	pg, my := openIdentityDialects(t)
	now := time.Now().UTC()
	type step struct {
		name string
		do   func(db *gorm.DB, tenant, other, id string) error
	}
	reserve := func(db *gorm.DB, id, tenant string) error {
		return db.Exec("INSERT INTO device_id_reservations (id, tenant_id, state) VALUES (?, ?, 'active')", id, tenant).Error
	}
	newDevice := func(id, tenant string) *model.Device {
		return &model.Device{ID: id, Voucher: "v-" + id, TenantID: tenant, DeviceNumber: "n-" + id}
	}
	steps := []step{
		{"insert without reservation", func(db *gorm.DB, tenant, _, id string) error { return db.Create(newDevice(id, tenant)).Error }},
		{"reserve", func(db *gorm.DB, tenant, _, id string) error { return reserve(db, id, tenant) }},
		{"insert under another tenant", func(db *gorm.DB, _, other, id string) error { return db.Create(newDevice(id, other)).Error }},
		{"insert with matching reservation", func(db *gorm.DB, tenant, _, id string) error { return db.Create(newDevice(id, tenant)).Error }},
		{"update id", func(db *gorm.DB, _, _, id string) error {
			return db.Exec("UPDATE devices SET id = ? WHERE id = ?", id+"-x", id).Error
		}},
		{"update tenant", func(db *gorm.DB, _, other, id string) error {
			return db.Exec("UPDATE devices SET tenant_id = ? WHERE id = ?", other, id).Error
		}},
		{"update ordinary column", func(db *gorm.DB, _, _, id string) error {
			return db.Exec("UPDATE devices SET name = 'renamed' WHERE id = ?", id).Error
		}},
		{"delete while reservation active", func(db *gorm.DB, _, _, id string) error {
			return db.Exec("DELETE FROM devices WHERE id = ?", id).Error
		}},
		{"tombstone for another tenant is not enough", func(db *gorm.DB, _, other, id string) error {
			return db.Exec("UPDATE device_id_reservations SET state='tombstoned', deleted_at=?, tenant_id=? WHERE id=?", now, other, id).Error
		}},
		{"delete with mismatching tombstone", func(db *gorm.DB, _, _, id string) error {
			return db.Exec("DELETE FROM devices WHERE id = ?", id).Error
		}},
		{"tombstone for the right tenant", func(db *gorm.DB, tenant, _, id string) error {
			return db.Exec("UPDATE device_id_reservations SET tenant_id=? WHERE id=?", tenant, id).Error
		}},
		{"delete with tombstone", func(db *gorm.DB, _, _, id string) error {
			return db.Exec("DELETE FROM devices WHERE id = ?", id).Error
		}},
		{"re-insert a tombstoned identity", func(db *gorm.DB, tenant, _, id string) error { return db.Create(newDevice(id, tenant)).Error }},
		{"tombstoned without a deletion time", func(db *gorm.DB, _, _, id string) error {
			return db.Exec("INSERT INTO device_id_reservations (id, tenant_id, state) VALUES (?, 't', 'tombstoned')", id+"-bad").Error
		}},
		{"active with a deletion time", func(db *gorm.DB, _, _, id string) error {
			return db.Exec("INSERT INTO device_id_reservations (id, tenant_id, state, deleted_at) VALUES (?, 't', 'active', ?)", id+"-bad2", now).Error
		}},
		{"unknown state", func(db *gorm.DB, _, _, id string) error {
			return db.Exec("INSERT INTO device_id_reservations (id, tenant_id, state) VALUES (?, 't', 'zombie')", id+"-bad3").Error
		}},
	}
	suffix := uuid.NewString()[:8]
	tenant, other, id := "tenant-"+suffix, "other-"+suffix, "dev-"+suffix
	var pgOutcome, myOutcome []string
	record := func(out *[]string, name string, err error) {
		verdict := "ok"
		if err != nil {
			verdict = "rejected"
		}
		*out = append(*out, name+": "+verdict)
	}
	for _, s := range steps {
		record(&pgOutcome, s.name, s.do(pg, tenant, other, id))
		record(&myOutcome, s.name, s.do(my, tenant, other, id))
	}
	for i := range pgOutcome {
		if pgOutcome[i] != myOutcome[i] {
			t.Errorf("step differs\n  pg: %s\n  my: %s", pgOutcome[i], myOutcome[i])
		}
	}
	// The scenario must actually exercise both outcomes.
	joined := strings.Join(pgOutcome, "\n")
	if strings.Count(joined, ": rejected") < 7 || strings.Count(joined, ": ok") < 5 {
		t.Fatalf("the oracle did not behave as expected:\n%s", joined)
	}
	t.Cleanup(func() {
		_ = pg.Exec("DELETE FROM devices WHERE tenant_id IN (?, ?)", tenant, other).Error
		_ = pg.Exec("DELETE FROM device_id_reservations WHERE id LIKE ?", "dev-"+suffix+"%").Error
	})
}
