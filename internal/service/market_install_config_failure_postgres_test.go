package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

// Only the final Community configuration/receipt transaction loses its reply.
// The separate existing native-import reservation may also use transactions;
// their acknowledgements must not be intercepted by this fault injection.
type marketConfigReplyLossPool struct {
	*sql.DB
	lockKey      string
	loseRollback bool
	commits      atomic.Int32
	rollbacks    atomic.Int32
}

func (p *marketConfigReplyLossPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &marketConfigReplyLossTx{Tx: tx, pool: p}, nil
}

type marketConfigReplyLossTx struct {
	*sql.Tx
	pool   *marketConfigReplyLossPool
	target bool
}

func (tx *marketConfigReplyLossTx) ExecContext(ctx context.Context, statement string, args ...interface{}) (sql.Result, error) {
	if strings.Contains(statement, "pg_advisory_xact_lock") {
		for _, arg := range args {
			if key, ok := arg.(string); ok && key == tx.pool.lockKey {
				tx.target = true
			}
		}
	}
	return tx.Tx.ExecContext(ctx, statement, args...)
}

func (tx *marketConfigReplyLossTx) Commit() error {
	err := tx.Tx.Commit()
	if !tx.target || err != nil {
		return err
	}
	tx.pool.commits.Add(1)
	return errors.New("injected final configuration commit reply loss")
}

func (tx *marketConfigReplyLossTx) Rollback() error {
	err := tx.Tx.Rollback()
	if !tx.target || err != nil {
		return err
	}
	tx.pool.rollbacks.Add(1)
	if tx.pool.loseRollback {
		return errors.New("injected final configuration rollback reply loss")
	}
	return nil
}

func marketConfigFailureInput() (model.MarketInstallStepRequest, repo.MarketNativeModelImport, *model.DeviceConfig) {
	tenant, subject := uuid.NewString(), uuid.NewString()
	modelID, snapshotID := uuid.NewString(), uuid.NewString()
	revision := 1
	req := model.MarketInstallStepRequest{
		MarketInstallIdentity: model.MarketInstallIdentity{TenantID: tenant, SubjectID: subject},
		OperationID:           uuid.NewString(), IdempotencyKey: strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", ""),
		PayloadHash: strings.Repeat("c", 64), Step: model.MarketInstallStep{ResourceKey: "native"},
	}
	identity := repo.MarketNativeModelImport{
		TenantID: tenant, CreatedBy: subject, MarketTemplateID: "native-template", MarketVersion: "1",
		SourceModelID: uuid.NewString(), SourceSnapshotID: uuid.NewString(), SourceRevision: 1,
		DependencyChecksum: strings.Repeat("a", 64),
	}
	candidate := &model.DeviceConfig{
		ID: uuid.NewString(), TenantID: tenant, Name: "native", DeviceType: "1",
		NativeThingModelID: &modelID, NativeThingModelSnapshotID: &snapshotID,
		NativeThingModelRevision: &revision, NativeBindingRevision: 1,
	}
	return req, identity, candidate
}

func marketConfigWriteFault(t *testing.T, db *gorm.DB, table, failure string) func() {
	t.Helper()
	if table != "device_configs" && table != "market_install_config_receipts" {
		t.Fatal("invalid fixture table")
	}
	body := "RAISE EXCEPTION 'injected configuration write failure';"
	if failure == "suppress" {
		body = "RETURN NULL;"
	} else if failure != "raise" {
		t.Fatal("invalid fixture failure")
	}
	if err := db.Exec("CREATE FUNCTION fail_install_config_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN " + body + " END $$; CREATE TRIGGER fail_install_config_write BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION fail_install_config_write()").Error; err != nil {
		t.Fatal(err)
	}
	active := true
	restore := func() {
		if !active {
			return
		}
		if err := db.Exec("DROP TRIGGER fail_install_config_write ON " + table + "; DROP FUNCTION fail_install_config_write()").Error; err != nil {
			t.Fatal(err)
		}
		active = false
	}
	t.Cleanup(restore)
	return restore
}

func requireMarketConfigRows(t *testing.T, db *gorm.DB, tenant string, want int64) {
	t.Helper()
	for _, table := range []string{"device_configs", "market_install_config_receipts"} {
		var count int64
		if err := db.Table(table).Where("tenant_id=?", tenant).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("%s rows=%d, want %d", table, count, want)
		}
	}
	var completed int64
	if err := db.Table("market_native_model_imports").Where("tenant_id=? AND status='COMPLETE'", tenant).Count(&completed).Error; err != nil {
		t.Fatal(err)
	}
	if completed != want {
		t.Fatalf("complete import mappings=%d, want %d", completed, want)
	}
}

