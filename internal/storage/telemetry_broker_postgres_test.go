package storage

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// telemetryBrokerFixture uses an isolated PostgreSQL search_path. It deliberately
// waits for the checked-in activity migration rather than synthesizing that
// production table in the test, so schema and transaction behavior stay aligned.
type telemetryBrokerFixture struct {
	db      *gorm.DB
	admin   *gorm.DB
	adminDB *sql.DB
	dbSQL   *sql.DB
	schema  string
}

func newTelemetryBrokerFixture(t *testing.T) *telemetryBrokerFixture {
	t.Helper()
	dsn := os.Getenv("TP_TELEMETRY_BROKER_TEST_DSN")
	if dsn == "" {
		b, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
		if err != nil {
			t.Skip("PostgreSQL QA DSN unavailable")
		}
		dsn = strings.TrimSpace(string(b))
	}
	if dsn == "" {
		t.Skip("PostgreSQL QA DSN empty")
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open isolated QA PostgreSQL: %v", err)
	}
	adminDB, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	schema := "telemetry_broker_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := `"` + schema + `"`
	if err := admin.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		t.Fatalf("create private test schema: %v", err)
	}

	db, err := gorm.Open(postgres.Open(telemetryBrokerDSNWithSchema(dsn, schema)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		_ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE").Error
		_ = adminDB.Close()
		t.Fatalf("open private schema: %v", err)
	}
	dbSQL, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	dbSQL.SetMaxOpenConns(8)
	fixture := &telemetryBrokerFixture{db: db, admin: admin, adminDB: adminDB, dbSQL: dbSQL, schema: schema}
	t.Cleanup(func() {
		_ = dbSQL.Close()
		_ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE").Error
		_ = adminDB.Close()
	})

	// These mirror only the legacy Community storage tables touched by the writer.
	// Broker tables are always installed from their numbered SQL migrations.
	for _, ddl := range []string{
		`CREATE TABLE devices (id varchar(36) PRIMARY KEY, tenant_id varchar(36) NOT NULL, is_enabled varchar(36) NOT NULL DEFAULT 'enabled', parent_id varchar(36), device_number varchar(36) NOT NULL DEFAULT '', voucher varchar(500) NOT NULL DEFAULT '')`,
		`CREATE TABLE telemetry_datas (device_id varchar(36) NOT NULL, "key" varchar(255) NOT NULL, ts int8 NOT NULL, bool_v bool, number_v float8, string_v text, tenant_id varchar(36), PRIMARY KEY(device_id,"key",ts))`,
		`CREATE TABLE telemetry_current_datas (device_id varchar(36) NOT NULL, "key" varchar(255) NOT NULL, ts timestamptz NOT NULL, bool_v bool, number_v float8, string_v text, tenant_id varchar(36), PRIMARY KEY(device_id,"key"))`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create legacy storage fixture table: %v", err)
		}
	}

	paths, err := filepath.Glob(filepath.Join("..", "..", "sql", "broker", "[0-9][0-9][0-9]_*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("broker migrations unavailable")
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sqlText := strings.ReplaceAll(string(content), "public.", quoted+".")
		if err := db.Exec(sqlText).Error; err != nil {
			t.Fatalf("apply private migration %s: %v", filepath.Base(path), err)
		}
	}
	if !db.Migrator().HasTable("broker_telemetry_activity") {
		t.Fatalf("broker_telemetry_activity is absent; add the numbered telemetry activity migration before running this integration suite")
	}
	return fixture
}

func telemetryBrokerDSNWithSchema(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err == nil {
			q := u.Query()
			q.Set("search_path", schema)
			u.RawQuery = q.Encode()
			return u.String()
		}
	}
	return dsn + " search_path=" + schema
}

func (f *telemetryBrokerFixture) insertDevice(t *testing.T, tenant, id, parent string) {
	t.Helper()
	if err := f.db.Exec(`INSERT INTO devices(id,tenant_id,is_enabled,parent_id,device_number) VALUES (?,?, 'enabled', ?, ?)`, id, tenant, nullableTestParent(parent), id).Error; err != nil {
		t.Fatalf("insert fixture device: %v", err)
	}
}

func nullableTestParent(parent string) any {
	if parent == "" {
		return nil
	}
	return parent
}

