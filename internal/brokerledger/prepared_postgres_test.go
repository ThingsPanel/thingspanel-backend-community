package brokerledger

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"testing"
)

type permanentFixture struct{}

func (permanentFixture) Error() string   { return "fixture schema rejection" }
func (permanentFixture) Permanent() bool { return true }

func TestPostgresPreparedIngressRunsBeforeLocksAndRechecksRoute(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	ctx := context.Background()
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")
	id, _, err := store.Accept(ctx, testMessage(tenant, device, "prepare-fence"))
	if err != nil {
		t.Fatal(err)
	}
	prepared := 0
	called := false
	worked, err := store.ProcessOnePrepared(ctx, "cell-a", func(ctx context.Context, readDB *gorm.DB, m Message) (*PreparedOperation, error) {
		prepared++
		if _, ok := readDB.Statement.ConnPool.(gorm.TxCommitter); ok {
			t.Fatal("prepare received transaction")
		}
		// Another connection may move the route while contract/decode work runs.
		err := db.Transaction(func(tx *gorm.DB) error {
			if e := tx.Exec("SET LOCAL lock_timeout='100ms'").Error; e != nil {
				return e
			}
			return tx.Exec("UPDATE broker_transport_bindings SET route_epoch=2 WHERE tenant_id=? AND device_id=?", tenant, device).Error
		})
		if err != nil {
			t.Fatalf("prepare holds route locks: %v", err)
		}
		return &PreparedOperation{Write: func(context.Context, *gorm.DB, Message) error { called = true; return nil }}, nil
	})
	if err != nil || !worked || prepared != 1 || called {
		t.Fatalf("worked=%v prepared=%d called=%v err=%v", worked, prepared, called, err)
	}
	r, err := store.GetReceipt(ctx, tenant, device, id)
	if err != nil || r.State != stateFenced {
		t.Fatalf("receipt=%+v err=%v", r, err)
	}
}

func TestPostgresPreparedIngressPermanentRejectionIsSingleTerminalAttempt(t *testing.T) {
	store, db := postgresStore(t, Limits{MaxAttempts: 5})
	ctx := context.Background()
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")
	id, _, err := store.Accept(ctx, testMessage(tenant, device, "schema-rejection"))
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(context.Context, *gorm.DB, Message) (*PreparedOperation, error) {
		return nil, permanentFixture{}
	}
	worked, err := store.ProcessOnePrepared(ctx, "cell-a", prepare)
	var permanent permanentFixture
	if !worked || !errors.As(err, &permanent) {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	r, err := store.GetReceipt(ctx, tenant, device, id)
	if err != nil || r.State != stateDead || r.Attempts != 1 {
		t.Fatalf("receipt=%+v err=%v", r, err)
	}
	worked, err = store.ProcessOnePrepared(ctx, "cell-a", func(context.Context, *gorm.DB, Message) (*PreparedOperation, error) {
		t.Fatal("permanent rejection retried")
		return nil, nil
	})
	if worked || err != nil {
		t.Fatalf("retry worked=%v err=%v", worked, err)
	}
}

func TestPostgresPreparedOutcomeOnlyAfterRealTerminalCommit(t *testing.T) {
	store, db := postgresStore(t, Limits{MaxAttempts: 3})
	ctx := context.Background()
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")
	if err := db.Exec("CREATE TABLE prepared_effects(id text PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	id, _, err := store.Accept(ctx, testMessage(tenant, device, "outcome"))
	if err != nil {
		t.Fatal(err)
	}
	fail := true
	observed := 0
	prepare := func(context.Context, *gorm.DB, Message) (*PreparedOperation, error) {
		return &PreparedOperation{
			Write: func(ctx context.Context, tx *gorm.DB, m Message) error {
				if err := tx.Exec("INSERT INTO prepared_effects VALUES(?)", m.ID).Error; err != nil {
					return err
				}
				if fail {
					return errors.New("retryable fixture error")
				}
				return nil
			},
			AfterOutcome: func(ctx context.Context, m Message, outcome ReceiptOutcome) {
				observed++
				r, e := store.GetReceipt(ctx, tenant, device, m.ID)
				if e != nil || r.State != stateProcessed || outcome != OutcomeCommitted {
					t.Errorf("callback not terminal: %+v %s %v", r, outcome, e)
				}
				var count int64
				if e = db.Table("prepared_effects").Count(&count).Error; e != nil || count != 1 {
					t.Errorf("callback before business commit: %d %v", count, e)
				}
				panic("observability cannot change committed outcome")
			},
		}, nil
	}
	if _, err = store.ProcessOnePrepared(ctx, "cell-a", prepare); err == nil {
		t.Fatal("fixture failure missing")
	}
	if observed != 0 {
		t.Fatal("pending rollback reported terminal")
	}
	var count int64
	db.Table("prepared_effects").Count(&count)
	if count != 0 {
		t.Fatal("rolled-back effect persisted")
	}
	fail = false
	if err = db.Exec("UPDATE broker_ingress_receipts SET retry_after=now() WHERE id=?", id).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = store.ProcessOnePrepared(ctx, "cell-a", prepare); err != nil || observed != 1 {
		t.Fatalf("committed callback %d err %v", observed, err)
	}
	if worked, e := store.ProcessOnePrepared(ctx, "cell-a", prepare); worked || e != nil || observed != 1 {
		t.Fatal("terminal callback repeated")
	}
}
