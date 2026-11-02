package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/repo"
	"project/pkg/global"
)

func storedMarketInstallGrant(t *testing.T, db *gorm.DB, id string) repo.MarketInstallAuthorization {
	t.Helper()
	var grant repo.MarketInstallAuthorization
	if err := db.Where("id=?", id).Take(&grant).Error; err != nil {
		t.Fatal(err)
	}
	return grant
}

func makeMarketInstallGrantLegacy(t *testing.T, db *gorm.DB, id string) repo.MarketInstallAuthorization {
	t.Helper()
	result := db.Model(&repo.MarketInstallAuthorization{}).Where("id=?", id).Updates(map[string]interface{}{"plan_exact": nil, "plan_encoding": nil})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("fixture did not create a historical representation", result.Error)
	}
	return storedMarketInstallGrant(t, db, id)
}

func requireGrantUnchangedExceptExact(t *testing.T, before, after repo.MarketInstallAuthorization) {
	t.Helper()
	after.PlanExact, after.PlanEncoding = before.PlanExact, before.PlanEncoding
	a, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(after)
	if err != nil || string(a) != string(b) {
		t.Fatal("representation repair modified original grant identity, plan, digest, lifetime, session or binding", err)
	}
}

func TestMarketInstallGrantLegacyPlanRecoveryPostgresRedis(t *testing.T) {
	db := marketInstallProviderFixture(t)
	ctx := context.Background()
	t.Run("verified_original_plan_seals_once_with_concurrent_readers", func(t *testing.T) {
		grant, request, _, _ := marketInstallGrantFixture(t)
		before := makeMarketInstallGrantLegacy(t, db, grant.ID)
		var wait sync.WaitGroup
		failures := make(chan error, 12)
		for i := 0; i < 12; i++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				got, plan, err := readCurrentMarketInstallGrant(ctx, request.MarketInstallIdentity)
				if err == nil && (got.ID != grant.ID || got.PlanDigest != grant.PlanDigest || len(plan.Steps) != 1) {
					err = errors.New("concurrent original grant read changed identity")
				}
				failures <- err
			}()
		}
		wait.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		after := storedMarketInstallGrant(t, db, grant.ID)
		if after.PlanExact == nil || after.PlanEncoding == nil || *after.PlanEncoding != repo.MarketInstallPlanEncoding {
			t.Fatal("verified legacy plan has no exact representation")
		}
		requireGrantUnchangedExceptExact(t, before, after)
		if _, err := AuthorizeMarketInstallStep(ctx, request); err != nil {
			t.Fatal("original step failed after verified recovery", err)
		}
	})
	t.Run("historical_jsonb_exponent_mismatch_never_rehashes_or_binds", func(t *testing.T) {
		step := model.MarketInstallStep{ID: "native:create", Owner: "thingmodel", Action: "create", ResourceKey: "native", Payload: json.RawMessage(`{"value":1e+20}`)}
		grant, request, _, _ := marketInstallGrantFixture(t, step)
		before := makeMarketInstallGrantLegacy(t, db, grant.ID)
		if plan, err := AuthorizeMarketInstallStep(ctx, request); !errors.Is(err, repo.ErrMarketInstallPlanIntegrity) || len(plan.Steps) != 0 {
			t.Fatalf("incompatible historical plan was executed or returned: %v", err)
		}
		after := storedMarketInstallGrant(t, db, grant.ID)
		if after.PlanExact != nil || after.PlanEncoding != nil {
			t.Fatal("incompatible old plan was adopted")
		}
		requireGrantUnchangedExceptExact(t, before, after)
	})
	t.Run("permission_withdrawal_does_not_seal_legacy_grant", func(t *testing.T) {
		grant, request, claims, _ := marketInstallGrantFixture(t)
		before := makeMarketInstallGrantLegacy(t, db, grant.ID)
		changed, err := global.CasbinEnforcer.RemoveNamedGroupingPolicy("g", claims.ID, "a08-grant-fixture-role")
		if err != nil || !changed {
			t.Fatal("fixture permission was not withdrawn", err)
		}
		if got, plan, err := readCurrentMarketInstallGrant(ctx, request.MarketInstallIdentity); !errors.Is(err, repo.ErrMarketInstallUnauthorized) || got != nil || len(plan.Steps) != 0 {
			t.Fatalf("revoked permission recovered an executable plan: %v", err)
		}
		after := storedMarketInstallGrant(t, db, grant.ID)
		if after.PlanExact != nil || after.PlanEncoding != nil {
			t.Fatal("denied request wrote representation metadata")
		}
		requireGrantUnchangedExceptExact(t, before, after)
	})
	t.Run("constraint_rejects_half_pair_and_unknown_encoding", func(t *testing.T) {
		grant, _, _, _ := marketInstallGrantFixture(t)
		for _, statement := range []string{
			"UPDATE market_install_authorizations SET plan_exact=NULL WHERE id=?",
			"UPDATE market_install_authorizations SET plan_encoding=NULL WHERE id=?",
			"UPDATE market_install_authorizations SET plan_encoding='future-number-format' WHERE id=?",
		} {
			if err := db.Exec(statement, grant.ID).Error; err == nil {
				t.Fatal("database accepted an unsupported representation")
			}
		}
	})
	t.Run("tampered_exact_text_preserves_guard", func(t *testing.T) {
		grant, request, _, _ := marketInstallGrantFixture(t)
		if err := db.Exec("UPDATE market_install_authorizations SET plan_exact='{}' WHERE id=?", grant.ID).Error; err != nil {
			t.Fatal(err)
		}
		if plan, err := AuthorizeMarketInstallStep(ctx, request); !errors.Is(err, repo.ErrMarketInstallPlanIntegrity) || len(plan.Steps) != 0 {
			t.Fatalf("changed exact plan bypassed original digest: %v", err)
		}
		if after := storedMarketInstallGrant(t, db, grant.ID); after.OperationID != "" || after.PlanDigest != grant.PlanDigest {
			t.Fatal("corrupted plan was bound or rehashed")
		}
	})
}