func (f *telemetryBrokerFixture) insertBinding(t *testing.T, tenant, device, cell string, epoch uint64, phase string) {
	t.Helper()
	if err := f.db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES (?,?,?,?,?)`, tenant, device, cell, epoch, phase).Error; err != nil {
		t.Fatalf("insert route binding: %v", err)
	}
}

func telemetryBrokerWriter(f *telemetryBrokerFixture) *telemetryWriter {
	return newTelemetryWriter(f.db, telemetryTestLogger{}, Config{TelemetryBatchSize: 32}, newMetricsCollector())
}

type telemetryTestLogger struct{}

func (telemetryTestLogger) Debug(...interface{})          {}
func (telemetryTestLogger) Debugf(string, ...interface{}) {}
func (telemetryTestLogger) Info(...interface{})           {}
func (telemetryTestLogger) Infof(string, ...interface{})  {}
func (telemetryTestLogger) Error(...interface{})          {}
func (telemetryTestLogger) Errorf(string, ...interface{}) {}
func (telemetryTestLogger) Warn(...interface{})           {}
func (telemetryTestLogger) Warnf(string, ...interface{})  {}

func telemetryTrustedItem(tenant, device, transportDevice, receipt, cell string, epoch uint64, timestamp int64, key string, value interface{}) *telemetryBatchItem {
	return &telemetryBatchItem{
		deviceID: device, tenantID: tenant, timestamp: timestamp,
		points: []TelemetryDataPoint{{Key: key, Value: value}},
		route:  &TrustedTelemetryRoute{ReceiptID: receipt, TransportDeviceID: transportDevice, BrokerCell: cell, RouteEpoch: epoch, ObservedAt: time.UnixMilli(timestamp).UTC()},
	}
}

func TestTrustedTelemetryFinalBatchCommitsReceiptActivityAndEffectAtomically(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	const tenant, root, logical = "tenant-trusted", "gateway-trusted", "child-trusted"
	f.insertDevice(t, tenant, root, "")
	f.insertDevice(t, tenant, logical, root)
	f.insertBinding(t, tenant, root, "cell-1", 4, "active")
	w := telemetryBrokerWriter(f)
	timestamp := time.Now().Add(-time.Second).UnixMilli()
	item := telemetryTrustedItem(tenant, logical, root, uuid.NewString(), "cell-1", 4, timestamp, "temperature", 21.5)
	accepted, written, failed, err := w.commitTrustedTelemetry([]*telemetryBatchItem{item})
	if err != nil {
		t.Fatalf("commit trusted sample: %v", err)
	}
	if len(accepted) != 1 || written != 1 || failed != 0 {
		t.Fatalf("accepted=%d written=%d failed=%d", len(accepted), written, failed)
	}

	assertTelemetryCount(t, f.db, "telemetry_datas", `device_id=? AND tenant_id=? AND "key"=?`, logical, tenant, "temperature", 1)
	assertTelemetryCount(t, f.db, "telemetry_current_datas", `device_id=? AND tenant_id=? AND "key"=?`, logical, tenant, "temperature", 1)
	assertTelemetryCount(t, f.db, "broker_ingress_receipts", `id=? AND tenant_id=? AND device_id=? AND stream='telemetry_sample' AND broker_cell=? AND route_epoch=? AND state='processed'`, item.route.ReceiptID, tenant, root, "cell-1", 4, 1)
	assertTelemetryCount(t, f.db, "broker_ingress_effects", `receipt_id=? AND tenant_id=? AND device_id=? AND effect_kind='automation' AND payload->>'trigger_param_type'='TEL'`, item.route.ReceiptID, tenant, logical, 1)
	assertTelemetryCount(t, f.db, "broker_telemetry_activity", `tenant_id=? AND device_id=? AND transport_device_id=? AND broker_cell=? AND route_epoch=? AND receipt_id=?`, tenant, logical, root, "cell-1", 4, item.route.ReceiptID, 1)
	assertTelemetryCount(t, f.db, "broker_state_projection_queue", `tenant_id=? AND device_id=?`, tenant, logical, 1)
}

func TestTrustedTelemetryRejectsStaleBindingsTenantAndReparentedChild(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	tests := []struct {
		name          string
		tenant        string
		logical       string
		transport     string
		cell          string
		bindingCell   string
		bindingEpoch  uint64
		itemEpoch     uint64
		phase         string
		logicalParent string
	}{
		{name: "old epoch", tenant: "t-epoch", logical: "d-epoch", transport: "d-epoch", cell: "cell-a", bindingCell: "cell-a", bindingEpoch: 8, itemEpoch: 7, phase: "active"},
		{name: "connecting phase", tenant: "t-phase", logical: "d-phase", transport: "d-phase", cell: "cell-a", bindingCell: "cell-a", bindingEpoch: 1, itemEpoch: 1, phase: "connecting"},
		{name: "wrong tenant", tenant: "t-other", logical: "d-other", transport: "root-other", cell: "cell-a", bindingCell: "cell-a", bindingEpoch: 1, itemEpoch: 1, phase: "active"},
		{name: "child was reparented", tenant: "t-parent", logical: "child-parent", transport: "new-root", cell: "cell-a", bindingCell: "cell-a", bindingEpoch: 2, itemEpoch: 2, phase: "active", logicalParent: "old-root"},
	}
	w := telemetryBrokerWriter(f)
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "wrong tenant" {
				f.insertDevice(t, "t-actual", tc.transport, "")
				f.insertBinding(t, "t-actual", tc.transport, tc.bindingCell, tc.bindingEpoch, tc.phase)
			} else if tc.name == "child was reparented" {
				f.insertDevice(t, tc.tenant, tc.logicalParent, "")
				f.insertDevice(t, tc.tenant, tc.transport, "")
				f.insertDevice(t, tc.tenant, tc.logical, tc.logicalParent)
				f.insertBinding(t, tc.tenant, tc.transport, tc.bindingCell, tc.bindingEpoch, tc.phase)
			} else {
				f.insertDevice(t, tc.tenant, tc.transport, "")
				if tc.logical != tc.transport {
					f.insertDevice(t, tc.tenant, tc.logical, "")
				}
				f.insertBinding(t, tc.tenant, tc.transport, tc.bindingCell, tc.bindingEpoch, tc.phase)
			}
			timestamp := time.Now().Add(time.Duration(-i-1) * time.Second).UnixMilli()
			item := telemetryTrustedItem(tc.tenant, tc.logical, tc.transport, uuid.NewString(), tc.cell, tc.itemEpoch, timestamp, "voltage", float64(i+1))
			accepted, written, failed, err := w.commitTrustedTelemetry([]*telemetryBatchItem{item})
			if err != nil {
				t.Fatalf("rejected sample unexpectedly returned transaction error: %v", err)
			}
			if len(accepted) != 0 || written != 0 || failed != 1 {
				t.Fatalf("accepted=%d written=%d failed=%d", len(accepted), written, failed)
			}
			assertTelemetryCount(t, f.db, "telemetry_datas", `device_id=? AND tenant_id=?`, tc.logical, tc.tenant, 0)
			assertTelemetryCount(t, f.db, "broker_ingress_receipts", `id=?`, item.route.ReceiptID, 0)
			assertTelemetryCount(t, f.db, "broker_ingress_effects", `tenant_id=? AND device_id=?`, tc.tenant, tc.logical, 0)
			assertTelemetryCount(t, f.db, "broker_telemetry_activity", `tenant_id=? AND device_id=?`, tc.tenant, tc.logical, 0)
			assertTelemetryCount(t, f.db, "broker_state_projection_queue", `tenant_id=? AND device_id=?`, tc.tenant, tc.logical, 0)
		})
	}
}

func TestTrustedTelemetryTransactionFailureRollsBackEveryTable(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	const tenant, device = "tenant-rollback", "device-rollback"
	f.insertDevice(t, tenant, device, "")
	f.insertBinding(t, tenant, device, "cell-rollback", 3, "active")
	if err := f.db.Exec(`CREATE FUNCTION reject_test_telemetry_effect() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected effect failure'; END $$`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`CREATE TRIGGER reject_test_telemetry_effect BEFORE INSERT ON broker_ingress_effects FOR EACH ROW EXECUTE FUNCTION reject_test_telemetry_effect()`).Error; err != nil {
		t.Fatal(err)
	}
	item := telemetryTrustedItem(tenant, device, device, uuid.NewString(), "cell-rollback", 3, time.Now().Add(-time.Second).UnixMilli(), "pressure", 18.0)
	accepted, written, failed, err := telemetryBrokerWriter(f).commitTrustedTelemetry([]*telemetryBatchItem{item})
	if err == nil {
		t.Fatal("injected effect failure committed")
	}
	if len(accepted) != 0 || written != 0 || failed != 1 {
		t.Fatalf("accepted=%d written=%d failed=%d", len(accepted), written, failed)
	}
	assertTelemetryCount(t, f.db, "telemetry_datas", `device_id=? AND tenant_id=?`, device, tenant, 0)
	assertTelemetryCount(t, f.db, "telemetry_current_datas", `device_id=? AND tenant_id=?`, device, tenant, 0)
	assertTelemetryCount(t, f.db, "broker_ingress_receipts", `id=?`, item.route.ReceiptID, 0)
	assertTelemetryCount(t, f.db, "broker_ingress_effects", `receipt_id=?`, item.route.ReceiptID, 0)
	assertTelemetryCount(t, f.db, "broker_telemetry_activity", `tenant_id=? AND device_id=?`, tenant, device, 0)
	assertTelemetryCount(t, f.db, "broker_state_projection_queue", `tenant_id=? AND device_id=?`, tenant, device, 0)
}

