package brokercontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openSQLiteRepository(t *testing.T) (*gorm.DB, *GormRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:broker-owner-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&ClientIDBinding{}, &TransportBinding{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, e := db.DB()
		if e == nil {
			_ = sqlDB.Close()
		}
	})
	return db, &GormRepository{DB: db}
}

func TestGormPersistenceClientIDBindingAndPhase(t *testing.T) {
	_, repo := openSQLiteRepository(t)
	ctx := context.Background()
	owned, err := repo.ClaimClientID(ctx, "community", "wire-1", "tenant-1", "device-1")
	if err != nil || !owned {
		t.Fatalf("first client claim: %v %v", owned, err)
	}
	owned, err = repo.ClaimClientID(ctx, "community", "wire-1", "tenant-1", "device-1")
	if err != nil || !owned {
		t.Fatalf("same owner refresh: %v %v", owned, err)
	}
	owned, err = repo.ClaimClientID(ctx, "community", "wire-1", "tenant-2", "device-2")
	if err != nil || owned {
		t.Fatalf("client ID theft: %v %v", owned, err)
	}
	binding, err := repo.ResolveActiveBinding(ctx, "tenant-1", "device-1", "community")
	if err != nil || binding.RouteEpoch != 1 || binding.Phase != "active" {
		t.Fatalf("new default binding: %#v %v", binding, err)
	}
	_, err = repo.ResolveActiveBinding(ctx, "tenant-1", "device-1", "other-cell")
	if !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("cross-cell binding was not denied: %v", err)
	}
	if err := repo.DB.Model(&TransportBinding{}).Where("tenant_id = ? AND device_id = ?", "tenant-1", "device-1").Update("phase", "draining").Error; err != nil {
		t.Fatal(err)
	}
	_, err = repo.ResolveActiveBinding(ctx, "tenant-1", "device-1", "community")
	if !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("draining binding renewed: %v", err)
	}
}
