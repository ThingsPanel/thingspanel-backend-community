package tdenginehistory

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/internal/storage"
	global "project/pkg/global"
)

type fixture struct {
	pg     dal.HistoryBackend
	td     *Backend
	sink   *storage.TDengineHistorySink
	device string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn, base := os.Getenv("TP_TELEMETRY_BROKER_TEST_DSN"), os.Getenv("TP_TDENGINE_TEST_URL")
	if dsn == "" || base == "" {
		t.Skip("TP_TELEMETRY_BROKER_TEST_DSN and TP_TDENGINE_TEST_URL required")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	schema := "tdhist_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE telemetry_datas (device_id varchar(36) NOT NULL, "key" varchar(255) NOT NULL, ts bigint NOT NULL,
		bool_v boolean, number_v double precision, string_v text, tenant_id varchar(36), PRIMARY KEY (device_id, "key", ts))`).Error; err != nil {
		t.Fatal(err)
	}
	query.SetDefault(db)
	previousDB := global.DB
	global.DB = db
	user, pass := os.Getenv("TP_TDENGINE_TEST_USER"), os.Getenv("TP_TDENGINE_TEST_PASSWORD")
	if user == "" {
		user, pass = "root", "taosdata"
	}
	sink := &storage.TDengineHistorySink{BaseURL: base, User: user, Password: pass, Database: "hist_" + strings.ReplaceAll(uuid.NewString(), "-", "")}
	if err := sink.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	adminPool, _ := admin.DB()
	t.Cleanup(func() {
		global.DB = previousDB
		_, _ = sink.Query(context.Background(), "DROP DATABASE IF EXISTS "+sink.Database)
		_ = pool.Close()
		_ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		_ = adminPool.Close()
	})
	return &fixture{pg: dal.History, td: &Backend{Sink: sink}, sink: sink, device: uuid.NewString()}
}

// seed writes the same points to both stores.
func (f *fixture) seed(t *testing.T, key string, base int64, n int) {
	t.Helper()
	var intents []storage.HistoryIntent
	for i := 0; i < n; i++ {
		v := float64(i) * 1.5
		ts := base + int64(i)*1000
		row := &model.TelemetryData{DeviceID: f.device, Key: key, T: ts, NumberV: &v}
		tenant := "tenant-td"
		row.TenantID = &tenant
		if i%4 == 3 {
			s := fmt.Sprintf("text-%d", i)
			row.NumberV, row.StringV = nil, &s
		}
		if err := query.TelemetryData.Create(row); err != nil {
			t.Fatal(err)
		}
		intents = append(intents, storage.HistoryIntent{ID: int64(i + 1), TenantID: tenant, DeviceID: f.device, Key: key, TS: ts, NumberV: row.NumberV, StringV: row.StringV})
	}
	if err := f.sink.Append(context.Background(), intents); err != nil {
		t.Fatal(err)
	}
}

func sortByTS(rows []*model.TelemetryData) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].T < rows[j].T })
}

func TestTDengineBackendMatchesPostgresReads(t *testing.T) {
	f := setup(t)
	base := time.Now().Add(-2 * time.Hour).UnixMilli()
	f.seed(t, "temp", base, 25)
	f.seed(t, "other", base, 5)
	start, end := base+3000, base+20000

	pgRange, err := f.pg.GetHistoryTelemetrData(f.device, "temp", start, end)
	if err != nil {
		t.Fatal(err)
	}
	tdRange, err := f.td.GetHistoryTelemetrData(f.device, "temp", start, end)
	if err != nil {
		t.Fatal(err)
	}
	sortByTS(pgRange)
	if len(pgRange) != 18 || !reflect.DeepEqual(pgRange, tdRange) {
		t.Fatalf("range differs: pg=%d td=%d", len(pgRange), len(tdRange))
	}

	page, size := 2, 5
	req := &model.GetTelemetryHistoryDataByPageReq{DeviceID: f.device, Key: "temp", StartTime: start, EndTime: end, Page: &page, PageSize: &size}
	pgTotal, pgPage, err := f.pg.GetHistoryTelemetrDataByPage(req)
	if err != nil {
		t.Fatal(err)
	}
	tdTotal, tdPage, err := f.td.GetHistoryTelemetrDataByPage(req)
	if err != nil {
		t.Fatal(err)
	}
	if pgTotal != tdTotal || pgTotal != 18 || len(pgPage) != 5 || !reflect.DeepEqual(pgPage, tdPage) {
		t.Fatalf("page differs: total pg=%d td=%d\npg=%v\ntd=%v", pgTotal, tdTotal, pgPage, tdPage)
	}
	req.Page, req.PageSize = nil, nil
	pgAll, _ := f.pg.GetHistoryTelemetrDataByExport(req, 0, 100)
	_, tdAll, _ := f.td.GetHistoryTelemetrDataByPage(req)
	if len(tdAll) != 18 || !reflect.DeepEqual(pgAll, tdAll) {
		t.Fatalf("unpaged list differs: pg=%d td=%d", len(pgAll), len(tdAll))
	}

	var pgExport, tdExport []*model.TelemetryData
	for offset := 0; ; offset += 7 {
		a, err := f.pg.GetHistoryTelemetrDataByExport(req, offset, 7)
		if err != nil {
			t.Fatal(err)
		}
		b, err := f.td.GetHistoryTelemetrDataByExport(req, offset, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(a) == 0 && len(b) == 0 {
			break
		}
		pgExport, tdExport = append(pgExport, a...), append(tdExport, b...)
	}
	if len(pgExport) != 18 || !reflect.DeepEqual(pgExport, tdExport) {
		t.Fatalf("export differs: pg=%d td=%d", len(pgExport), len(tdExport))
	}

	pgStat, err := f.pg.GetTelemetrStatisticData(f.device, "temp", start, end)
	if err != nil {
		t.Fatal(err)
	}
	tdStat, err := f.td.GetTelemetrStatisticData(f.device, "temp", start, end)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(pgStat, func(i, j int) bool { return pgStat[i]["x"].(int64) < pgStat[j]["x"].(int64) })
	if len(pgStat) != len(tdStat) || fmt.Sprint(pgStat) != fmt.Sprint(tdStat) {
		t.Fatalf("statistic data differs:\npg=%v\ntd=%v", pgStat, tdStat)
	}

	if n, err := f.td.GetTelemetryDataCountByTenantId("tenant-td"); err != nil || n != 30 {
		t.Fatalf("tenant count=%d err=%v want 30", n, err)
	}
	if n, err := f.td.GetTelemetryDataCountByTenantId("nobody"); err != nil || n != 0 {
		t.Fatalf("unknown tenant count=%d err=%v", n, err)
	}
	// A key that was never written reads as empty, like PostgreSQL.
	if rows, err := f.td.GetHistoryTelemetrData(f.device, "missing", start, end); err != nil || len(rows) != 0 {
		t.Fatalf("missing key rows=%d err=%v", len(rows), err)
	}
}

func TestTDengineBackendDeletes(t *testing.T) {
	f := setup(t)
	base := time.Now().Add(-2 * time.Hour).UnixMilli()
	f.seed(t, "temp", base, 10)
	f.seed(t, "keep", base, 10)
	if err := f.td.DeleteTelemetrData(f.device, "temp"); err != nil {
		t.Fatal(err)
	}
	if err := f.td.DeleteTelemetrData(f.device, "temp"); err != nil {
		t.Fatalf("deleting again must be a no-op: %v", err)
	}
	if rows, _ := f.td.GetHistoryTelemetrData(f.device, "temp", 0, base+1e9); len(rows) != 0 {
		t.Fatalf("deleted key still has %d rows", len(rows))
	}
	if rows, _ := f.td.GetHistoryTelemetrData(f.device, "keep", 0, base+1e9); len(rows) != 10 {
		t.Fatalf("other key lost rows: %d", len(rows))
	}
	if err := f.td.DeleteTelemetrDataByTime(base + 4000); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.td.GetHistoryTelemetrData(f.device, "keep", 0, base+1e9)
	if len(rows) != 5 || rows[0].T != base+5000 {
		t.Fatalf("retention kept %d rows, first=%v", len(rows), rows)
	}
}

func TestTDengineBackendUnsupportedOperationsFailLoudly(t *testing.T) {
	b := &Backend{Sink: &storage.TDengineHistorySink{Database: "x"}}
	if err := b.DeleteTelemetrDataByDeviceId("d", nil); err == nil {
		t.Fatal("device-scope deletion must stay unsupported")
	}
	if _, err := b.GetTelemetrStatisticaAgregationData("d", "k", 0, 1, 1000, "median"); err == nil {
		t.Fatal("an unknown aggregate function must be rejected before any request is sent")
	}
	if _, err := b.GetTelemetryStatisticDataByDeviceIds(nil, nil, "", nil, ""); err == nil {
		t.Fatal("multi-device statistics must stay unsupported")
	}
	zero := 0
	req := &model.GetTelemetryHistoryDataByPageReq{Page: &zero, PageSize: &zero}
	if _, _, err := b.GetHistoryTelemetrDataByPage(req); err == nil {
		t.Fatal("page 0 must be rejected before any request is sent")
	}
}

func TestTDengineAggregationMatchesPostgres(t *testing.T) {
	f := setup(t)
	base := (time.Now().Add(-3*time.Hour).UnixMilli() / 1000 / 600) * 600 * 1000 // start on a 10-minute boundary
	f.seed(t, "temp", base, 130)
	// Values the PostgreSQL query ignores: out-of-range magnitudes.
	for i, v := range []float64{1e16, -1e16, 1e15} {
		ts := base + int64(200000+i*1000)
		big := v
		tenant := "tenant-td"
		if err := query.TelemetryData.Create(&model.TelemetryData{DeviceID: f.device, Key: "temp", T: ts, NumberV: &big, TenantID: &tenant}); err != nil {
			t.Fatal(err)
		}
		if err := f.sink.Append(context.Background(), []storage.HistoryIntent{{ID: int64(900 + i), TenantID: tenant, DeviceID: f.device, Key: "temp", TS: ts, NumberV: &big}}); err != nil {
			t.Fatal(err)
		}
	}
	close := func(a, b interface{}) bool {
		x, y := a.(float64), b.(float64)
		return x == y || (x-y) < 1e-9*(1+abs(x)) && (y-x) < 1e-9*(1+abs(x))
	}
	for _, window := range []int64{30000, 60000, 300000, 600000, 500} {
		for _, function := range []string{"avg", "max", "min", "sum", "diff"} {
			name := fmt.Sprintf("%s/%dms", function, window)
			pg, err := f.pg.GetTelemetrStatisticaAgregationData(f.device, "temp", base, base+400000, window, function)
			if err != nil {
				t.Fatalf("%s: pg: %v", name, err)
			}
			td, err := f.td.GetTelemetrStatisticaAgregationData(f.device, "temp", base, base+400000, window, function)
			if err != nil {
				t.Fatalf("%s: td: %v", name, err)
			}
			if len(pg) == 0 || len(pg) != len(td) {
				t.Fatalf("%s: bucket count pg=%d td=%d", name, len(pg), len(td))
			}
			for i := range pg {
				if pg[i]["x"] != td[i]["x"] || pg[i]["x2"] != td[i]["x2"] || !close(pg[i]["y"], td[i]["y"]) {
					t.Fatalf("%s bucket %d differs:\npg=%v\ntd=%v", name, i, pg[i], td[i])
				}
			}
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
