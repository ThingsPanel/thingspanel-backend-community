package service

import (
	"database/sql"
	"testing"

	"github.com/spf13/viper"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"project/pkg/errcode"
	"project/pkg/global"
)

func TestTenantLatestTelemetryDoesNotRejectMySQLDialect(t *testing.T) {
	pool, err := sql.Open("mysql", "user@tcp(127.0.0.1:1)/unused")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	previousDB := global.DB
	previousProfile := viper.GetString("grpc.tptodb_type")
	global.DB = db
	viper.Set("grpc.tptodb_type", "")
	t.Cleanup(func() {
		global.DB = previousDB
		viper.Set("grpc.tptodb_type", previousProfile)
	})

	_, err = (&Device{}).getTenantTelemetryDataCurrent("tenant")
	apiErr, ok := err.(*errcode.Error)
	if !ok || apiErr.Code == errcode.CodeServiceUnavailable || apiErr.Code != errcode.CodeDBError {
		t.Fatalf("MySQL must pass the dialect gate and reach the DB query; got %#v", err)
	}
}
