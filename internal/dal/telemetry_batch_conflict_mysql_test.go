package dal

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Both engines must recover from a duplicate inside a mixed batch: update the
// existing sample and still insert the non-conflicting sample.
func TestTelemetryBatchConflictBehavesTheSameOnPostgresAndMySQL(t *testing.T) {
	pg, my := platformMySQL(t)
	createdPGTable := !pg.Migrator().HasTable("telemetry_datas")
	if createdPGTable {
		if err := pg.Exec(`CREATE TABLE telemetry_datas (device_id varchar(36) NOT NULL, key varchar(255) NOT NULL, ts bigint NOT NULL, bool_v boolean, number_v double precision, string_v text, tenant_id varchar(36), UNIQUE (device_id, key, ts))`).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pg.Exec("DROP TABLE IF EXISTS telemetry_datas").Error })
	}
	previousDB := global.DB
	previousQuery := *query.Q
	t.Cleanup(func() {
		global.DB = previousDB
		*query.Q = previousQuery
	})

	deviceID := uuid.NewString()
	tenantID := "telemetry-batch-test"
	ts := time.Now().UnixMilli()
	value := func(n float64) *float64 { return &n }

	writeAndRead := func(db *gorm.DB) []string {
		global.DB = db
		query.SetDefault(db)
		first := []*model.TelemetryData{
			{DeviceID: deviceID, Key: "temp", T: ts, NumberV: value(1), TenantID: &tenantID},
			{DeviceID: deviceID, Key: "hum", T: ts, NumberV: value(2), TenantID: &tenantID},
		}
		if err := CreateTelemetrDataBatch(first); err != nil {
			t.Fatalf("first batch on %s: %v", db.Dialector.Name(), err)
		}
		second := []*model.TelemetryData{
			{DeviceID: deviceID, Key: "temp", T: ts, NumberV: value(10), TenantID: &tenantID},
			{DeviceID: deviceID, Key: "co2", T: ts, NumberV: value(3), TenantID: &tenantID},
		}
		if err := CreateTelemetrDataBatch(second); err != nil {
			t.Fatalf("colliding batch on %s: %v", db.Dialector.Name(), err)
		}
		var rows []struct {
			Key     string  `gorm:"column:key"`
			NumberV float64 `gorm:"column:number_v"`
		}
		keyColumn := "key"
		if db.Dialector.Name() == "mysql" {
			keyColumn = "`key`"
		}
		if err := db.Table("telemetry_datas").Select(keyColumn+", number_v").Where("device_id = ?", deviceID).Find(&rows).Error; err != nil {
			t.Fatalf("read rows on %s: %v", db.Dialector.Name(), err)
		}
		got := make([]string, 0, len(rows))
		for _, row := range rows {
			got = append(got, fmt.Sprintf("%s=%.0f", row.Key, row.NumberV))
		}
		sort.Strings(got)
		return got
	}

	pgRows := writeAndRead(pg)
	myRows := writeAndRead(my)
	want := []string{"co2=3", "hum=2", "temp=10"}
	if fmt.Sprint(pgRows) != fmt.Sprint(want) || fmt.Sprint(myRows) != fmt.Sprint(want) {
		t.Fatalf("batch conflict result mismatch: pg=%v mysql=%v want=%v", pgRows, myRows, want)
	}
}
