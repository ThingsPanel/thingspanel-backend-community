package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/utils"
	componentsql "project/sql"
)

func TestServiceAccessCreateOperationPostgres(t *testing.T) {
	db := brokerEvidenceTestDB(t)
	if err := db.AutoMigrate(&model.ServiceAccess{}); err != nil {
		t.Fatal(err)
	}
	files, err := componentsql.ServiceAccessMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, file := range files {
			if err := tx.Exec(string(file.Contents)).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	q := query.Use(db)
	oldAccess, oldDevice := query.ServiceAccess, query.Device
	query.ServiceAccess, query.Device = &q.ServiceAccess, &q.Device
	t.Cleanup(func() { query.ServiceAccess, query.Device = oldAccess, oldDevice })
	ctx := context.Background()
	svc := &ServiceAccess{}
	fixture := func() (*model.CreateAccessReq, *utils.UserClaims) {
		return &model.CreateAccessReq{OperationKey: uuid.NewString(), Name: "fixture", ServicePluginID: "fixture-plugin", Voucher: "fixture-opaque", ServiceAccessConfig: `{"mode":"test"}`},
			&utils.UserClaims{ID: uuid.NewString(), TenantID: uuid.NewString()}
	}
	count := func(table, tenant string) int64 {
		t.Helper()
		var n int64
		if err := db.Table(table).Where("tenant_id = ?", tenant).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	assertNoWrites := func(tenant string) {
		t.Helper()
		for _, table := range []string{"service_access", "service_access_create_receipts"} {
			if n := count(table, tenant); n != 0 {
				t.Fatalf("failed creation left %s=%d", table, n)
			}
		}
	}
	assertUnknown := func(resp map[string]interface{}, err error) {
		t.Helper()
		var code *errcode.Error
		if resp != nil || !errors.As(err, &code) || code.Code != errcode.CodeDBError || !strings.Contains(code.CustomMsg, "unknown") {
			t.Fatalf("expected explicit unknown, got response=%v err=%v", resp, err)
		}
	}
	withPool := func(wrapped *gorm.DB, call func() (map[string]interface{}, error)) (map[string]interface{}, error) {
		wrappedQuery := query.Use(wrapped)
		previous := query.ServiceAccess
		query.ServiceAccess = &wrappedQuery.ServiceAccess
		defer func() { query.ServiceAccess = previous }()
		return call()
	}

	t.Run("same_key_replays_exactly_one_resource_after_concurrent_requests", func(t *testing.T) {
		req, claims := fixture()
		type outcome struct {
			response map[string]interface{}
			err      error
		}
		results := make(chan outcome, 12)
		var wg sync.WaitGroup
		for i := 0; i < cap(results); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				response, err := svc.CreateAccessContext(ctx, req, claims)
				results <- outcome{response, err}
			}()
		}
		wg.Wait()
		close(results)
		var id string
		fresh := 0
		for result := range results {
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.response["state"] != "confirmed" {
				t.Fatal("creation was not confirmed")
			}
			got, ok := result.response["id"].(string)
			if !ok || got == "" {
				t.Fatal("missing original resource")
			}
			if id == "" {
				id = got
			} else if got != id {
				t.Fatal("original key created multiple resources")
			}
			if result.response["replayed"] == false {
				fresh++
			}
		}
		if fresh != 1 || count("service_access", claims.TenantID) != 1 || count("service_access_create_receipts", claims.TenantID) != 1 {
			t.Fatal("concurrent key did not have exactly one winner")
		}
		observed, err := svc.GetCreateOperation(ctx, req.OperationKey, claims)
		if err != nil || observed.State != "confirmed" || observed.ID != id {
			t.Fatalf("original query=%+v err=%v", observed, err)
		}
		changed := *req
		changed.Name = "different"
		if response, err := svc.CreateAccess(&changed, claims); response != nil || err == nil || !isServiceAccessOperationConflict(err) {
			t.Fatalf("same-key different content=%v %v", response, err)
		}
		otherSubject := *claims
		otherSubject.ID = uuid.NewString()
		if response, err := svc.CreateAccess(req, &otherSubject); response != nil || err == nil {
			t.Fatal("same tenant key was taken over by another subject")
		}
		if observed, err := svc.GetCreateOperation(ctx, req.OperationKey, &otherSubject); observed != nil || err == nil {
			t.Fatal("query exposed another subject's receipt")
		}
		otherTenant := *claims
		otherTenant.TenantID = uuid.NewString()
		if response, err := svc.CreateAccess(req, &otherTenant); err != nil || response["id"] == id {
			t.Fatal("key leaked across tenants", err)
		}
		second := *req
		second.OperationKey = uuid.NewString()
		if response, err := svc.CreateAccess(&second, claims); err != nil || response["id"] == id {
			t.Fatal("distinct installation key did not produce a distinct instance", err)
		}
	})

	t.Run("empty_config_normalization_and_resource_drift_do_not_recreate", func(t *testing.T) {
		req, claims := fixture()
		req.ServiceAccessConfig = ""
		response, err := svc.CreateAccess(req, claims)
		if err != nil {
			t.Fatal(err)
		}
		id := response["id"].(string)
		req.ServiceAccessConfig = "{}"
		if replay, err := svc.CreateAccess(req, claims); err != nil || replay["id"] != id || replay["replayed"] != true {
			t.Fatal("equivalent empty config failed replay", err)
		}
		if err := db.Model(&model.ServiceAccess{}).Where("id=?", id).Updates(map[string]interface{}{"service_access_config": `{"later":true}`, "update_at": time.Now().UTC()}).Error; err != nil {
			t.Fatal(err)
		}
		observed, err := svc.GetCreateOperation(ctx, req.OperationKey, claims)
		if err != nil || observed.State != "requires_reconciliation" || observed.Reason != "RESOURCE_CHANGED" || observed.ID != id {
			t.Fatalf("resource drift=%+v err=%v", observed, err)
		}
		if replay, err := svc.CreateAccess(req, claims); replay != nil || err == nil {
			t.Fatal("later modified resource was reported as unchanged success")
		}
		if err := svc.Delete(id, claims); err != nil {
			t.Fatal(err)
		}
		observed, err = svc.GetCreateOperation(ctx, req.OperationKey, claims)
		if err != nil || observed.State != "requires_reconciliation" || observed.Reason != "RESOURCE_MISSING" || observed.ID != id {
			t.Fatalf("deleted resource receipt=%+v err=%v", observed, err)
		}
		if replay, err := svc.CreateAccess(req, claims); replay != nil || err == nil {
			t.Fatal("deleted original intent was recreated")
		}
		if count("service_access", claims.TenantID) != 0 || count("service_access_create_receipts", claims.TenantID) != 1 {
			t.Fatal("delete discarded receipt or recreated resource")
		}
	})

	for _, table := range []string{"service_access_create_receipts", "service_access"} {
		for _, failure := range []string{"raise", "suppress"} {
			t.Run(table+"_"+failure+"_is_not_success", func(t *testing.T) {
				req, claims := fixture()
				body := "RAISE EXCEPTION 'injected access write failure';"
				if failure == "suppress" {
					body = "RETURN NULL;"
				}
				if err := db.Exec("CREATE FUNCTION fail_access_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN " + body + " END $$; CREATE TRIGGER fail_access_write BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION fail_access_write()").Error; err != nil {
					t.Fatal(err)
				}
				response, createErr := svc.CreateAccess(req, claims)
				if err := db.Exec("DROP TRIGGER fail_access_write ON " + table + "; DROP FUNCTION fail_access_write()").Error; err != nil {
					t.Fatal(err)
				}
				if response != nil || createErr == nil {
					t.Fatalf("database failure reported success: %v %v", response, createErr)
				}
				assertNoWrites(claims.TenantID)
				observed, err := svc.GetCreateOperation(ctx, req.OperationKey, claims)
				if err != nil || observed.State != "not_observed" {
					t.Fatalf("rollback query=%+v err=%v", observed, err)
				}
			})
		}
	}

	t.Run("lost_commit_reply_queries_the_original_durable_result", func(t *testing.T) {
		req, claims := fixture()
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		loss := &parentCommitLossPool{DB: pool}
		wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		response, err := withPool(wrapped, func() (map[string]interface{}, error) { return svc.CreateAccess(req, claims) })
		assertUnknown(response, err)
		if loss.commits.Load() != 1 {
			t.Fatal("unknown commit was automatically retried")
		}
		observed, err := svc.GetCreateOperation(ctx, req.OperationKey, claims)
		if err != nil || observed.State != "confirmed" {
			t.Fatalf("lost reply reconciliation=%+v %v", observed, err)
		}
		if replay, err := svc.CreateAccess(req, claims); err != nil || replay["id"] != observed.ID || replay["replayed"] != true {
			t.Fatal("reconciliation created a different resource", err)
		}
		if count("service_access", claims.TenantID) != 1 || count("service_access_create_receipts", claims.TenantID) != 1 {
			t.Fatal("lost reply duplicated a resource")
		}
	})

	t.Run("lost_rollback_reply_remains_unknown_and_no_receipt_is_invented", func(t *testing.T) {
		req, claims := fixture()
		if err := db.Exec("CREATE FUNCTION fail_access_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected access write failure'; END $$; CREATE TRIGGER fail_access_write BEFORE INSERT ON service_access FOR EACH ROW EXECUTE FUNCTION fail_access_write()").Error; err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		loss := &parentCommitLossPool{DB: pool, rollbackLoss: true}
		wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		response, err := withPool(wrapped, func() (map[string]interface{}, error) { return svc.CreateAccess(req, claims) })
		if dropErr := db.Exec("DROP TRIGGER fail_access_write ON service_access; DROP FUNCTION fail_access_write()").Error; dropErr != nil {
			t.Fatal(dropErr)
		}
		assertUnknown(response, err)
		assertNoWrites(claims.TenantID)
		if loss.commits.Load() != 0 {
			t.Fatal("failed transaction attempted commit")
		}
	})

	t.Run("receipt_query_database_failure_is_not_not_observed", func(t *testing.T) {
		req, claims := fixture()
		if err := db.Exec("ALTER TABLE service_access_create_receipts RENAME TO hidden_service_access_receipts").Error; err != nil {
			t.Fatal(err)
		}
		observed, readErr := svc.GetCreateOperation(ctx, req.OperationKey, claims)
		if err := db.Exec("ALTER TABLE hidden_service_access_receipts RENAME TO service_access_create_receipts").Error; err != nil {
			t.Fatal(err)
		}
		if observed != nil || readErr == nil {
			t.Fatal("unavailable database was treated as authoritative absence")
		}
	})
}

func TestServiceAccessOperationRejectsInvalidInputBeforeSQL(t *testing.T) {
	svc := &ServiceAccess{}
	claims := &utils.UserClaims{ID: "subject", TenantID: "tenant"}
	for _, key := range []string{" ", "bad/key", ":first", strings.Repeat("k", 201), "非ascii"} {
		req := &model.CreateAccessReq{OperationKey: key, Name: "fixture", ServicePluginID: "plugin", Voucher: "fixture"}
		if response, err := svc.CreateAccess(req, claims); response != nil || err == nil {
			t.Fatal("invalid creation key accepted")
		}
		if response, err := svc.GetCreateOperation(context.Background(), key, claims); response != nil || err == nil {
			t.Fatal("invalid query key accepted")
		}
	}
	if response, err := svc.CreateAccess(nil, claims); response != nil || err == nil {
		t.Fatal("nil request accepted")
	}
	if err := svc.Update(nil, claims); err == nil {
		t.Fatal("nil update accepted")
	}
}