func marketInstallGrantWriteFault(t *testing.T, db *gorm.DB, event, failure string) func() {
	t.Helper()
	if event != "INSERT" && event != "UPDATE" {
		t.Fatal("invalid fixture event")
	}
	body := "RAISE EXCEPTION 'injected grant persistence failure';"
	switch failure {
	case "suppress":
		body = "RETURN NULL;"
	case "alter_exact":
		body = "NEW.plan_exact := '{}'; RETURN NEW;"
	case "raise":
	default:
		t.Fatal("invalid fixture fault")
	}
	if err := db.Exec("CREATE FUNCTION fail_install_grant_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN " + body + " END $$; CREATE TRIGGER fail_install_grant_write BEFORE " + event + " ON market_install_authorizations FOR EACH ROW EXECUTE FUNCTION fail_install_grant_write()").Error; err != nil {
		t.Fatal(err)
	}
	active := true
	restore := func() {
		if !active {
			return
		}
		if err := db.Exec("DROP TRIGGER fail_install_grant_write ON market_install_authorizations; DROP FUNCTION fail_install_grant_write()").Error; err != nil {
			t.Fatal(err)
		}
		active = false
	}
	t.Cleanup(restore)
	return restore
}

// Fault injection runs the real database commit/rollback first and then loses
// its acknowledgement at the GORM connection boundary. It does not simulate a
// PostgreSQL server restart or a network proxy and is reported at that scope.
type marketGrantReplyLossPool struct {
	*sql.DB
	loseRollback bool
	commits      atomic.Int32
	rollbacks    atomic.Int32
}

func (p *marketGrantReplyLossPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &marketGrantReplyLossTx{Tx: tx, pool: p}, nil
}

type marketGrantReplyLossTx struct {
	*sql.Tx
	pool *marketGrantReplyLossPool
}

