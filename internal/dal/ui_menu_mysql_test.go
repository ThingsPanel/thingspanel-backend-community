package dal

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
	"project/pkg/utils"
	componentsql "project/sql"
)

// platformMySQL returns a MySQL database holding the whole platform schema and seed rows.
func platformMySQL(t *testing.T) (pg, my *gorm.DB) {
	t.Helper()
	pgDSN := os.Getenv("TEST_PG_SCHEMA_DSN")
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if pgDSN == "" || host == "" || port == "" {
		t.Skip("TEST_PG_SCHEMA_DSN and TP_MYSQL_TEST_* required")
	}
	pg, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = strconv.Atoi(port)
	cfg := mysqldriver.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr = os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), "tcp", host+":"+port
	cfg.DBName, cfg.ParseTime, cfg.Loc, cfg.ClientFoundRows, cfg.MultiStatements = os.Getenv("TP_MYSQL_TEST_DB"), true, time.UTC, true, false
	my, err = gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Discard, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := my.DB()
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dropAll := func() {
		ctx := context.Background()
		_, _ = conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0")
		rows, _ := conn.QueryContext(ctx, "SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = DATABASE()")
		var existing [][2]string
		for rows.Next() {
			var n, kind string
			_ = rows.Scan(&n, &kind)
			existing = append(existing, [2]string{n, kind})
		}
		rows.Close()
		for _, n := range existing {
			if n[1] == "VIEW" {
				_, _ = conn.ExecContext(ctx, "DROP VIEW IF EXISTS `"+n[0]+"`")
			} else {
				_, _ = conn.ExecContext(ctx, "DROP TABLE IF EXISTS `"+n[0]+"`")
			}
		}
		_, _ = conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1")
	}
	dropAll()
	var names []string
	names = append(names, componentsql.MySQLMigrationNames()...)
	names = append(names, componentsql.MySQLAuthMigrationNames()...)
	names = append(names, componentsql.MySQLDeviceStatusMigrationNames()...)
	names = append(names, componentsql.MySQLServiceAccessMigrationNames()...)
	names = append(names, componentsql.MySQLHistoryIntentMigrationNames()...)
	names = append(names, componentsql.MySQLPlatformConfigMigrationNames()...)
	names = append(names, componentsql.MySQLAutomationMigrationNames()...)
	names = append(names, componentsql.MySQLPlatformBaseMigrationNames()...)
	for _, name := range names {
		statements, err := componentsql.MySQLFamilyMigrationStatements(os.Getenv("TP_MYSQL_TEST_FLAVOR"), name)
		if err != nil {
			t.Fatal(err)
		}
		for i, s := range statements {
			if _, err := conn.ExecContext(context.Background(), s); err != nil {
				t.Fatalf("apply %s statement %d: %v", name, i+1, err)
			}
		}
	}
	previous := global.DB
	t.Cleanup(func() {
		global.DB = previous
		if previous != nil {
			query.SetDefault(previous)
		}
		dropAll()
		_ = conn.Close()
		if p, e := my.DB(); e == nil {
			_ = p.Close()
		}
		if p, e := pg.DB(); e == nil {
			_ = p.Close()
		}
	})
	return pg, my
}

func flattenMenu(nodes []*model.UiElementsListRsp, depth int) []string {
	var out []string
	for _, n := range nodes {
		kind := "nil"
		if n.ElementType != nil {
			kind = fmt.Sprint(*n.ElementType)
		}
		out = append(out, fmt.Sprintf("%d:%s:%s", depth, n.ElementCode, kind))
		out = append(out, flattenMenu(n.Children, depth+1)...)
	}
	return out
}

// The authority column is a JSON array of role names; the menu of a role is the elements whose array holds it.
func TestMenuByAuthorityMatchesPostgresOnMySQL(t *testing.T) {
	pg, my := platformMySQL(t)
	_ = sql.ErrNoRows
	menu := func(db *gorm.DB, authority string) []string {
		global.DB = db
		query.SetDefault(db)
		_, data, err := ServeUiElementsListByAuthority(&utils.UserClaims{Authority: authority, TenantID: "tenant-x", ID: "u1"})
		if err != nil {
			t.Fatalf("%s: %v", authority, err)
		}
		list, _ := data.([]*model.UiElementsListRsp)
		return flattenMenu(list, 0)
	}
	for _, authority := range []string{"SYS_ADMIN", "TENANT_ADMIN"} {
		want, got := menu(pg, authority), menu(my, authority)
		if len(want) == 0 {
			t.Fatalf("%s: the PostgreSQL menu is empty; the oracle is not built from sql/1..32", authority)
		}
		// Elements with the same `orders` value come back in an unspecified order on either database, so the
		// comparison is of the sets (with their depth and type), not of the sequence among ties.
		sort.Strings(want)
		sort.Strings(got)
		if !reflect.DeepEqual(want, got) {
			inGot := map[string]bool{}
			for _, e := range got {
				inGot[e] = true
			}
			var onlyPG, onlyMy []string
			inWant := map[string]bool{}
			for _, e := range want {
				inWant[e] = true
				if !inGot[e] {
					onlyPG = append(onlyPG, e)
				}
			}
			for _, e := range got {
				if !inWant[e] {
					onlyMy = append(onlyMy, e)
				}
			}
			t.Errorf("%s menu differs: PostgreSQL %d entries, MySQL %d entries\n only in PostgreSQL: %v\n only in MySQL: %v", authority, len(want), len(got), onlyPG, onlyMy)
		}
		t.Logf("%s: %d menu entries on both", authority, len(want))
	}
}
