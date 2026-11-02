package service

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/pkg/global"
)

// This fixture has its own database/role and a private users schema. Policies
// exist only in a fresh in-process Casbin instance, never in deployment tables.
func dictionaryPermissionFixture(t *testing.T) (*gorm.DB, *casbin.Enforcer) {
	t.Helper()
	dsn := os.Getenv("TP_PLATFORMCONFIG_TEST_DSN")
	if dsn == "" {
		t.Skip("TP_PLATFORMCONFIG_TEST_DSN is required for real PostgreSQL permission verification")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid dictionary fixture DSN")
	}
	loopback := func(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
	if !loopback(config.Host) || config.Database != "tp_takeover_platformconfig" || config.User != "platformconfig_fixture" {
		t.Fatal("refusing non-dedicated dictionary fixture")
	}
	for _, fallback := range config.Fallbacks {
		if !loopback(fallback.Host) {
			t.Fatal("refusing non-loopback fixture fallback")
		}
	}
	owner := stdlib.OpenDB(*config)
	t.Cleanup(func() { _ = owner.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var database, user string
	if err := owner.QueryRowContext(ctx, `SELECT current_database(),current_user`).Scan(&database, &user); err != nil || database != "tp_takeover_platformconfig" || user != "platformconfig_fixture" {
		t.Fatal("dictionary fixture actual database identity mismatch")
	}
	schema := "tp_dictionary_permission_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := owner.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := owner.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); err != nil {
			t.Error(err)
		}
	})
	selected := config.Copy()
	selected.RuntimeParams = map[string]string{}
	for k, v := range config.RuntimeParams {
		selected.RuntimeParams[k] = v
	}
	selected.RuntimeParams["search_path"] = schema
	pool := stdlib.OpenDB(*selected)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE users(id text PRIMARY KEY,tenant_id text,status text,authority text); INSERT INTO users VALUES('dictionary-subject','dictionary-tenant','N','SYS_ADMIN')`).Error; err != nil {
		t.Fatal(err)
	}
	enforcer, err := casbin.NewEnforcer("../../configs/casbin.conf")
	if err != nil {
		t.Fatal(err)
	}
	previousDB, previousEnforcer := global.DB, global.CasbinEnforcer
	global.DB, global.CasbinEnforcer = db, enforcer
	t.Cleanup(func() { global.DB, global.CasbinEnforcer = previousDB, previousEnforcer })
	return db, enforcer
}

func TestDictionaryAuthorizationPostgresCurrentPermission(t *testing.T) {
	db, enforcer := dictionaryPermissionFixture(t)
	ctx := context.Background()
	operations := []string{"create-column", "create-language", "read-enum", "read-page", "read-languages", "delete-column", "delete-language", "read-protocols"}
	request := model.DictionaryAuthorizationRequest{ProtocolVersion: 1, TenantID: "dictionary-tenant", SubjectID: "dictionary-subject"}
	if _, err := enforcer.AddNamedGroupingPolicy("g", request.SubjectID, "dictionary-role"); err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations {
		resource, _ := dictionaryAuthorizationResource(operation)
		if _, err := enforcer.AddNamedGroupingPolicy("g2", resource, "dictionary-function"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := enforcer.AddPolicy("dictionary-role", "dictionary-function", "allow"); err != nil {
		t.Fatal(err)
	}
	allow := func(input model.DictionaryAuthorizationRequest) {
		t.Helper()
		result, err := AuthorizeCurrentDictionaryOperation(ctx, input)
		resource, _ := dictionaryAuthorizationResource(input.Operation)
		if err != nil || result == nil || !result.Authorized || result.ProtocolVersion != 1 || result.TenantID != input.TenantID || result.SubjectID != input.SubjectID || result.Operation != input.Operation || result.Resource != resource {
			t.Fatalf("permission response=%#v error=%v", result, err)
		}
	}
	deny := func(input model.DictionaryAuthorizationRequest) {
		t.Helper()
		result, err := AuthorizeCurrentDictionaryOperation(ctx, input)
		if result != nil || !errors.Is(err, ErrDictionaryPermissionDenied) {
			t.Fatalf("revoked scope response=%#v error=%v", result, err)
		}
	}
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			input := request
			input.Operation = operation
			allow(input)
			resource, _ := dictionaryAuthorizationResource(operation)
			if _, err := enforcer.RemoveNamedGroupingPolicy("g2", resource, "dictionary-function"); err != nil {
				t.Fatal(err)
			}
			deny(input)
			if _, err := enforcer.AddNamedGroupingPolicy("g2", resource, "dictionary-function"); err != nil {
				t.Fatal(err)
			}
			if _, err := enforcer.RemovePolicy("dictionary-role", "dictionary-function", "allow"); err != nil {
				t.Fatal(err)
			}
			deny(input)
			if _, err := enforcer.AddPolicy("dictionary-role", "dictionary-function", "allow"); err != nil {
				t.Fatal(err)
			}
			if _, err := enforcer.RemoveNamedGroupingPolicy("g", input.SubjectID, "dictionary-role"); err != nil {
				t.Fatal(err)
			}
			deny(input)
			if _, err := enforcer.AddNamedGroupingPolicy("g", input.SubjectID, "dictionary-role"); err != nil {
				t.Fatal(err)
			}
			allow(input)
		})
	}
	request.Operation = "read-enum"
	for _, change := range []func(*model.DictionaryAuthorizationRequest){
		func(v *model.DictionaryAuthorizationRequest) { v.TenantID = "foreign" },
		func(v *model.DictionaryAuthorizationRequest) { v.SubjectID = "foreign" },
		func(v *model.DictionaryAuthorizationRequest) { v.ProtocolVersion = 2 },
		func(v *model.DictionaryAuthorizationRequest) { v.Operation = "api/v1/dict" },
	} {
		candidate := request
		change(&candidate)
		deny(candidate)
	}
	for _, change := range []string{"status='F'", "status=NULL", "tenant_id='foreign'", "tenant_id=NULL", "authority=NULL"} {
		if err := db.Exec(`UPDATE users SET ` + change + ` WHERE id='dictionary-subject'`).Error; err != nil {
			t.Fatal(err)
		}
		deny(request)
		if err := db.Exec(`UPDATE users SET status='N',tenant_id='dictionary-tenant',authority='SYS_ADMIN' WHERE id='dictionary-subject'`).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Global dictionary writes/page still require current SYS_ADMIN; ordinary
	// readers need their own explicit current policy and are not tenant owners.
	if err := db.Exec(`UPDATE users SET authority='TENANT_ADMIN' WHERE id='dictionary-subject'`).Error; err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations {
		input := request
		input.Operation = operation
		_, admin := dictionaryAuthorizationResource(operation)
		if admin {
			deny(input)
		} else {
			allow(input)
		}
	}
	if err := db.Exec(`UPDATE users SET authority='SYS_ADMIN' WHERE id='dictionary-subject'`).Error; err != nil {
		t.Fatal(err)
	}
	readOnly := db.Begin(&sql.TxOptions{ReadOnly: true})
	if readOnly.Error != nil {
		t.Fatal(readOnly.Error)
	}
	global.DB = readOnly
	allow(request)
	rollbackErr := readOnly.Rollback().Error
	global.DB = db
	if rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	global.CasbinEnforcer = nil
	if result, err := AuthorizeCurrentDictionaryOperation(ctx, request); result != nil || !errors.Is(err, ErrDictionaryPermissionUnavailable) {
		t.Fatal("missing policy owner did not fail unavailable")
	}
	global.CasbinEnforcer = enforcer
	global.DB = nil
	if result, err := AuthorizeCurrentDictionaryOperation(ctx, request); result != nil || !errors.Is(err, ErrDictionaryPermissionUnavailable) {
		t.Fatal("missing database did not fail unavailable")
	}
	global.DB = db
	if err := db.Exec(`DROP TABLE users`).Error; err != nil {
		t.Fatal(err)
	}
	if result, err := AuthorizeCurrentDictionaryOperation(ctx, request); result != nil || !errors.Is(err, ErrDictionaryPermissionUnavailable) {
		t.Fatal("SQL error became a permission decision")
	}
}

func TestDictionaryAuthorizationPostgresEmptyTenantPlatformAdministrator(t *testing.T) {
	db, enforcer := dictionaryPermissionFixture(t)
	ctx := context.Background()
	request := model.DictionaryAuthorizationRequest{ProtocolVersion: 1, SubjectID: "dictionary-subject"}
	if _, err := enforcer.AddNamedGroupingPolicy("g", request.SubjectID, "dictionary-role"); err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.AddPolicy("dictionary-role", "dictionary-function", "allow"); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"create-column", "create-language", "read-enum", "read-page", "read-languages", "delete-column", "delete-language", "read-protocols"} {
		t.Run(operation, func(t *testing.T) {
			input := request
			input.Operation = operation
			resource, _ := dictionaryAuthorizationResource(operation)
			if _, err := enforcer.AddNamedGroupingPolicy("g2", resource, "dictionary-function"); err != nil {
				t.Fatal(err)
			}
			for _, tenant := range []string{"''", "NULL"} {
				if err := db.Exec(`UPDATE users SET tenant_id=` + tenant + `,authority='SYS_ADMIN',status='N' WHERE id='dictionary-subject'`).Error; err != nil {
					t.Fatal(err)
				}
				result, err := AuthorizeCurrentDictionaryOperation(ctx, input)
				if err != nil || result == nil || !result.Authorized || result.TenantID != "" || result.SubjectID != input.SubjectID || result.Resource != resource {
					t.Fatalf("existing platform authority tenant=%s: %#v %v", tenant, result, err)
				}
			}
			if _, err := enforcer.RemoveNamedGroupingPolicy("g2", resource, "dictionary-function"); err != nil {
				t.Fatal(err)
			}
			if result, err := AuthorizeCurrentDictionaryOperation(ctx, input); result != nil || !errors.Is(err, ErrDictionaryPermissionDenied) {
				t.Fatal("platform role bypassed withdrawn policy")
			}
			if _, err := enforcer.AddNamedGroupingPolicy("g2", resource, "dictionary-function"); err != nil {
				t.Fatal(err)
			}
			for _, change := range []string{"authority='TENANT_ADMIN',tenant_id=''", "authority='TENANT_USER',tenant_id=NULL", "authority='PLATFORM_ADMIN',tenant_id=''", "authority='SYS_ADMIN',tenant_id='foreign'", "authority='SYS_ADMIN',tenant_id='',status='F'"} {
				if err := db.Exec(`UPDATE users SET ` + change + ` WHERE id='dictionary-subject'`).Error; err != nil {
					t.Fatal(err)
				}
				if result, err := AuthorizeCurrentDictionaryOperation(ctx, input); result != nil || !errors.Is(err, ErrDictionaryPermissionDenied) {
					t.Fatalf("empty-tenant exception expanded to %s: %#v %v", change, result, err)
				}
			}
		})
	}
}
