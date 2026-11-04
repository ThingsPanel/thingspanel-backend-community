package app

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"testing"

	"github.com/spf13/viper"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"

	global "project/pkg/global"
)

func TestBrokerIngressControlPlaneRejectsMySQLPlatform(t *testing.T) {
	if os.Getenv("TP_TEST_BROKER_DIALECT") == "mysql" {
		t.Setenv("GOTP_AUTOMATION_MODE", "legacy")
		viper.Set("mqtt.input_profile", "ingress_v1")
		pool, err := sql.Open("mysql", "user@tcp(127.0.0.1:1)/unused")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pool.Close() })
		db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		previous := global.DB
		global.DB = db
		t.Cleanup(func() { global.DB = previous })
		service := &MQTTService{app: &Application{DB: db}}
		if err := service.configureBrokerIngress(context.Background(), nil); err == nil || err.Error() != "broker control plane requires relational.driver=postgres" {
			t.Fatalf("MySQL platform accepted broker ingress startup: %v", err)
		}
		if _, err := startBrokerCommandWorker(context.Background(), nil, nil); err == nil || err.Error() != "broker control plane requires relational.driver=postgres" {
			t.Fatalf("MySQL platform accepted broker command worker startup: %v", err)
		}
		return
	}
	for _, scenario := range []string{"mysql"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBrokerIngressControlPlaneRejectsMySQLPlatform$")
		cmd.Env = append(os.Environ(), "TP_TEST_BROKER_DIALECT="+scenario)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("scenario %s: %v: %s", scenario, err, output)
		}
	}
}