func (tx *marketGrantReplyLossTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	tx.pool.commits.Add(1)
	return errors.New("injected grant commit acknowledgement loss")
}

func (tx *marketGrantReplyLossTx) Rollback() error {
	if err := tx.Tx.Rollback(); err != nil {
		return err
	}
	tx.pool.rollbacks.Add(1)
	if tx.pool.loseRollback {
		return errors.New("injected grant rollback acknowledgement loss")
	}
	return nil
}

func withMarketGrantReplyLoss(t *testing.T, db *gorm.DB, rollback bool, call func() error) (error, *marketGrantReplyLossPool) {
	t.Helper()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	loss := &marketGrantReplyLossPool{DB: pool, loseRollback: rollback}
	wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	previous := global.DB
	global.DB = wrapped
	defer func() { global.DB = previous }()
	return call(), loss
}

func TestMarketInstallGrantExactPlanWriteAcknowledgementPostgresRedis(t *testing.T) {
	db := marketInstallProviderFixture(t)
	ctx := context.Background()
	for _, failure := range []string{"suppress", "raise", "alter_exact"} {
		t.Run("create_"+failure, func(t *testing.T) {
			original, _, claims, rawToken := marketInstallGrantFixture(t)
			var plan model.MarketInstallPlan
			if err := json.Unmarshal(original.Plan, &plan); err != nil {
				t.Fatal(err)
			}
			key := "grant-failure-" + uuid.NewString()
			restore := marketInstallGrantWriteFault(t, db, "INSERT", failure)
			grant, err := IssueMarketInstallAuthorization(ctx, model.MarketInstallRequest{BundleKey: "bundle", Version: "1"}, plan, claims, rawToken, key)
			if err == nil || grant != nil {
				t.Fatalf("failed grant insert returned confirmation: grant=%+v err=%v", grant, err)
			}
			if observed, err := repo.FindMarketInstallAuthorizationByKey(ctx, claims.TenantID, claims.ID, key); observed != nil || !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("failed grant remained queryable as committed: %v", err)
			}
			restore()
			if grant, err := IssueMarketInstallAuthorization(ctx, model.MarketInstallRequest{BundleKey: "bundle", Version: "1"}, plan, claims, rawToken, key); err != nil || grant == nil || grant.RequestKey != key {
				t.Fatalf("same key did not recover after confirmed rollback: %v", err)
			}
		})
		t.Run("seal_"+failure, func(t *testing.T) {
			grant, request, _, _ := marketInstallGrantFixture(t)
			before := makeMarketInstallGrantLegacy(t, db, grant.ID)
			restore := marketInstallGrantWriteFault(t, db, "UPDATE", failure)
			if got, plan, err := readCurrentMarketInstallGrant(ctx, request.MarketInstallIdentity); err == nil || got != nil || len(plan.Steps) != 0 {
				t.Fatalf("failed legacy seal returned an executable plan: %v", err)
			}
			after := storedMarketInstallGrant(t, db, grant.ID)
			if after.PlanExact != nil || after.PlanEncoding != nil {
				t.Fatal("failed legacy seal left partial representation")
			}
			requireGrantUnchangedExceptExact(t, before, after)
			restore()
			if got, _, err := readCurrentMarketInstallGrant(ctx, request.MarketInstallIdentity); err != nil || got.ID != grant.ID {
				t.Fatalf("same grant did not recover after confirmed rollback: %v", err)
			}
		})
	}
	t.Run("committed_new_grant_reply_loss_recovers_only_by_original_key", func(t *testing.T) {
		original, _, claims, rawToken := marketInstallGrantFixture(t)
		var plan model.MarketInstallPlan
		if err := json.Unmarshal(original.Plan, &plan); err != nil {
			t.Fatal(err)
		}
		key := "grant-lost-reply-" + uuid.NewString()
		var returned *repo.MarketInstallAuthorization
		err, loss := withMarketGrantReplyLoss(t, db, false, func() error {
			var err error
			returned, err = IssueMarketInstallAuthorization(ctx, model.MarketInstallRequest{BundleKey: "bundle", Version: "1"}, plan, claims, rawToken, key)
			return err
		})
		if returned != nil || !errors.Is(err, repo.ErrMarketInstallGrantOutcomeUnknown) || loss.commits.Load() != 1 {
			t.Fatalf("lost commit acknowledgement returned success/known failure: grant=%+v err=%v commits=%d", returned, err, loss.commits.Load())
		}
		observed, err := repo.FindMarketInstallAuthorizationByKey(ctx, claims.TenantID, claims.ID, key)
		if err != nil || observed == nil || observed.PlanExact == nil || observed.RequestKey != key {
			t.Fatalf("committed exact grant not recoverable by original key: %v", err)
		}
		if got, _, err := readCurrentMarketInstallGrant(ctx, model.MarketInstallIdentity{TenantID: claims.TenantID, SubjectID: claims.ID, AuthorizationRef: observed.ID}); err != nil || got.ID != observed.ID {
			t.Fatalf("original persisted grant was not usable after querying uncertain result: %v", err)
		}
		var count int64
		if err := db.Model(&repo.MarketInstallAuthorization{}).Where("tenant_id=? AND request_key=?", claims.TenantID, key).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("original key acquired multiple grants", count, err)
		}
	})
	t.Run("committed_legacy_seal_reply_loss_returns_no_plan", func(t *testing.T) {
		grant, request, _, _ := marketInstallGrantFixture(t)
		before := makeMarketInstallGrantLegacy(t, db, grant.ID)
		var got *repo.MarketInstallAuthorization
		var plan model.MarketInstallPlan
		err, loss := withMarketGrantReplyLoss(t, db, false, func() error {
			var err error
			got, plan, err = readCurrentMarketInstallGrant(ctx, request.MarketInstallIdentity)
			return err
		})
		if got != nil || len(plan.Steps) != 0 || !errors.Is(err, repo.ErrMarketInstallGrantOutcomeUnknown) || loss.commits.Load() != 1 {
			t.Fatalf("uncertain legacy seal returned an executable plan: %v", err)
		}
		after := storedMarketInstallGrant(t, db, grant.ID)
		if after.PlanExact == nil || after.PlanEncoding == nil {
			t.Fatal("fault injection did not commit real legacy metadata")
		}
		requireGrantUnchangedExceptExact(t, before, after)
		if got, _, err := readCurrentMarketInstallGrant(ctx, request.MarketInstallIdentity); err != nil || got.ID != grant.ID {
			t.Fatalf("original grant did not resolve after lost seal acknowledgement: %v", err)
		}
	})
	t.Run("rollback_reply_loss_stays_unknown", func(t *testing.T) {
		grant, request, _, _ := marketInstallGrantFixture(t)
		before := makeMarketInstallGrantLegacy(t, db, grant.ID)
		marketInstallGrantWriteFault(t, db, "UPDATE", "raise")
		var got *repo.MarketInstallAuthorization
		var plan model.MarketInstallPlan
		err, loss := withMarketGrantReplyLoss(t, db, true, func() error {
			var err error
			got, plan, err = readCurrentMarketInstallGrant(ctx, request.MarketInstallIdentity)
			return err
		})
		if got != nil || len(plan.Steps) != 0 || !errors.Is(err, repo.ErrMarketInstallGrantOutcomeUnknown) || loss.rollbacks.Load() != 1 || loss.commits.Load() != 0 {
			t.Fatalf("lost rollback acknowledgement was misclassified: %v", err)
		}
		after := storedMarketInstallGrant(t, db, grant.ID)
		if after.PlanExact != nil || after.PlanEncoding != nil {
			t.Fatal("real rollback left exact metadata")
		}
		requireGrantUnchangedExceptExact(t, before, after)
	})
}
