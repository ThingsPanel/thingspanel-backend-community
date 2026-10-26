package uplink

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/brokerstate"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
)

func TestStatusUplinkBoundDeviceIgnoresLegacyExpiryAndUsesSQLActivity(t *testing.T) {
	db, closeDB := statusActivityDB(t)
	defer closeDB()
	rdb, stopRedis := statusActivityRedis(t)
	defer stopRedis()

	prevDB, prevRedis, prevStatusRedis, prevSSE := global.DB, global.REDIS, global.STATUS_REDIS, global.TPSSEManager
	prevDevice, prevDeviceConfig := query.Device, query.DeviceConfig
	prevQuery := *query.Q
	query.SetDefault(db)
	global.DB, global.REDIS, global.STATUS_REDIS = db, rdb, rdb
	global.TPSSEManager = global.NewSSEManager()
	t.Cleanup(func() {
		global.DB, global.REDIS, global.STATUS_REDIS = prevDB, prevRedis, prevStatusRedis
		global.TPSSEManager = prevSSE
		*query.Q = prevQuery
		query.Device, query.DeviceConfig = prevDevice, prevDeviceConfig
	})

	const tenant, device, cell = "activity-uplink-tenant", "activity-uplink-device", "activity-uplink-cell"
	configID := "activity-uplink-config"
	other := `{"heartbeat":30,"online_timeout":30}`
	if err := db.Create(&model.DeviceConfig{ID: configID, Name: "activity", DeviceType: "1", TenantID: tenant, OtherConfig: &other, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Device{ID: device, TenantID: tenant, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "number-activity", Voucher: `{}`, IsOnline: 1, DeviceConfigID: &configID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES(?,?,?,1,'active')`, tenant, device, cell).Error; err != nil {
		t.Fatal(err)
	}
	receipt := uuid.NewString()
	if err := db.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state,processed_at) VALUES(?,?,?,'telemetry_sample',?,1,decode('01','hex'),'{}',decode(repeat('00',32),'hex'),'processed',now())`, receipt, tenant, device, cell).Error; err != nil {
		t.Fatal(err)
	}
	observed := time.Now().UTC()
	if err := db.Exec(`INSERT INTO broker_telemetry_activity(tenant_id,device_id,transport_device_id,broker_cell,route_epoch,receipt_id,observed_at,next_check_at) VALUES(?,?,?, ?,1,?,?,now()+interval '30 seconds')`, tenant, device, device, cell, receipt, observed).Error; err != nil {
		t.Fatal(err)
	}

	log := logrus.New()
	log.SetOutput(io.Discard)
	status := NewStatusUplink(StatusUplinkConfig{Logger: log, BrokerStateStore: brokerstate.New(db), InputProfile: "ingress_v1"})

	// The old Redis-owner notifications still arrive during migration. For a
	// route-bound device they must only trigger a SQL recheck; fresh SQL activity
	// keeps the existing online state and creates no transition intent.
	for _, source := range []string{"heartbeat_expired", "timeout_expired"} {
		status.processMessage(&DeviceMessage{Type: MessageTypeStatus, DeviceID: device, Payload: []byte("0"), Metadata: map[string]interface{}{"source": source}})
		var online int16
		if err := db.Raw(`SELECT is_online FROM devices WHERE tenant_id=? AND id=?`, tenant, device).Scan(&online).Error; err != nil || online != 1 {
			t.Fatalf("legacy %s notification overrode fresh SQL activity: online=%d err=%v", source, online, err)
		}
	}
	if got := statusActivityCount(t, db, "broker_ingress_receipts", `tenant_id=? AND device_id=? AND stream='state_transition'`, tenant, device); got != 0 {
		t.Fatalf("fresh expiry hint created %d state transitions", got)
	}

	// Once SQL activity is past its deadline, the durable current-projection
	// queue—not the legacy expiry payload—determines offline state.
	if err := db.Exec(`UPDATE broker_telemetry_activity SET observed_at=now()-interval '40 seconds',next_check_at=now()-interval '1 second' WHERE tenant_id=? AND device_id=?`, tenant, device).Error; err != nil {
		t.Fatal(err)
	}
	status.retryBrokerProjections()
	var online int16
	if err := db.Raw(`SELECT is_online FROM devices WHERE tenant_id=? AND id=?`, tenant, device).Scan(&online).Error; err != nil || online != 0 {
		t.Fatalf("expired SQL activity was not projected offline: online=%d err=%v", online, err)
	}
	if got := statusActivityCount(t, db, "device_status_history", `tenant_id=? AND device_id=?`, tenant, device); got != 1 {
		t.Fatalf("SQL expiry wrote %d history records, want one", got)
	}
	if got := statusActivityCount(t, db, "broker_ingress_receipts", `tenant_id=? AND device_id=? AND stream='state_transition'`, tenant, device); got != 1 {
		t.Fatalf("SQL expiry wrote %d transition receipts, want one", got)
	}
	if got := statusActivityCount(t, db, "broker_ingress_effects", `tenant_id=? AND device_id=? AND effect_kind='automation'`, tenant, device); got != 1 {
		t.Fatalf("SQL expiry wrote %d state intents, want one", got)
	}
	if got := statusActivityCount(t, db, "broker_automation_actions", `tenant_id=? AND target_device_id=?`, tenant, device); got != 0 {
		t.Fatalf("legacy status automation created %d broker action rows", got)
	}
	time.Sleep(50 * time.Millisecond) // allow afterStatusChange's Redis/SSE notifications to drain before fixture cleanup
}

func statusActivityCount(t *testing.T, db *gorm.DB, table, where string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := db.Table(table).Where(where, args...).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func statusActivityDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "status_activity_" + hex.EncodeToString(random)
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	isolatedDSN := dsn + " search_path=" + schema
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		// The QA runner supplies a keyword DSN. Keep URL DSNs safe as well.
		isolatedDSN = strings.TrimRight(dsn, "&") + "&options=-csearch_path%3D" + schema
	}
	db, err := gorm.Open(postgres.Open(isolatedDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	dbSQL, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_ = dbSQL.Close()
		_ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		_ = adminSQL.Close()
	}
	if err := db.AutoMigrate(&model.Device{}, &model.DeviceConfig{}, &model.DeviceStatusHistory{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE broker_transport_bindings(tenant_id text,device_id text,broker_cell text,route_epoch bigint,phase text,PRIMARY KEY(tenant_id,device_id)); CREATE TABLE broker_state_projection_queue(tenant_id text,device_id text,updated_at timestamptz DEFAULT now(),PRIMARY KEY(tenant_id,device_id));`).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"002_ledger.sql", "005_ingress_effects.sql", "006_commands.sql", "009_automation_actions.sql", "010_telemetry_activity.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(strings.ReplaceAll(string(body), "public.", "")).Error; err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return db, cleanup
}

func statusActivityRedis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	server, err := exec.LookPath("redis-server")
	if err != nil {
		server = "/opt/homebrew/bin/redis-server"
		if _, statErr := os.Stat(server); statErr != nil {
			t.Skip("redis-server unavailable")
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cmd := exec.Command(server, "--port", strconv.Itoa(port), "--bind", "127.0.0.1", "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start isolated redis: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), MaxRetries: 0})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if client.Ping(context.Background()).Err() == nil {
			return client, func() {
				_ = client.Close()
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	_ = client.Close()
	t.Fatal("isolated redis did not become ready")
	return nil, nil
}