func withMarketConfigReplyLoss(t *testing.T, db *gorm.DB, req model.MarketInstallStepRequest, rollback bool, call func() (*repo.MarketInstallConfigReceipt, error)) (*repo.MarketInstallConfigReceipt, error, *marketConfigReplyLossPool) {
	t.Helper()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	loss := &marketConfigReplyLossPool{DB: pool, lockKey: "a08-config:" + req.TenantID + ":" + req.SubjectID + ":" + req.IdempotencyKey, loseRollback: rollback}
	wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	previous := global.DB
	global.DB = wrapped
	defer func() { global.DB = previous }()
	receipt, callErr := call()
	return receipt, callErr, loss
}

func TestMarketInstallConfigWriteAcknowledgementPostgres(t *testing.T) {
	db := marketInstallProviderFixture(t)
	ctx := context.Background()
	for _, table := range []string{"device_configs", "market_install_config_receipts"} {
		for _, failure := range []string{"raise", "suppress"} {
			t.Run(table+"_"+failure, func(t *testing.T) {
				req, identity, candidate := marketConfigFailureInput()
				restore := marketConfigWriteFault(t, db, table, failure)
				receipt, err := repo.ExecuteMarketInstallConfigBinding(ctx, req, identity, candidate)
				if receipt != nil || err == nil {
					t.Fatalf("database %s returned a confirmation: receipt=%+v err=%v", failure, receipt, err)
				}
				requireMarketConfigRows(t, db, req.TenantID, 0)
				if observed, err := repo.ReadMarketInstallConfigReceipt(ctx, req); observed != nil || !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("rolled-back original key query=%+v err=%v", observed, err)
				}
				restore()
				receipt, err = repo.ExecuteMarketInstallConfigBinding(ctx, req, identity, candidate)
				if err != nil || receipt == nil || receipt.ConfigID != candidate.ID {
					t.Fatalf("original-key retry after confirmed rollback=%+v err=%v", receipt, err)
				}
				requireMarketConfigRows(t, db, req.TenantID, 1)
			})
		}
	}
	t.Run("committed_configuration_reply_loss_is_not_confirmed", func(t *testing.T) {
		req, identity, candidate := marketConfigFailureInput()
		receipt, err, loss := withMarketConfigReplyLoss(t, db, req, false, func() (*repo.MarketInstallConfigReceipt, error) {
			return repo.ExecuteMarketInstallConfigBinding(ctx, req, identity, candidate)
		})
		if receipt != nil || err == nil || loss.commits.Load() != 1 {
			t.Fatalf("lost commit reply returned success or missed final transaction: receipt=%+v err=%v commits=%d", receipt, err, loss.commits.Load())
		}
		requireMarketConfigRows(t, db, req.TenantID, 1)
		observed, err := repo.ReadMarketInstallConfigReceipt(ctx, req)
		if err != nil || observed == nil || observed.ConfigID != candidate.ID {
			t.Fatalf("committed original key query=%+v err=%v", observed, err)
		}
		replayed, err := repo.ExecuteMarketInstallConfigBinding(ctx, req, identity, candidate)
		if err != nil || replayed == nil || replayed.ConfigID != observed.ConfigID {
			t.Fatalf("original-key recovery duplicated/lost configuration: receipt=%+v err=%v", replayed, err)
		}
		requireMarketConfigRows(t, db, req.TenantID, 1)
	})
	t.Run("rollback_reply_loss_is_not_confirmed", func(t *testing.T) {
		req, identity, candidate := marketConfigFailureInput()
		marketConfigWriteFault(t, db, "market_install_config_receipts", "raise")
		receipt, err, loss := withMarketConfigReplyLoss(t, db, req, true, func() (*repo.MarketInstallConfigReceipt, error) {
			return repo.ExecuteMarketInstallConfigBinding(ctx, req, identity, candidate)
		})
		if receipt != nil || err == nil || loss.rollbacks.Load() != 1 || loss.commits.Load() != 0 {
			t.Fatalf("lost rollback reply returned confirmation: receipt=%+v err=%v commits=%d rollbacks=%d", receipt, err, loss.commits.Load(), loss.rollbacks.Load())
		}
		requireMarketConfigRows(t, db, req.TenantID, 0)
		if observed, err := repo.ReadMarketInstallConfigReceipt(ctx, req); observed != nil || !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("rollback-loss query=%+v err=%v", observed, err)
		}
	})
}