func TestTrustedTelemetryDuplicateReceiptAndOldSampleDoNotDuplicateEffectOrRegressLatest(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	const tenant, device = "tenant-repeat", "device-repeat"
	f.insertDevice(t, tenant, device, "")
	f.insertBinding(t, tenant, device, "cell-repeat", 5, "active")
	w := telemetryBrokerWriter(f)
	newerTS := time.Now().Add(-time.Second).UnixMilli()
	newer := telemetryTrustedItem(tenant, device, device, uuid.NewString(), "cell-repeat", 5, newerTS, "temperature", 23.0)
	if _, _, _, err := w.commitTrustedTelemetry([]*telemetryBatchItem{newer}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := w.commitTrustedTelemetry([]*telemetryBatchItem{newer}); err != nil {
		t.Fatalf("same route receipt retry: %v", err)
	}
	olderTS := newerTS - 60_000
	older := telemetryTrustedItem(tenant, device, device, uuid.NewString(), "cell-repeat", 5, olderTS, "temperature", 17.0)
	if _, _, _, err := w.commitTrustedTelemetry([]*telemetryBatchItem{older}); err != nil {
		t.Fatal(err)
	}
	assertTelemetryCount(t, f.db, "telemetry_datas", `device_id=? AND tenant_id=? AND "key"='temperature'`, device, tenant, 2)
	assertTelemetryCount(t, f.db, "broker_ingress_receipts", `tenant_id=? AND stream='telemetry_sample'`, tenant, 2)
	assertTelemetryCount(t, f.db, "broker_ingress_effects", `tenant_id=? AND device_id=? AND effect_kind='automation'`, tenant, device, 2)
	var current struct {
		TS      time.Time
		NumberV float64
	}
	if err := f.db.Table("telemetry_current_datas").Select("ts,number_v").Where("device_id=? AND key='temperature'", device).Take(&current).Error; err != nil {
		t.Fatal(err)
	}
	if !current.TS.Equal(time.UnixMilli(newerTS).UTC()) || current.NumberV != 23.0 {
		t.Fatalf("latest regressed: ts=%s value=%f", current.TS, current.NumberV)
	}
	var activity struct {
		ReceiptID  string
		ObservedAt time.Time
	}
	if err := f.db.Table("broker_telemetry_activity").Select("receipt_id,observed_at").Where("tenant_id=? AND device_id=?", tenant, device).Take(&activity).Error; err != nil {
		t.Fatal(err)
	}
	if activity.ReceiptID != newer.route.ReceiptID || !activity.ObservedAt.Equal(time.UnixMilli(newerTS).UTC()) {
		t.Fatalf("activity regressed: %#v", activity)
	}
}

func TestTrustedTelemetryMetricsCountInsertedRowsAndBatchDuplicates(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	const tenant, device = "tenant-metrics", "device-metrics"
	f.insertDevice(t, tenant, device, "")
	f.insertBinding(t, tenant, device, "cell-metrics", 2, "active")
	metrics := newMetricsCollector()
	w := telemetryBrokerWriter(f)
	w.metrics = metrics
	timestamp := time.Now().Add(-time.Second).UnixMilli()
	first := telemetryTrustedItem(tenant, device, device, uuid.NewString(), "cell-metrics", 2, timestamp, "temperature", 20.0)
	duplicate := telemetryTrustedItem(tenant, device, device, uuid.NewString(), "cell-metrics", 2, timestamp, "temperature", 20.0)
	w.doFlushTrusted([]*telemetryBatchItem{first, duplicate})

	got := metrics.GetMetrics()
	if got.TelemetryWritten != 1 || got.TelemetryDuplicatesInBatch != 1 || got.TelemetryFailed != 0 {
		t.Fatalf("after same-batch duplicate: written=%d duplicates=%d failed=%d", got.TelemetryWritten, got.TelemetryDuplicatesInBatch, got.TelemetryFailed)
	}

	// A distinct trusted receipt can commit while its history point conflicts
	// with an existing (device,key,timestamp) row. That conflict is not a
	// successful history write and is not a duplicate inside this batch.
	historyConflict := telemetryTrustedItem(tenant, device, device, uuid.NewString(), "cell-metrics", 2, timestamp, "temperature", 20.0)
	w.doFlushTrusted([]*telemetryBatchItem{historyConflict})

	got = metrics.GetMetrics()
	if got.TelemetryWritten != 1 || got.TelemetryDuplicatesInBatch != 1 || got.TelemetryFailed != 0 {
		t.Fatalf("after existing-history conflict: written=%d duplicates=%d failed=%d", got.TelemetryWritten, got.TelemetryDuplicatesInBatch, got.TelemetryFailed)
	}
	assertTelemetryCount(t, f.db, "telemetry_datas", `device_id=? AND tenant_id=? AND "key"=? AND ts=?`, device, tenant, "temperature", timestamp, 1)
	assertTelemetryCount(t, f.db, "broker_ingress_receipts", `tenant_id=? AND stream='telemetry_sample'`, tenant, 3)
}

func TestTrustedAndLegacyItemsUseSeparateCommitPaths(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	const tenant = "tenant-mixed"
	f.insertDevice(t, tenant, "legacy-device", "")
	f.insertDevice(t, tenant, "trusted-device", "")
	f.insertBinding(t, tenant, "trusted-device", "cell-mixed", 1, "active")
	timestamp := time.Now().Add(-time.Second).UnixMilli()
	legacy := &telemetryBatchItem{deviceID: "legacy-device", tenantID: tenant, timestamp: timestamp, points: []TelemetryDataPoint{{Key: "legacy", Value: 1.0}}}
	trusted := telemetryTrustedItem(tenant, "trusted-device", "trusted-device", uuid.NewString(), "cell-mixed", 1, timestamp, "trusted", 2.0)
	telemetryBrokerWriter(f).doFlush([]*telemetryBatchItem{legacy, trusted})
	assertTelemetryCount(t, f.db, "telemetry_datas", `tenant_id=?`, tenant, 2)
	assertTelemetryCount(t, f.db, "broker_ingress_receipts", `tenant_id=? AND device_id='trusted-device' AND stream='telemetry_sample'`, tenant, 1)
	assertTelemetryCount(t, f.db, "broker_ingress_receipts", `tenant_id=? AND device_id='legacy-device'`, tenant, 0)
	assertTelemetryCount(t, f.db, "broker_ingress_effects", `tenant_id=? AND device_id='trusted-device'`, tenant, 1)
	assertTelemetryCount(t, f.db, "broker_ingress_effects", `tenant_id=? AND device_id='legacy-device'`, tenant, 0)
}

func TestTrustedTelemetryBindingFinalLockBlocksRouteMigration(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	const tenant, device = "tenant-lock", "device-lock"
	f.insertDevice(t, tenant, device, "")
	f.insertBinding(t, tenant, device, "cell-lock", 1, "active")
	locked := make(chan struct{})
	release := make(chan struct{})
	const callbackName = "test:telemetry-binding-lock-barrier"
	var once sync.Once
	if err := f.db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "broker_transport_bindings" {
			once.Do(func() { close(locked); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(callbackName)

	item := telemetryTrustedItem(tenant, device, device, uuid.NewString(), "cell-lock", 1, time.Now().Add(-time.Second).UnixMilli(), "humidity", 42.0)
	writerDone := make(chan error, 1)
	go func() {
		_, _, _, err := telemetryBrokerWriter(f).commitTrustedTelemetry([]*telemetryBatchItem{item})
		writerDone <- err
	}()
	select {
	case <-locked:
	case err := <-writerDone:
		t.Fatalf("writer ended before taking binding lock: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("trusted writer did not reach binding lock")
	}

	migrationDone := make(chan error, 1)
	go func() {
		migrationDone <- f.db.Exec("UPDATE broker_transport_bindings SET route_epoch=2 WHERE tenant_id=? AND device_id=?", tenant, device).Error
	}()
	select {
	case err := <-migrationDone:
		close(release)
		t.Fatalf("route migration was not blocked by trusted final binding lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("trusted commit failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("trusted commit did not finish after releasing barrier")
	}
	select {
	case err := <-migrationDone:
		if err != nil {
			t.Fatalf("route migration after trusted commit failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("migration remained blocked after trusted commit")
	}
	var epoch uint64
	if err := f.db.Table("broker_transport_bindings").Select("route_epoch").Where("tenant_id=? AND device_id=?", tenant, device).Scan(&epoch).Error; err != nil {
		t.Fatal(err)
	}
	if epoch != 2 {
		t.Fatalf("migration epoch=%d want 2", epoch)
	}
}

func assertTelemetryCount(t *testing.T, db *gorm.DB, table, where string, args ...interface{}) {
	t.Helper()
	want := args[len(args)-1].(int)
	queryArgs := args[:len(args)-1]
	var got int64
	if err := db.Table(table).Where(where, queryArgs...).Count(&got).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != int64(want) {
		t.Fatalf("%s(%s) rows=%d want=%d", table, where, got, want)
	}
}
