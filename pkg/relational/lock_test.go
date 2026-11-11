package relational_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational/reltest"
)

// Differential tests for LockTx: every step runs on PostgreSQL and on MySQL and must give the same observable result.

// beginLocked starts a transaction, takes keys, and returns it. Commit/rollback is the caller's.
func beginLocked(ctx context.Context, db *gorm.DB, keys ...string) (*gorm.DB, error) {
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	if err := relational.LockTx(ctx, tx, keys...); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// acquiresWithin reports whether a second transaction obtains keys within d (committing it if so).
func acquiresWithin(ctx context.Context, db *gorm.DB, d time.Duration, keys ...string) bool {
	done := make(chan struct{})
	go func() {
		tx, err := beginLocked(ctx, db, keys...)
		if err == nil {
			tx.Rollback()
		}
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		<-done // the blocked acquirer proceeds once the holder ends; wait so no goroutine leaks
		return false
	}
}

func TestLockTxDifferential(t *testing.T) {
	reltest.Run(t, "LockTx", func(s *reltest.Scenario) {
		s.Exec("locks table (MySQL baseline migration part)", "SELECT 1", relational.AdvisoryLocksDDL)

		s.Step("outside a transaction is refused", func(ctx context.Context, db *gorm.DB) (any, error) {
			return errors.Is(relational.LockTx(ctx, db, "k"), relational.ErrNotInTransaction), nil
		})
		s.Step("no key and empty key are refused", func(ctx context.Context, db *gorm.DB) (any, error) {
			tx := db.Begin()
			defer tx.Rollback()
			return []bool{relational.LockTx(ctx, tx) != nil, relational.LockTx(ctx, tx, "") != nil}, nil
		})
		s.Step("second transaction waits while the first holds the key, then proceeds on commit", func(ctx context.Context, db *gorm.DB) (any, error) {
			holder, err := beginLocked(ctx, db, "device:1")
			if err != nil {
				return nil, err
			}
			blocked := make(chan bool, 1)
			go func() { blocked <- !acquiresWithinHeld(ctx, db, 400*time.Millisecond, "device:1") }()
			stillBlocked := <-blocked // true when the waiter could not get the lock in 400ms
			if err := holder.Commit().Error; err != nil {
				return nil, err
			}
			afterCommit := acquiresWithin(ctx, db, 5*time.Second, "device:1")
			return map[string]bool{"blockedWhileHeld": stillBlocked, "freeAfterCommit": afterCommit}, nil
		})
		s.Step("rollback releases the lock", func(ctx context.Context, db *gorm.DB) (any, error) {
			holder, err := beginLocked(ctx, db, "device:2")
			if err != nil {
				return nil, err
			}
			holder.Rollback()
			return acquiresWithin(ctx, db, 5*time.Second, "device:2"), nil
		})
		s.Step("different keys do not block each other", func(ctx context.Context, db *gorm.DB) (any, error) {
			holder, err := beginLocked(ctx, db, "device:3")
			if err != nil {
				return nil, err
			}
			defer holder.Rollback()
			return acquiresWithin(ctx, db, 5*time.Second, "device:4"), nil
		})
		s.Step("the same key twice in one call and in one transaction is reentrant", func(ctx context.Context, db *gorm.DB) (any, error) {
			tx, err := beginLocked(ctx, db, "dup", "dup")
			if err != nil {
				return nil, err
			}
			defer tx.Rollback()
			return relational.LockTx(ctx, tx, "dup") == nil, nil
		})
		s.Step("opposite key orders never deadlock (40 rounds x 2 transactions)", func(ctx context.Context, db *gorm.DB) (any, error) {
			var wg sync.WaitGroup
			errs := make(chan error, 80)
			for i := 0; i < 40; i++ {
				for _, keys := range [][]string{{"a", "b", "c"}, {"c", "b", "a"}} {
					keys := keys
					wg.Add(1)
					go func() {
						defer wg.Done()
						tx, err := beginLocked(ctx, db, keys...)
						if err != nil {
							errs <- err
							return
						}
						time.Sleep(time.Millisecond)
						errs <- tx.Commit().Error
					}()
				}
			}
			wg.Wait()
			close(errs)
			failures := 0
			for err := range errs {
				if err != nil {
					failures++
					fmt.Println("lock round error:", err)
				}
			}
			return failures, nil
		})
	})
}

// acquiresWithinHeld is acquiresWithin for the case where the lock is expected to stay held for the whole window.
// It returns false when the waiter did not finish inside d, and then lets the waiter finish in the background once
// the holder ends (the caller commits the holder right after).
func acquiresWithinHeld(ctx context.Context, db *gorm.DB, d time.Duration, keys ...string) bool {
	done := make(chan struct{})
	go func() {
		tx, err := beginLocked(ctx, db, keys...)
		if err == nil {
			tx.Rollback()
		}
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		go func() { <-done }()
		return false
	}
}
