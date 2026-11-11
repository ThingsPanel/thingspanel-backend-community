package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"project/internal/dal"
	"project/internal/model"
	"project/internal/repo"
	"project/pkg/global"
	"project/pkg/utils"
)

func marketInstallProviderFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db := brokerEvidenceTestDB(t)
	useSceneDeleteBarrierDB(t, db)
	if err := db.Exec(`CREATE TABLE users(id text PRIMARY KEY,tenant_id text,status text,authority text,auth_version bigint NOT NULL DEFAULT 0)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"27.sql", "32.sql", "market_install/003_exact_plan.sql"} {
		raw, err := os.ReadFile("../../sql/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(raw)).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Grant issuance now requires the same existing install resource as every
	// public management entrypoint. Seed only this isolated fixture's policies;
	// production missing-policy configurations remain denied.
	enforcer, err := casbin.NewEnforcer("../../configs/casbin.conf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.AddNamedGroupingPolicy("g2", MarketInstallationReadResource, "a08-grant-fixture-function"); err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.AddPolicy("a08-grant-fixture-role", "a08-grant-fixture-function", "allow"); err != nil {
		t.Fatal(err)
	}
	previousEnforcer := global.CasbinEnforcer
	global.CasbinEnforcer = enforcer
	t.Cleanup(func() { global.CasbinEnforcer = previousEnforcer })
	return db
}
func marketInstallGrantFixture(t *testing.T, suppliedSteps ...model.MarketInstallStep) (*repo.MarketInstallAuthorization, model.MarketInstallStepRequest, *utils.UserClaims, string) {
	t.Helper()
	if len(suppliedSteps) > 1 {
		t.Fatal("grant fixture accepts one immutable step")
	}
	ctx := context.Background()
	user, actor := uuid.NewString(), uuid.NewString()
	tenant := "tenant-" + uuid.NewString()
	if err := global.DB.Exec(`INSERT INTO users(id,tenant_id,status,authority,auth_version) VALUES(?,?,'N','TENANT_ADMIN',3),(?,?,'N','TENANT_ADMIN',7)`, user, tenant, actor, tenant).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := global.CasbinEnforcer.AddNamedGroupingPolicy("g", user, "a08-grant-fixture-role"); err != nil {
		t.Fatal(err)
	}
	claims := &utils.UserClaims{ID: user, TenantID: tenant, Authority: "TENANT_ADMIN", AuthVersion: 3, Delegated: true, ActorID: actor, ActorVersion: 7, StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(time.Hour).Unix()}}
	rawToken := "fixture-user-session-" + uuid.NewString()
	if err := global.REDIS.Set(ctx, rawToken, "1", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { global.REDIS.Del(ctx, rawToken, dal.AppPushSessionKey(rawToken)) })
	step := model.MarketInstallStep{ID: "native:create", Owner: "thingmodel", Action: "create", ResourceKey: "native", Payload: json.RawMessage(`{"model":{"name":"fixed"},"resources":{}}`)}
	if len(suppliedSteps) == 1 {
		step = suppliedSteps[0]
	}
	plan := model.MarketInstallPlan{ArtifactDigest: "sha256:" + strings.Repeat("a", 64), Steps: []model.MarketInstallStep{step}}
	request := model.MarketInstallRequest{BundleKey: "bundle", Version: "1"}
	grant, err := IssueMarketInstallAuthorization(ctx, request, plan, claims, rawToken, "key-"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := marketInstallStepHash(step)
	operation := uuid.NewString()
	req := model.MarketInstallStepRequest{ProtocolVersion: 1, MarketInstallIdentity: model.MarketInstallIdentity{TenantID: tenant, SubjectID: user, AuthorizationRef: grant.ID}, OperationID: operation, Generation: 1, StepID: step.ID, IdempotencyKey: marketInstallStepKey(operation, step), PayloadHash: hash, Step: step, PriorResults: []model.MarketInstallPriorResult{}}
	return grant, req, claims, rawToken
}
func TestMarketInstallAuthorizationPostgresRedis(t *testing.T) {
	db := marketInstallProviderFixture(t)
	ctx := context.Background()
	t.Run("exact_scope_and_original_operation", func(t *testing.T) {
		grant, req, _, rawToken := marketInstallGrantFixture(t)
		if _, err := AuthorizeMarketInstallStep(ctx, req); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"tenant", "subject", "step", "key", "hash", "operation"} {
			t.Run(field, func(t *testing.T) {
				forged := req
				switch field {
				case "tenant":
					forged.TenantID = "foreign"
				case "subject":
					forged.SubjectID = "foreign"
				case "step":
					forged.Step.Payload = json.RawMessage(`{"model":{"name":"changed"}}`)
				case "key":
					forged.IdempotencyKey = strings.Repeat("f", 64)
				case "hash":
					forged.PayloadHash = strings.Repeat("f", 64)
				case "operation":
					forged.OperationID = uuid.NewString()
					forged.IdempotencyKey = marketInstallStepKey(forged.OperationID, forged.Step)
				}
				if _, err := AuthorizeMarketInstallStep(ctx, forged); err == nil {
					t.Fatal("forged installation scope authorized")
				}
			})
		}
		raw, err := json.Marshal(grant)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), rawToken) {
			t.Fatal("grant persisted user token")
		}
	})
	t.Run("logout_uses_existing_cleanup_chain", func(t *testing.T) {
		_, req, _, rawToken := marketInstallGrantFixture(t)
		if _, err := AuthorizeMarketInstallStep(ctx, req); err != nil {
			t.Fatal(err)
		}
		if err := (&User{}).Logout(rawToken); err != nil {
			t.Fatal(err)
		}
		if _, err := AuthorizeMarketInstallStep(ctx, req); err == nil {
			t.Fatal("logged-out session retained installation authority")
		}
		if n := global.REDIS.Exists(ctx, dal.AppPushSessionKey(rawToken)).Val(); n != 0 {
			t.Fatal("existing logout failed to delete derived authorization marker")
		}
	})
	for _, change := range []string{"subject_version", "subject_disabled", "actor_version", "actor_disabled", "expiry", "revoked", "plan_tampered"} {
		t.Run(change, func(t *testing.T) {
			grant, req, claims, _ := marketInstallGrantFixture(t)
			var err error
			switch change {
			case "subject_version":
				err = db.Exec("UPDATE users SET auth_version=auth_version+1 WHERE id=?", claims.ID).Error
			case "subject_disabled":
				err = db.Exec("UPDATE users SET status='F' WHERE id=?", claims.ID).Error
			case "actor_version":
				err = db.Exec("UPDATE users SET auth_version=auth_version+1 WHERE id=?", claims.ActorID).Error
			case "actor_disabled":
				err = db.Exec("UPDATE users SET status='F' WHERE id=?", claims.ActorID).Error
			case "expiry":
				err = db.Exec("UPDATE market_install_authorizations SET expires_at=NOW()-INTERVAL '1 second' WHERE id=?", grant.ID).Error
			case "revoked":
				err = db.Exec("UPDATE market_install_authorizations SET revoked_at=NOW() WHERE id=?", grant.ID).Error
			case "plan_tampered":
				err = db.Exec("UPDATE market_install_authorizations SET plan_digest=repeat('0',64) WHERE id=?", grant.ID).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := AuthorizeMarketInstallStep(ctx, req); err == nil {
				t.Fatal("revoked or corrupted grant remained usable")
			}
		})
	}
}
func TestMarketInstallConfigReceiptPostgres(t *testing.T) {
	db := marketInstallProviderFixture(t)
	ctx := context.Background()
	tenant, subject := uuid.NewString(), uuid.NewString()
	modelID, snapshotID := uuid.NewString(), uuid.NewString()
	revision := 1
	identity := repo.MarketNativeModelImport{TenantID: tenant, MarketTemplateID: "native-template", MarketVersion: "1", SourceModelID: uuid.NewString(), SourceSnapshotID: uuid.NewString(), SourceRevision: 1, DependencyChecksum: strings.Repeat("a", 64), CreatedBy: subject}
	req := model.MarketInstallStepRequest{MarketInstallIdentity: model.MarketInstallIdentity{TenantID: tenant, SubjectID: subject}, OperationID: uuid.NewString(), IdempotencyKey: strings.Repeat("b", 64), PayloadHash: strings.Repeat("c", 64), Step: model.MarketInstallStep{ResourceKey: "native"}}
	candidate := func() *model.DeviceConfig {
		return &model.DeviceConfig{ID: uuid.NewString(), TenantID: tenant, Name: "native", DeviceType: "1", NativeThingModelID: &modelID, NativeThingModelSnapshotID: &snapshotID, NativeThingModelRevision: &revision, NativeBindingRevision: 1}
	}
	var wg sync.WaitGroup
	receipts := make(chan *repo.MarketInstallConfigReceipt, 10)
	failures := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := repo.ExecuteMarketInstallConfigBinding(ctx, req, identity, candidate())
			if err != nil {
				failures <- err
			} else {
				receipts <- receipt
			}
		}()
	}
	wg.Wait()
	close(receipts)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var configID string
	for receipt := range receipts {
		if configID == "" {
			configID = receipt.ConfigID
		}
		if receipt.ConfigID != configID {
			t.Fatal("same original step created multiple configurations")
		}
	}
	var count int64
	if err := db.Model(&model.DeviceConfig{}).Where("tenant_id=?", tenant).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("configuration count=%d err=%v", count, err)
	}
	receipt, err := repo.ReadMarketInstallConfigReceipt(ctx, req)
	if err != nil || receipt.ConfigID != configID {
		t.Fatalf("lost reply query: %#v %v", receipt, err)
	}
	forged := req
	forged.PayloadHash = strings.Repeat("d", 64)
	if _, err := repo.ReadMarketInstallConfigReceipt(ctx, forged); !errors.Is(err, repo.ErrMarketInstallUnauthorized) {
		t.Fatalf("same-key content conflict accepted: %v", err)
	}
	if err := db.Model(&model.DeviceConfig{}).Where("id=?", configID).Update("native_binding_revision", 2).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReadMarketInstallConfigReceipt(ctx, req); !errors.Is(err, repo.ErrMarketInstallConfigChanged) {
		t.Fatalf("manual binding edit became confirmed: %v", err)
	}
	if err := db.Delete(&model.DeviceConfig{}, "id=?", configID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReadMarketInstallConfigReceipt(ctx, req); !errors.Is(err, repo.ErrMarketInstallConfigChanged) {
		t.Fatalf("deleted target became absent/recreatable: %v", err)
	}
}
