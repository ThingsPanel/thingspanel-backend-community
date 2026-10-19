package brokercommands

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func dispatchFixture(t *testing.T, key string) (*Store, *gorm.DB, context.Context, string, string, string, Row) {
	t.Helper()
	store, db := postgresStore(t)
	tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	row, _, err := store.Submit(context.Background(), newRequest(tenant, "dispatch-user", root, target, key), persistLog)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	claimed, err := store.Claim(ctx, func(context.Context, *gorm.DB, Row) error { return nil })
	if err != nil || claimed == nil || claimed.ID != row.ID {
		t.Fatalf("claim row=%v err=%v", claimed, err)
	}
	return store, db, ctx, tenant, root, target, *claimed
}

func TestPostgresDispatchSuccessErrorAndTerminalGuard(t *testing.T) {
	t.Run("success is protocol acknowledgement and dispatches once", func(t *testing.T) {
		store, _, ctx, tenant, _, _, row := dispatchFixture(t, "dispatch-success")
		var publishes atomic.Int32
		publish := func(context.Context, Row) error { publishes.Add(1); return nil }
		if err := store.Dispatch(ctx, row.ID, func(context.Context, *gorm.DB, Row) error { return nil }, publish); err != nil {
			t.Fatal(err)
		}
		stored, err := store.GetScoped(ctx, tenant, row.ID)
		if err != nil || stored.State != StateBrokerAccepted {
			t.Fatalf("state=%q err=%v", stored.State, err)
		}
		if err := store.Dispatch(ctx, row.ID, func(context.Context, *gorm.DB, Row) error { return nil }, publish); !errors.Is(err, ErrState) {
			t.Fatalf("second dispatch=%v, want ErrState", err)
		}
		if publishes.Load() != 1 {
			t.Fatalf("publish count=%d, want one", publishes.Load())
		}
		if err := store.Acknowledge(ctx, store.db, tenant, row.TargetDeviceID, row.ID, true); err != nil {
			t.Fatal(err)
		}
		if err := store.FinishPublish(ctx, row.ID, true, "late result"); !errors.Is(err, ErrState) {
			t.Fatalf("late finish after device ACK=%v, want ErrState", err)
		}
		stored, err = store.GetScoped(ctx, tenant, row.ID)
		if err != nil || stored.State != StateDeviceReportedSuccess {
			t.Fatalf("early device ACK state downgraded to %q, err=%v", stored.State, err)
		}
	})

	t.Run("publish error is unknown and never reclaimed", func(t *testing.T) {
		store, _, ctx, tenant, _, _, row := dispatchFixture(t, "dispatch-ambiguous")
		if err := store.Dispatch(ctx, row.ID, func(context.Context, *gorm.DB, Row) error { return nil }, func(context.Context, Row) error { return errors.New("connection lost after write") }); err != nil {
			t.Fatal(err)
		}
		stored, err := store.GetScoped(ctx, tenant, row.ID)
		if err != nil || stored.State != StateUnknown {
			t.Fatalf("state=%q err=%v", stored.State, err)
		}
		claimed, err := store.Claim(ctx, func(context.Context, *gorm.DB, Row) error { return nil })
		if err != nil || claimed != nil {
			t.Fatalf("unknown command reclaimed: row=%v err=%v", claimed, err)
		}
	})

	t.Run("permanent guard rejection makes no publish", func(t *testing.T) {
		store, _, ctx, tenant, _, _, row := dispatchFixture(t, "dispatch-guard")
		var publishes atomic.Int32
		err := store.Dispatch(ctx, row.ID, func(context.Context, *gorm.DB, Row) error { return fmt.Errorf("%w: revoked", ErrRejected) }, func(context.Context, Row) error { publishes.Add(1); return nil })
		if err != nil {
			t.Fatal(err)
		}
		stored, err := store.GetScoped(ctx, tenant, row.ID)
		if err != nil || stored.State != StateFailed || publishes.Load() != 0 {
			t.Fatalf("state=%q publishes=%d err=%v", stored.State, publishes.Load(), err)
		}
	})
}

func TestPostgresDispatchRejectsExpiryAndChangedEpochBeforePublish(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		state  string
		mutate func(*gorm.DB, string, string) error
	}{
		{name: "expired", state: StateExpired, mutate: func(db *gorm.DB, tenant, root string) error {
			return db.Model(&Row{}).Where("tenant_id=?", tenant).Update("expires_at", time.Now().Add(-time.Second)).Error
		}},
		{name: "epoch changed", state: StateFenced, mutate: func(db *gorm.DB, tenant, root string) error {
			return db.Model(&bindingRow{}).Where("tenant_id=? AND device_id=?", tenant, root).Update("route_epoch", 8).Error
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store, db, ctx, tenant, root, _, row := dispatchFixture(t, "dispatch-"+testCase.name)
			if err := testCase.mutate(db, tenant, root); err != nil {
				t.Fatal(err)
			}
			var publishes atomic.Int32
			if err := store.Dispatch(ctx, row.ID, func(context.Context, *gorm.DB, Row) error { return nil }, func(context.Context, Row) error { publishes.Add(1); return nil }); err != nil {
				t.Fatal(err)
			}
			stored, err := store.GetScoped(ctx, tenant, row.ID)
			if err != nil || stored.State != testCase.state || publishes.Load() != 0 {
				t.Fatalf("state=%q publishes=%d err=%v", stored.State, publishes.Load(), err)
			}
		})
	}
}

func TestPostgresClaimInCellDoesNotConsumeOtherCellCommand(t *testing.T) {
	store, _, ctx, tenant, _, _, submitted := dispatchFixtureWithoutClaim(t, "claim-other-cell")
	claimed, err := store.ClaimInCell(ctx, "another-cell", func(context.Context, *gorm.DB, Row) error {
		t.Fatal("other-cell command reached the local guard")
		return nil
	})
	if err != nil || claimed != nil {
		t.Fatalf("other-cell claim=%v err=%v", claimed, err)
	}
	stored, err := store.GetScoped(ctx, tenant, submitted.ID)
	if err != nil || stored.State != StateAccepted {
		t.Fatalf("other-cell worker changed state to %q, err=%v", stored.State, err)
	}
}

func dispatchFixtureWithoutClaim(t *testing.T, key string) (*Store, *gorm.DB, context.Context, string, string, string, Row) {
	t.Helper()
	store, db := postgresStore(t)
	tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	row, _, err := store.Submit(context.Background(), newRequest(tenant, "dispatch-user", root, target, key), persistLog)
	if err != nil {
		t.Fatal(err)
	}
	return store, db, context.Background(), tenant, root, target, row
}

func TestPostgresDispatchHoldsRouteLockAcrossPublish(t *testing.T) {
	store, db, ctx, tenant, root, _, row := dispatchFixture(t, "dispatch-route-lock")
	started, release := make(chan struct{}), make(chan struct{})
	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- store.Dispatch(ctx, row.ID, func(context.Context, *gorm.DB, Row) error { return nil }, func(context.Context, Row) error {
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch did not enter publish callback")
	}
	updateStarted, updateDone := make(chan struct{}), make(chan error, 1)
	go func() {
		close(updateStarted)
		updateDone <- db.Model(&bindingRow{}).Where("tenant_id=? AND device_id=?", tenant, root).Update("route_epoch", 9).Error
	}()
	<-updateStarted
	select {
	case err := <-updateDone:
		t.Fatalf("route update escaped the publish route lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-dispatchDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-updateDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("route update stayed blocked after publish completed")
	}
}
