package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
)

type templateCommitPool struct {
	*sql.DB
	commits atomic.Int32
}

func (p *templateCommitPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, e := p.DB.BeginTx(ctx, opts)
	if e != nil {
		return nil, e
	}
	return &templateCommitTx{Tx: tx, p: p}, nil
}

type templateCommitTx struct {
	*sql.Tx
	p *templateCommitPool
}

func (t *templateCommitTx) Commit() error {
	t.p.commits.Add(1)
	if e := t.Tx.Commit(); e != nil {
		return e
	}
	return errors.New("injected lost commit response")
}

func TestDeviceTemplateDeleteTenantPostgres(t *testing.T) {
	db := brokerEvidenceTestDB(t)
	if e := db.AutoMigrate(&model.DeviceTemplate{}); e != nil {
		t.Fatal(e)
	}
	if e := db.Exec(`ALTER TABLE device_configs ADD CONSTRAINT fixture_template_fk FOREIGN KEY (device_template_id) REFERENCES device_templates(id) ON DELETE RESTRICT`).Error; e != nil {
		t.Fatal(e)
	}
	old := global.DB
	global.DB = db
	t.Cleanup(func() { global.DB = old })
	private, public := int16(1), int16(2)
	rows := []model.DeviceTemplate{{ID: "a", TenantID: "tenant-a", Name: "a", Flag: &private}, {ID: "b", TenantID: "tenant-b", Name: "b", Flag: &private}, {ID: "public", TenantID: "tenant-a", Name: "public", Flag: &public}, {ID: "referenced", TenantID: "tenant-a", Name: "referenced", Flag: &private}, {ID: "unknown", TenantID: "tenant-a", Name: "unknown", Flag: &private}}
	if e := db.Create(&rows).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec(`UPDATE device_templates SET flag=NULL WHERE id='public'`).Error; e != nil {
		t.Fatal(e)
	}
	ref := "referenced"
	if e := db.Create(&model.DeviceConfig{ID: "cfg", TenantID: "tenant-a", DeviceTemplateID: &ref}).Error; e != nil {
		t.Fatal(e)
	}
	svc := &DeviceTemplate{}
	a := &utils.UserClaims{ID: "actor", TenantID: "tenant-a", Authority: "TENANT_ADMIN"}
	check := func(e error, code int) {
		t.Helper()
		var x *errcode.Error
		if !errors.As(e, &x) || x.Code != code {
			t.Fatalf("error=%v want=%d", e, code)
		}
	}
	check(svc.DeleteDeviceTemplate("a", nil), errcode.CodeNoPermission)
	check(svc.DeleteDeviceTemplate("a", &utils.UserClaims{ID: " ", TenantID: "tenant-a"}), errcode.CodeNoPermission)
	check(svc.DeleteDeviceTemplate("a", &utils.UserClaims{ID: "actor"}), errcode.CodeNoPermission)
	check(svc.DeleteDeviceTemplate("b", a), errcode.CodeNotFound)
	check(svc.DeleteDeviceTemplate("missing", a), errcode.CodeNotFound)
	check(svc.DeleteDeviceTemplate("public", a), errcode.CodeOpDenied)
	if e := db.Exec(`UPDATE device_templates SET flag=99 WHERE id='public'`).Error; e != nil {
		t.Fatal(e)
	}
	check(svc.DeleteDeviceTemplate("public", a), errcode.CodeOpDenied)
	if e := db.Exec(`UPDATE device_templates SET flag=2 WHERE id='public'`).Error; e != nil {
		t.Fatal(e)
	}
	check(svc.DeleteDeviceTemplate("public", &utils.UserClaims{ID: "actor", TenantID: "tenant-a", Authority: "TENANT_USER"}), errcode.CodeOpDenied)
	if e := svc.DeleteDeviceTemplate("public", a); e != nil {
		t.Fatal(e)
	}
	check(svc.DeleteDeviceTemplate("referenced", a), 200050)
	if e := db.Exec(`CREATE FUNCTION suppress_template_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$; CREATE TRIGGER suppress_template_delete BEFORE DELETE ON device_templates FOR EACH ROW EXECUTE FUNCTION suppress_template_delete()`).Error; e != nil {
		t.Fatal(e)
	}
	check(svc.DeleteDeviceTemplate("a", a), errcode.CodeNotFound)
	if e := db.Exec(`DROP TRIGGER suppress_template_delete ON device_templates`).Error; e != nil {
		t.Fatal(e)
	}
	if e := svc.DeleteDeviceTemplate("a", a); e != nil {
		t.Fatal(e)
	}
	for id, want := range map[string]int64{"a": 0, "b": 1, "public": 0, "referenced": 1} {
		var n int64
		if e := db.Model(&model.DeviceTemplate{}).Where("id=?", id).Count(&n).Error; e != nil || n != want {
			t.Fatalf("%s count %d err %v", id, n, e)
		}
	}
	sqlDB, e := db.DB()
	if e != nil {
		t.Fatal(e)
	}
	pool := &templateCommitPool{DB: sqlDB}
	wrapped, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if e != nil {
		t.Fatal(e)
	}
	global.DB = wrapped
	e = svc.DeleteDeviceTemplate("unknown", a)
	global.DB = db
	var ec *errcode.Error
	if !errors.As(e, &ec) || ec.Code != errcode.CodeDBError || !strings.Contains(ec.CustomMsg, "unknown") {
		t.Fatalf("commit error %v", e)
	}
	var n int64
	if e := db.Model(&model.DeviceTemplate{}).Where("id=?", "unknown").Count(&n).Error; e != nil || n != 0 || pool.commits.Load() != 1 {
		t.Fatalf("count=%d commits=%d err=%v", n, pool.commits.Load(), e)
	}
}
