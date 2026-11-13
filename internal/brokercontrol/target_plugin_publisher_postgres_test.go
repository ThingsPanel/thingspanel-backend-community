package brokercontrol

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	bc "project/internal/brokercontract"
	"testing"
	"time"
)

type postgresPublisherRepository struct {
	targetPluginRepository
	bindingRepo *GormRepository
}

func (r postgresPublisherRepository) ResolveActiveBinding(ctx context.Context, t, d, c string) (TransportBinding, error) {
	return r.bindingRepo.ResolveActiveBinding(ctx, t, d, c)
}
func (r postgresPublisherRepository) ResolveAdmissionBinding(ctx context.Context, t, d, c string) (TransportBinding, error) {
	return r.bindingRepo.ResolveAdmissionBinding(ctx, t, d, c)
}

func TestPostgresPluginPublisherAuthenticatesWhileDispatchHoldsRoute(t *testing.T) {
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_BROKER_DATABASE_DSN is not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated postgres")
	}
	schema := fmt.Sprintf("publisher_lock_%d", time.Now().UnixNano())
	if err = admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		if pool, e := admin.DB(); e == nil {
			pool.Close()
		}
	})
	testDSN, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pool, e := db.DB(); e == nil {
			pool.Close()
		}
	})
	if err = db.AutoMigrate(&TransportBinding{}); err != nil {
		t.Fatal(err)
	}
	binding := TransportBinding{TenantID: "tenant-a", DeviceID: "target", BrokerCell: "community", RouteEpoch: 1, Phase: "active"}
	if err = db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	protocol := "HTTP"
	record := DeviceRecord{ID: "target", TenantID: "tenant-a", DeviceNumber: "number", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active", ProtocolType: &protocol}
	repo := postgresPublisherRepository{targetPluginRepository: targetPluginRepository{fakeRepository: &fakeRepository{serviceTargets: map[string]DeviceRecord{"target": record}}, plugins: []bc.PluginTarget{{TenantID: "tenant-a", DeviceID: "target", DeviceNumber: "number", Kind: bc.PrincipalDevice, TopicPrefix: "plugin/HTTP/"}}}, bindingRepo: &GormRepository{DB: db}}
	service := serviceFixture()
	service.TargetScopedPublisher = true
	service.TargetTenantScope = "platform"
	service.AllowedDeviceIDs = nil
	service.AllowedTenantIDs = nil
	service.Policy.Publish = []string{"devices/command/+/+"}
	owner := testOwner(t, repo, []ServiceCredential{service})
	req := serviceRequest()
	req.WireClientID, _ = bc.TargetPublisherClientID(service.AllowedClientIDs[0], "target")
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err = tx.Exec("SELECT 1 FROM broker_transport_bindings WHERE tenant_id=? AND device_id=? FOR UPDATE", "tenant-a", "target").Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	grant, err := owner.Authenticate(ctx, req)
	if err != nil || !grant.Allow {
		t.Fatalf("target publisher must authenticate without acquiring dispatch route lock: allow=%v error=%v", grant.Allow, err)
	}
	if !hasExact(grant.Policy.Publish, "plugin/HTTP/devices/command/number/+") {
		t.Fatal("exact plugin alias missing")
	}
	if err = tx.Rollback().Error; err != nil && !errors.Is(err, gorm.ErrInvalidTransaction) {
		t.Fatal(err)
	}
	// A new authentication must not admit absent, draining, connecting, or
	// cross-cell routes. An epoch advance is observed rather than cached.
	for _, phase := range []string{"draining", "connecting"} {
		if err = db.Model(&TransportBinding{}).Where("tenant_id=? AND device_id=?", "tenant-a", "target").Update("phase", phase).Error; err != nil {
			t.Fatal(err)
		}
		g, e := owner.Authenticate(context.Background(), req)
		if e != nil || g.Allow {
			t.Fatalf("%s target grant allow=%v err=%v", phase, g.Allow, e)
		}
	}
	if err = db.Model(&TransportBinding{}).Where("tenant_id=? AND device_id=?", "tenant-a", "target").Updates(map[string]any{"phase": "active", "broker_cell": "other-cell", "route_epoch": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if g, e := owner.Authenticate(context.Background(), req); e != nil || g.Allow {
		t.Fatalf("cross-cell grant allow=%v err=%v", g.Allow, e)
	}
	if err = db.Model(&TransportBinding{}).Where("tenant_id=? AND device_id=?", "tenant-a", "target").Update("broker_cell", "community").Error; err != nil {
		t.Fatal(err)
	}
	observed, e := repo.ReadActiveBinding(context.Background(), "tenant-a", "target", "community")
	if e != nil || observed.RouteEpoch != 2 {
		t.Fatalf("stale route snapshot: %+v %v", observed, e)
	}
	if err = db.Where("tenant_id=? AND device_id=?", "tenant-a", "target").Delete(&TransportBinding{}).Error; err != nil {
		t.Fatal(err)
	}
	if g, e := owner.Authenticate(context.Background(), req); e != nil || g.Allow {
		t.Fatalf("missing route grant allow=%v err=%v", g.Allow, e)
	}
	var count int64
	if err = db.Model(&TransportBinding{}).Where("tenant_id=? AND device_id=?", "tenant-a", "target").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("publisher created missing route count=%d err=%v", count, err)
	}
}

func (r postgresPublisherRepository) ReadActiveBinding(ctx context.Context, t, d, c string) (TransportBinding, error) {
	return r.bindingRepo.ReadActiveBinding(ctx, t, d, c)
}
