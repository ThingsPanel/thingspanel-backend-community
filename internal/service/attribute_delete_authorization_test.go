package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestAttributeDeleteScopesMutationToCurrentTenant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	old := global.DB
	global.DB = db
	t.Cleanup(func() { global.DB = old })
	for _, sql := range []string{
		"CREATE TABLE devices (id text primary key, tenant_id text)",
		"CREATE TABLE attribute_datas (id text primary key, device_id text)",
		"INSERT INTO devices VALUES ('own', 'tenant-a'), ('foreign', 'tenant-b')",
		"INSERT INTO attribute_datas VALUES ('a-own', 'own'), ('a-foreign', 'foreign'), ('a-orphan', 'missing')",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &AttributeData{}
	claims := &utils.UserClaims{ID: "user-a", TenantID: "tenant-a", Authority: "TENANT_ADMIN"}
	for _, id := range []string{"a-foreign", "a-orphan", "missing"} {
		if err := svc.DeleteAttributeDataAuthorized(context.Background(), id, claims); err == nil {
			t.Fatalf("deleted unauthorized %s", id)
		}
	}
	if err := svc.DeleteAttributeDataAuthorized(context.Background(), "a-own", nil); err == nil {
		t.Fatal("accepted absent claims")
	}
	var count int64
	if err := db.Table("attribute_datas").Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("denied mutation changed rows: %d %v", count, err)
	}
	if err := svc.DeleteAttributeDataAuthorized(context.Background(), "a-own", claims); err != nil {
		t.Fatal(err)
	}
	root := &utils.UserClaims{ID: "root", Authority: "SYS_ADMIN"}
	if err := svc.DeleteAttributeDataAuthorized(context.Background(), "a-foreign", root); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAttributeDataAuthorized(context.Background(), "a-orphan", root); err == nil {
		t.Fatal("deleted orphan without an owning device")
	}
	if err := db.Table("attribute_datas").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("unexpected remaining rows: %d %v", count, err)
	}
}
