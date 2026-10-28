package brokercontrol

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	bc "project/internal/brokercontract"
)

func TestPostgresRuntimeSyncTokenScopeDuplicateAckAndBootFence(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_BROKER_DATABASE_DSN"))
	if dsn == "" {
		b, e := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
		if e != nil {
			t.Skip("isolated broker QA PostgreSQL DSN unavailable")
		}
		dsn = strings.TrimSpace(string(b))
	}
	if dsn == "" {
		t.Skip("isolated broker QA PostgreSQL DSN empty")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal("open isolated QA database")
	}
	adminSQL, e := admin.DB()
	if e != nil {
		t.Fatal("get QA pool")
	}
	adminSQL.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = adminSQL.Close() })
	var database string
	if e := admin.Raw("SELECT current_database()").Scan(&database).Error; e != nil {
		t.Fatal("inspect QA database")
	}
	if database != "brokerqa" {
		t.Fatal("refusing runtime test outside brokerqa")
	}
	var random [8]byte
	if _, e := rand.Read(random[:]); e != nil {
		t.Fatal(e)
	}
	schema := "brokerqa_runtime_" + hex.EncodeToString(random[:])
	if e := admin.Exec("CREATE SCHEMA \"" + schema + "\"").Error; e != nil {
		t.Fatal("create private schema")
	}
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA \"" + schema + "\" CASCADE").Error })
	isolatedDSN, e := runtimeDSN(dsn, schema)
	if e != nil {
		t.Fatal("configure schema search path")
	}
	db, e := gorm.Open(postgres.Open(isolatedDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal("open private schema")
	}
	sqlDB, e := db.DB()
	if e != nil {
		t.Fatal("get isolated pool")
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	paths, e := filepath.Glob(filepath.Join("..", "..", "sql", "broker", "[0-9][0-9][0-9]_*.sql"))
	if e != nil {
		t.Fatal(e)
	}
	sort.Strings(paths)
	if len(paths) < 12 || !strings.HasSuffix(paths[11], "012_route_changes.sql") {
		t.Fatalf("broker migrations missing through 012: %d", len(paths))
	}
	for _, path := range paths {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal("read broker migration")
		}
		if e = db.Exec(strings.ReplaceAll(string(raw), "public.", `"`+schema+`".`)).Error; e != nil {
			t.Fatalf("apply %s: %v", filepath.Base(path), e)
		}
	}
	ctx := context.Background()
	store := NewMigrationStore(db)
	const tenant, device, cell, instance, boot = "tenant-runtime", "device-runtime", "cell-runtime", "broker-1", "boot-1"
	if e := db.Create(&TransportBinding{TenantID: tenant, DeviceID: device, BrokerCell: cell, RouteEpoch: 7, Phase: "active"}).Error; e != nil {
		t.Fatal("seed binding")
	}
	for _, role := range []string{"broker", "adapter", "dispatcher", "effects"} {
		if _, e := store.RegisterMember(ctx, cell, role, role+"-1", boot); e != nil {
			t.Fatalf("register %s: %v", role, e)
		}
	}
	_, membersSHA, e := store.Members(ctx, cell)
	if e != nil {
		t.Fatal("read roster")
	}
	changeID := uuid.NewString()
	_, e = store.Freeze(ctx, FreezeRouteRequest{ChangeID: changeID, TenantID: tenant, DeviceID: device, ExpectedCell: cell, TargetCell: "cell-target", Actor: "qa", ExpectedEpoch: 7, MembersSHA256: membersSHA, PlanSHA256: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatalf("freeze: %v", e)
	}
	token := strings.Repeat("runtime-token-", 4)
	owner, e := NewOwner(&fakeRepository{}, Config{Tokens: []TokenBinding{{Token: token, BrokerCell: cell, BrokerInstance: instance}}})
	if e != nil {
		t.Fatal("configure owner token")
	}
	handler := owner.RuntimeHandler(store)
	ack := bc.RuntimeFenceAck{RuntimeFence: bc.RuntimeFence{ChangeID: changeID, TenantID: tenant, DeviceID: device, DeniedEpoch: 7, NeedsAck: true}, EvidenceSHA256: strings.Repeat("b", 64)}
	request := bc.RuntimeSyncRequest{BrokerCell: cell, BrokerInstance: instance, BootID: boot, Applied: []bc.RuntimeFenceAck{ack}}
	post := func(tok string, req bc.RuntimeSyncRequest) (int, bc.RuntimeSyncResponse) {
		raw, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/v1/runtime/sync", bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+tok)
		handler.ServeHTTP(w, r)
		var response bc.RuntimeSyncResponse
		_ = json.Unmarshal(w.Body.Bytes(), &response)
		return w.Code, response
	}
	if code, _ := post(strings.Repeat("x", len(token)), request); code != http.StatusUnauthorized {
		t.Fatalf("wrong token status=%d", code)
	}
	wrong := request
	wrong.BrokerCell = "other-cell"
	if code, _ := post(token, wrong); code != http.StatusUnauthorized {
		t.Fatalf("wrong cell status=%d", code)
	}
	wrong = request
	wrong.BrokerInstance = "other-instance"
	if code, _ := post(token, wrong); code != http.StatusUnauthorized {
		t.Fatalf("wrong instance status=%d", code)
	}
	var brokerGeneration string
	if e := db.Raw("SELECT generation FROM broker_route_members WHERE broker_cell=? AND role='broker' AND instance_id='broker-1'", cell).Scan(&brokerGeneration).Error; e != nil {
		t.Fatal(e)
	}
	if code, response := post(token, request); code != http.StatusOK || len(response.Fences) != 1 {
		t.Fatalf("valid runtime sync code=%d response=%+v", code, response)
	}
	var acked int64
	if e := db.Table("broker_route_barrier_members").Where("change_id=?::uuid AND role='broker' AND instance_id='broker-1' AND acknowledged_at IS NOT NULL", changeID).Count(&acked).Error; e != nil || acked != 1 {
		t.Fatalf("broker ACK count=%d err=%v", acked, e)
	}
	var eventsBefore int64
	if e := db.Table("broker_route_change_events").Where("change_id=?::uuid AND stage='fence_ack'", changeID).Count(&eventsBefore).Error; e != nil {
		t.Fatal(e)
	}
	if code, _ := post(token, request); code != http.StatusOK {
		t.Fatalf("duplicate runtime sync code=%d", code)
	}
	var eventsAfter int64
	if e := db.Table("broker_route_change_events").Where("change_id=?::uuid AND stage='fence_ack'", changeID).Count(&eventsAfter).Error; e != nil {
		t.Fatal(e)
	}
	if eventsAfter != eventsBefore {
		t.Fatalf("duplicate ACK grew audit events: %d -> %d", eventsBefore, eventsAfter)
	}
	// A real replacement boot gets a new generation. Even if it repeats the
	// old ACK payload, the frozen manifest cannot inherit the old boot proof.
	request.BootID = "boot-2"
	if code, _ := post(token, request); code != http.StatusOK {
		t.Fatalf("replacement boot sync code=%d", code)
	}
	var newGeneration string
	if e := db.Raw("SELECT generation FROM broker_route_members WHERE broker_cell=? AND role='broker' AND instance_id='broker-1'", cell).Scan(&newGeneration).Error; e != nil {
		t.Fatal(e)
	}
	if newGeneration == brokerGeneration {
		t.Fatal("replacement boot inherited generation")
	}
	if e := store.PrepareTarget(ctx, tenant, device, changeID, strings.Repeat("c", 64)); !errors.Is(e, ErrBarrierIncomplete) {
		t.Fatalf("PrepareTarget accepted replaced broker boot: %v", e)
	}
}

func runtimeDSN(dsn, schema string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		if e != nil {
			return "", e
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return dsn + " search_path=" + schema, nil
}
