package service

import (
	"context"
	"errors"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/repo"
	"project/pkg/global"
)

// Real SQL and the project's real Casbin matcher, scoped to a private fixture.
// No execution grant, session, or installation table is created by this fixture.
func lookupPermissionFixture(t *testing.T) (*gorm.DB, *casbin.Enforcer) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	return installLookupPermissionFixture(t, db)
}

func installLookupPermissionFixture(t *testing.T, db *gorm.DB) (*gorm.DB, *casbin.Enforcer) {
	t.Helper()
	if err := db.Exec(`CREATE TABLE users(id text PRIMARY KEY,tenant_id text,status text,authority text)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO users(id,tenant_id,status,authority) VALUES('original-subject','original-tenant','N','TENANT_ADMIN')`).Error; err != nil {
		t.Fatal(err)
	}
	enforcer, err := casbin.NewEnforcer("../../configs/casbin.conf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.AddNamedGroupingPolicy("g2", MarketInstallationReadResource, "lookup-fixture-function"); err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.AddPolicy("lookup-fixture-role", "lookup-fixture-function", "allow"); err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.AddNamedGroupingPolicy("g", "original-subject", "lookup-fixture-role"); err != nil {
		t.Fatal(err)
	}
	previousDB, previousEnforcer := global.DB, global.CasbinEnforcer
	global.DB, global.CasbinEnforcer = db, enforcer
	t.Cleanup(func() { global.DB, global.CasbinEnforcer = previousDB, previousEnforcer })
	return db, enforcer
}

func exerciseLookupPermissionRevocation(t *testing.T, db *gorm.DB, enforcer *casbin.Enforcer) {
	t.Helper()
	ctx := context.Background()
	identity := model.MarketInstallationReadIdentity{ProtocolVersion: 1, TenantID: "original-tenant", SubjectID: "original-subject"}
	allowed := func() {
		t.Helper()
		decision, err := AuthorizeCurrentMarketInstallationManagement(ctx, identity)
		if err != nil || decision == nil || !decision.Authorized || decision.ProtocolVersion != 1 || decision.TenantID != identity.TenantID || decision.SubjectID != identity.SubjectID || decision.Resource != MarketInstallationReadResource {
			t.Fatalf("current authorization: %#v %v", decision, err)
		}
	}
	denied := func(candidate model.MarketInstallationReadIdentity) {
		t.Helper()
		if decision, err := AuthorizeCurrentMarketInstallationManagement(ctx, candidate); decision != nil || !errors.Is(err, repo.ErrMarketInstallUnauthorized) {
			t.Fatalf("revoked identity/resource allowed: %#v %v", decision, err)
		}
	}
	allowed()
	for _, candidate := range []model.MarketInstallationReadIdentity{
		{}, {ProtocolVersion: 2, TenantID: identity.TenantID, SubjectID: identity.SubjectID},
		{ProtocolVersion: 1, TenantID: "foreign", SubjectID: identity.SubjectID},
		{ProtocolVersion: 1, TenantID: identity.TenantID, SubjectID: "foreign"},
	} {
		denied(candidate)
	}
	for _, change := range []string{"status='F'", "tenant_id='foreign'", "tenant_id=NULL", "authority='TENANT_USER'", "authority=NULL"} {
		if err := db.Exec("UPDATE users SET " + change + " WHERE id='original-subject'").Error; err != nil {
			t.Fatal(err)
		}
		denied(identity)
		if err := db.Exec("UPDATE users SET status='N',tenant_id='original-tenant',authority='TENANT_ADMIN' WHERE id='original-subject'").Error; err != nil {
			t.Fatal(err)
		}
	}
	allowed()
	if _, err := enforcer.RemoveNamedGroupingPolicy("g", identity.SubjectID, "lookup-fixture-role"); err != nil {
		t.Fatal(err)
	}
	denied(identity)
	if _, err := enforcer.AddNamedGroupingPolicy("g", identity.SubjectID, "lookup-fixture-role"); err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.RemovePolicy("lookup-fixture-role", "lookup-fixture-function", "allow"); err != nil {
		t.Fatal(err)
	}
	denied(identity)
	if _, err := enforcer.AddPolicy("lookup-fixture-role", "lookup-fixture-function", "allow"); err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.RemoveNamedGroupingPolicy("g2", MarketInstallationReadResource, "lookup-fixture-function"); err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.AddNamedGroupingPolicy("g2", "api/v1/device/market/bundles/installation-operations", "lookup-fixture-function"); err != nil {
		t.Fatal(err)
	}
	denied(identity)
	if _, err := enforcer.AddNamedGroupingPolicy("g2", MarketInstallationReadResource, "lookup-fixture-function"); err != nil {
		t.Fatal(err)
	}
	allowed()
	global.CasbinEnforcer = nil
	if decision, err := AuthorizeCurrentMarketInstallationManagement(ctx, identity); err == nil || decision != nil {
		t.Fatal("unavailable enforcer allowed a read")
	}
	global.CasbinEnforcer = enforcer
	global.DB = nil
	if decision, err := AuthorizeCurrentMarketInstallationManagement(ctx, identity); err == nil || decision != nil {
		t.Fatal("unavailable database allowed a read")
	}
	global.DB = db
	if err := db.Exec("DROP TABLE users").Error; err != nil {
		t.Fatal(err)
	}
	if decision, err := AuthorizeCurrentMarketInstallationManagement(ctx, identity); err == nil || decision != nil || errors.Is(err, repo.ErrMarketInstallUnauthorized) {
		t.Fatal("database error became a confirmed permission denial or authorization")
	}
}

func TestMarketInstallationReadAuthorizationCurrentSQLAndCasbin(t *testing.T) {
	db, enforcer := lookupPermissionFixture(t)
	exerciseLookupPermissionRevocation(t, db, enforcer)
}
