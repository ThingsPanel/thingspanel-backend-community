package dal

import (
	"fmt"
	"os"
	"testing"
	"time"

	"project/internal/query"
	"project/pkg/global"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeviceGroupRawQueriesQuoteReservedTableOnMySQL(t *testing.T) {
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if host == "" || port == "" || os.Getenv("TP_MYSQL_TEST_USER") == "" || os.Getenv("TP_MYSQL_TEST_DB") == "" {
		t.Skip("TP_MYSQL_TEST_HOST/PORT/USER/DB required")
	}
	cfg := mysqldriver.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr = os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), "tcp", host+":"+port
	cfg.DBName, cfg.ParseTime, cfg.Loc = os.Getenv("TP_MYSQL_TEST_DB"), true, time.UTC
	admin, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	dbName := fmt.Sprintf("tp_group_quote_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE `" + dbName + "`").Error; err != nil {
		_ = adminPool.Close()
		t.Fatal(err)
	}
	cfg.DBName = dbName
	my, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Discard, SkipDefaultTransaction: true})
	if err != nil {
		_ = admin.Exec("DROP DATABASE `" + dbName + "`")
		_ = adminPool.Close()
		t.Fatal(err)
	}
	pool, err := my.DB()
	if err != nil {
		_ = admin.Exec("DROP DATABASE `" + dbName + "`")
		_ = adminPool.Close()
		t.Fatal(err)
	}
	previous := global.DB
	t.Cleanup(func() {
		global.DB = previous
		if previous != nil {
			query.SetDefault(previous)
		}
		_ = pool.Close()
		_ = admin.Exec("DROP DATABASE `" + dbName + "`")
		_ = adminPool.Close()
	})
	for _, ddl := range []string{
		"CREATE TABLE `groups` (id VARCHAR(36) PRIMARY KEY, parent_id VARCHAR(36), name VARCHAR(100), tenant_id VARCHAR(100))",
		"CREATE TABLE r_group_device (group_id VARCHAR(36), device_id VARCHAR(36), tenant_id VARCHAR(100))",
	} {
		if err := my.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	global.DB = my
	query.SetDefault(my)

	for _, row := range []struct {
		id, parent, name string
	}{
		{id: "root", parent: "0", name: "Root"},
		{id: "child", parent: "root", name: "Child"},
	} {
		if err := my.Exec("INSERT INTO `groups` (id, parent_id, name, tenant_id) VALUES (?, ?, ?, ?)", row.id, row.parent, row.name, "tenant-x").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := my.Exec("INSERT INTO r_group_device (group_id, device_id, tenant_id) VALUES (?, ?, ?)", "child", "device-x", "tenant-x").Error; err != nil {
		t.Fatal(err)
	}

	paths, err := GetDeviceGroupPathsByDeviceIDs([]string{"device-x"}, "tenant-x", false)
	if err != nil {
		t.Fatalf("device group paths: %v", err)
	}
	if len(paths) != 1 || paths[0].GroupPath != "Root/Child" {
		t.Fatalf("unexpected device group paths: %#v", paths)
	}

	tier, err := GetDeviceGroupTierById("child", "tenant-x")
	if err != nil {
		t.Fatalf("device group tier: %v", err)
	}
	if got := tier["group_path"]; got != "Root/Child" {
		t.Fatalf("unexpected group tier path: %#v", got)
	}

	children, err := GetGroupChildrenIds("root")
	if err != nil {
		t.Fatalf("device group children: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("unexpected group children: %#v", children)
	}
}
