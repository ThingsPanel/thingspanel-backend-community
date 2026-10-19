package brokercontrol

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	bc "project/internal/brokercontract"
	"project/internal/brokermapper"
)

func TestPostgresAdmissionBindingAllowsConnectingButActiveBindingDoesNot(t *testing.T) {
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		b, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
		if err != nil {
			t.Skip("isolated PostgreSQL DSN unavailable")
		}
		dsn = strings.TrimSpace(string(b))
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated PostgreSQL")
	}
	schema := "owner_connecting_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec("CREATE SCHEMA \"" + schema + "\"").Error; err != nil {
		t.Fatal("create private schema")
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP SCHEMA \"" + schema + "\" CASCADE").Error
		if db, e := admin.DB(); e == nil {
			_ = db.Close()
		}
	})
	scoped := dsn + " search_path=" + schema
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		if e != nil {
			t.Fatal("parse isolated database URL")
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		scoped = u.String()
	}
	db, err := gorm.Open(postgres.Open(scoped), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open private schema connection")
	}
	if err := db.AutoMigrate(&TransportBinding{}); err != nil {
		t.Fatal("migrate binding schema")
	}
	repo := NewGormRepository(db)
	row := TransportBinding{TenantID: "tenant-a", DeviceID: "device-a", BrokerCell: "cell-a", RouteEpoch: 4, Phase: "connecting", UpdatedAt: time.Now().UTC()}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal("seed connecting binding")
	}
	got, err := repo.ResolveAdmissionBinding(context.Background(), row.TenantID, row.DeviceID, row.BrokerCell)
	if err != nil || got.Phase != "connecting" || got.RouteEpoch != 4 {
		t.Fatalf("admission binding=%+v err=%v", got, err)
	}
	if _, err := repo.ResolveActiveBinding(context.Background(), row.TenantID, row.DeviceID, row.BrokerCell); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("active resolver accepted connecting binding: %v", err)
	}
}

type connectingOwnerRepository struct{ binding TransportBinding }

func (r connectingOwnerRepository) FindCredentialMatches(context.Context, string) ([]DeviceRecord, error) {
	return []DeviceRecord{{ID: r.binding.DeviceID, TenantID: r.binding.TenantID, DeviceNumber: "num-a", IsEnabled: "enabled", ActivateFlag: "active", DeviceType: "1"}}, nil
}
func (connectingOwnerRepository) ResolveServiceTargets(context.Context, []string) ([]DeviceRecord, error) {
	return nil, nil
}
func (connectingOwnerRepository) ListEnabledChildren(context.Context, string, string) ([]DeviceRecord, error) {
	return nil, nil
}
func (connectingOwnerRepository) HasEnabledTopicMappings(context.Context, string, string) (bool, error) {
	return false, nil
}
func (connectingOwnerRepository) CompileMappingSnapshot(context.Context, string, string) (*brokermapper.Snapshot, error) {
	return brokermapper.Standard(), nil
}
func (connectingOwnerRepository) ClaimClientID(context.Context, string, string, string, string) (bool, error) {
	return true, nil
}
func (r connectingOwnerRepository) ResolveActiveBinding(context.Context, string, string, string) (TransportBinding, error) {
	return r.binding, nil
}
func (r connectingOwnerRepository) ResolveAdmissionBinding(context.Context, string, string, string) (TransportBinding, error) {
	return r.binding, nil
}

func TestOwnerGrantCarriesConnectingBindingPhase(t *testing.T) {
	binding := TransportBinding{TenantID: "tenant-a", DeviceID: "device-a", BrokerCell: "cell-a", RouteEpoch: 4, Phase: "connecting", UpdatedAt: time.Now().UTC()}
	owner, err := NewOwner(connectingOwnerRepository{binding: binding}, Config{Tokens: []TokenBinding{{Token: strings.Repeat("t", 40), BrokerCell: binding.BrokerCell, BrokerInstance: "node-a"}}})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := owner.Authenticate(context.Background(), bc.AuthRequest{RequestID: "r1", Provider: CommunityProvider, BrokerCell: binding.BrokerCell, BrokerInstance: "node-a", ConnectionNonce: "nonce-a", WireClientID: "client-a", Username: "device-user", Password: "device-pass", Protocol: 4})
	if err != nil || !grant.Allow || grant.BindingPhase != "connecting" || grant.RouteEpoch != binding.RouteEpoch {
		t.Fatalf("connecting owner grant=%+v err=%v", grant, err)
	}
}
