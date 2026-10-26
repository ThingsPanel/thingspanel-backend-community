package brokerstate

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	bc "project/internal/brokercontract"
	"project/internal/model"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not configured")
	}
	root, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	schema := "broker_state_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if e = root.Exec("CREATE SCHEMA " + schema).Error; e != nil {
		t.Fatal(e)
	}
	db, e := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
		root.Exec("DROP SCHEMA " + schema + " CASCADE")
		r, _ := root.DB()
		r.Close()
	})
	if e = db.Exec(`CREATE TABLE broker_transport_bindings(tenant_id text,device_id text,broker_cell text,route_epoch bigint,phase text,PRIMARY KEY(tenant_id,device_id));INSERT INTO broker_transport_bindings VALUES('ta','da','c',1,'active')`).Error; e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.Device{}, &model.DeviceStatusHistory{}); e != nil {
		t.Fatal(e)
	}
	if e = db.Exec(`INSERT INTO devices(id,tenant_id,is_enabled,activate_flag,device_number,voucher,is_online) VALUES('da','ta','enabled','active','n-da','{}',0)`).Error; e != nil {
		t.Fatal(e)
	}
	sql, e := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "003_state.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Exec(string(sql)).Error; e != nil {
		t.Fatal(e)
	}
	sql, e = os.ReadFile(filepath.Join("..", "..", "sql", "broker", "007_state_projection.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Exec(strings.ReplaceAll(string(sql), "public.", "")).Error; e != nil {
		t.Fatal(e)
	}
	return New(db)
}
func observation(conn string, seq uint64, on bool) bc.StateObservation {
	return bc.StateObservation{ContractVersion: "1", MappingVersion: "standard-v1", PolicyVersion: "policy-1", Principal: bc.Principal{TenantID: "ta", DeviceID: "da", Kind: bc.PrincipalDevice}, BrokerCell: "c", BrokerInstance: "i", NodeBootID: "boot", ConnectionID: conn, RouteEpoch: 1, EventSeq: seq, Online: on, ObservedAt: time.Now().UTC()}
}
func TestLateOfflineCannotCloseNewConnection(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, o := range []bc.StateObservation{observation("old", 1, true), observation("new", 1, true), observation("old", 2, false)} {
		if _, e := s.Apply(ctx, o); e != nil {
			t.Fatal(e)
		}
	}
	d, e := s.Snapshot(ctx, "ta", "da")
	if e != nil || d.Status != "online" || d.Connections != 1 {
		t.Fatalf("%+v %v", d, e)
	}
	if d, e = s.Snapshot(ctx, "other", "da"); e != nil || d.Status != "unknown" {
		t.Fatal("tenant scope leak")
	}
}
func TestCloseTombstoneAndFencing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, e := s.Apply(ctx, observation("a", 2, false)); e != nil {
		t.Fatal(e)
	}
	d, e := s.Apply(ctx, observation("a", 1, true))
	if e != nil || d.Status != "offline" || d.Changed {
		t.Fatalf("resurrected %+v %v", d, e)
	}
	o := observation("b", 1, true)
	o.RouteEpoch = 2
	if _, e = s.Apply(ctx, o); e != ErrFenced {
		t.Fatalf("wrong epoch %v", e)
	}
}
func TestStaleOpenObservationIsUnknown(t *testing.T) {
	s := testStore(t)
	o := observation("a", 1, true)
	o.ObservedAt = time.Now().Add(-2 * time.Minute)
	d, e := s.Apply(context.Background(), o)
	if e != nil || d.Status != "unknown" || d.Fresh {
		t.Fatalf("stale promoted to offline %+v %v", d, e)
	}
}

func TestProjectRecoversCommittedObservationAndIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Apply(ctx, observation("a", 1, true)); err != nil {
		t.Fatal(err)
	}
	projected, err := s.Project(ctx, "ta", "da")
	if err != nil || !projected.Changed || projected.Status != "online" || projected.Device.IsOnline != 1 {
		t.Fatalf("first projection: %+v %v", projected, err)
	}
	var count int64
	if err := s.db.Model(&model.DeviceStatusHistory{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("history=%d err=%v", count, err)
	}
	var pending int64
	if err := s.db.Table("broker_state_projection_queue").Count(&pending).Error; err != nil || pending != 0 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	again, err := s.Project(ctx, "ta", "da")
	if err != nil || again.Changed {
		t.Fatalf("repeated projection: %+v %v", again, err)
	}
	if err := s.db.Model(&model.DeviceStatusHistory{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate history=%d err=%v", count, err)
	}
}

func TestProjectRecomputesNewestConnectionStateBeforeWriting(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, o := range []bc.StateObservation{observation("a", 1, true), observation("a", 2, false), observation("b", 1, true)} {
		if _, err := s.Apply(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	d, err := s.Project(ctx, "ta", "da")
	if err != nil || !d.Changed || d.Status != "online" || d.Device.IsOnline != 1 {
		t.Fatalf("projected stale offline over new online: %+v %v", d, err)
	}
}

func TestProjectFailureKeepsQueueAndUnknownDoesNotForgeOffline(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Apply(ctx, observation("a", 1, true)); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec(`CREATE FUNCTION fail_status_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test failure'; END $$; CREATE TRIGGER fail_status_history BEFORE INSERT ON device_status_history FOR EACH ROW EXECUTE FUNCTION fail_status_history()`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, "ta", "da"); err == nil {
		t.Fatal("expected projection transaction failure")
	}
	var pending int64
	if err := s.db.Table("broker_state_projection_queue").Count(&pending).Error; err != nil || pending != 1 {
		t.Fatalf("failed projection lost queue: %d %v", pending, err)
	}
	if err := s.db.Exec(`DROP TRIGGER fail_status_history ON device_status_history; DROP FUNCTION fail_status_history()`).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Project(ctx, "ta", "da")
	if err != nil || !recovered.Changed || recovered.Status != "online" {
		t.Fatalf("retry did not recover committed state: %+v %v", recovered, err)
	}
	stale := observation("a", 2, true)
	stale.ObservedAt = time.Now().Add(-2 * time.Minute)
	if _, err := s.Apply(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&model.Device{}).Where("id = ? AND tenant_id = ?", "da", "ta").Update("is_online", 1).Error; err != nil {
		t.Fatal(err)
	}
	d, err := s.Project(ctx, "ta", "da")
	if err != nil || d.Status != "unknown" || d.Changed {
		t.Fatalf("unknown projected as status: %+v %v", d, err)
	}
	var online int16
	if err := s.db.Raw(`SELECT is_online FROM devices WHERE tenant_id='ta' AND id='da'`).Scan(&online).Error; err != nil || online != 1 {
		t.Fatalf("unknown changed device status: %d %v", online, err)
	}
	if err := s.db.Table("broker_state_projection_queue").Count(&pending).Error; err != nil || pending != 0 {
		t.Fatalf("unknown did not clear key: %d %v", pending, err)
	}
}

func TestProjectFutureAndStaleReceivedOnlineRemainUnknown(t *testing.T) {
	for _, tc := range []struct {
		name  string
		where string
		value time.Time
	}{
		{name: "future-observation", where: "observed_at", value: time.Now().UTC().Add(10 * time.Second)},
		{name: "stale-receipt", where: "received_at", value: time.Now().UTC().Add(-61 * time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			o := observation("time-boundary", 1, true)
			if tc.where == "observed_at" {
				o.ObservedAt = tc.value
			}
			if _, err := s.Apply(ctx, o); err != nil {
				t.Fatal(err)
			}
			if tc.where == "received_at" {
				if err := s.db.Exec("UPDATE broker_connection_observations SET received_at=? WHERE tenant_id='ta' AND device_id='da' AND connection_id='time-boundary'", tc.value).Error; err != nil {
					t.Fatal(err)
				}
			}
			decision, err := s.Snapshot(ctx, "ta", "da")
			if err != nil || decision.Status != "unknown" || decision.Fresh {
				t.Fatalf("snapshot should be unknown: %+v %v", decision, err)
			}
			if err := s.db.Model(&model.Device{}).Where("tenant_id=? AND id=?", "ta", "da").Update("is_online", 1).Error; err != nil {
				t.Fatal(err)
			}
			projected, err := s.Project(ctx, "ta", "da")
			if err != nil || projected.Status != "unknown" || projected.Changed || projected.Device.IsOnline != 1 {
				t.Fatalf("unknown observation changed device status: %+v %v", projected, err)
			}
			var online int16
			if err := s.db.Raw("SELECT is_online FROM devices WHERE tenant_id='ta' AND id='da'").Scan(&online).Error; err != nil || online != 1 {
				t.Fatalf("device marked offline: is_online=%d err=%v", online, err)
			}
			var history int64
			if err := s.db.Model(&model.DeviceStatusHistory{}).Count(&history).Error; err != nil || history != 0 {
				t.Fatalf("unknown observation wrote history: count=%d err=%v", history, err)
			}
		})
	}
}

func TestPendingPageHasHardMaximum(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 300; i++ {
		if err := s.db.Exec(`INSERT INTO broker_state_projection_queue(tenant_id,device_id,updated_at) VALUES(?,?,now())`, "tenant", fmt.Sprintf("device-%03d", i)).Error; err != nil {
			t.Fatal(err)
		}
	}
	keys, err := s.Pending(context.Background(), 1000)
	if err != nil || len(keys) != 256 {
		t.Fatalf("pending page length=%d err=%v", len(keys), err)
	}
}

func TestDeferProjectionMovesFailureBehindNewerWork(t *testing.T) {
	s := testStore(t)
	if err := s.db.Exec(`INSERT INTO broker_state_projection_queue(tenant_id,device_id,updated_at) VALUES('ta','da',now()-interval '1 hour'),('ta','newer',now())`).Error; err != nil {
		t.Fatal(err)
	}
	keys, err := s.Pending(context.Background(), 1)
	if err != nil || len(keys) != 1 || keys[0].DeviceID != "da" {
		t.Fatalf("oldest pending key: %+v %v", keys, err)
	}
	if err := s.DeferProjection(context.Background(), "ta", "da"); err != nil {
		t.Fatal(err)
	}
	keys, err = s.Pending(context.Background(), 1)
	if err != nil || len(keys) != 1 || keys[0].DeviceID != "newer" {
		t.Fatalf("deferred key starved newer work: %+v %v", keys, err)
	}
}

func TestProjectEnforcesTenantAndRouteFence(t *testing.T) {
	s := testStore(t)
	if _, err := s.Apply(context.Background(), observation("a", 1, true)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(context.Background(), "other", "da"); err != ErrFenced {
		t.Fatalf("cross-tenant project err=%v", err)
	}
	if err := s.db.Exec(`UPDATE broker_transport_bindings SET route_epoch=2 WHERE tenant_id='ta' AND device_id='da'`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(context.Background(), "ta", "da"); err != nil {
		t.Fatalf("current empty epoch should be unknown and clear stale pending: %v", err)
	}
	var online int16
	if err := s.db.Raw(`SELECT is_online FROM devices WHERE tenant_id='ta' AND id='da'`).Scan(&online).Error; err != nil || online != 0 {
		t.Fatalf("old epoch changed device: %d %v", online, err)
	}
}

func TestSkipProjectionOnlyDeletesDirtyKey(t *testing.T) {
	s := testStore(t)
	if _, err := s.Apply(context.Background(), observation("a", 1, true)); err != nil {
		t.Fatal(err)
	}
	if err := s.SkipProjection(context.Background(), "ta", "da"); err != nil {
		t.Fatal(err)
	}
	var pending int64
	if err := s.db.Table("broker_state_projection_queue").Count(&pending).Error; err != nil || pending != 0 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	var online int16
	if err := s.db.Raw(`SELECT is_online FROM devices WHERE tenant_id='ta' AND id='da'`).Scan(&online).Error; err != nil || online != 0 {
		t.Fatalf("skip wrote device status: %d %v", online, err)
	}
}
