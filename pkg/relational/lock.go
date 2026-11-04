package relational

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"gorm.io/gorm"
)

// AdvisoryLocksTable is the table LockTx uses on MySQL. Every MySQL schema whose code calls LockTx must create it in
// its baseline migration with AdvisoryLocksDDL (it is not created here: a lock helper must not run DDL).
const AdvisoryLocksTable = "tp_advisory_locks"

// AdvisoryLocksDDL is the baseline-migration statement for AdvisoryLocksTable.
const AdvisoryLocksDDL = "CREATE TABLE IF NOT EXISTS " + AdvisoryLocksTable +
	" (lock_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY) ENGINE=InnoDB"

// ErrNotInTransaction: LockTx was given a handle that is not an open transaction, where a lock would be meaningless.
var ErrNotInTransaction = errors.New("relational: LockTx must be called inside a transaction")

// LockTx takes a transaction-scoped mutual-exclusion lock on each key and holds it until tx commits or rolls back.
//
//	PostgreSQL: pg_advisory_xact_lock(hashtextextended(key, 0))
//	MySQL:      INSERT ... ON DUPLICATE KEY UPDATE (an exclusive row lock) on one row of tp_advisory_locks, keyed by SHA-256 of the key.
//	            (GET_LOCK is session-scoped and does not end with the transaction, so it is not a substitute.)
//
// This is the one cross-module exception to "no shared SQL in pkg/relational" (decision O3): the semantics must be
// identical everywhere. With several keys they are de-duplicated and taken in sorted order, so two transactions
// locking {a,b} and {b,a} cannot deadlock. A lock wait that exceeds the server timeout surfaces as KindTimeout.
func LockTx(ctx context.Context, tx *gorm.DB, keys ...string) error {
	if tx == nil {
		return fmt.Errorf("relational: LockTx: nil handle")
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return ErrNotInTransaction
	}
	d, err := DialectOf(tx)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("relational: LockTx: no key")
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	for i, k := range sorted {
		if k == "" {
			return fmt.Errorf("relational: LockTx: empty key")
		}
		if i > 0 && k == sorted[i-1] {
			continue
		}
		switch d {
		case Postgres:
			err = tx.WithContext(ctx).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, k).Error
		default:
			sum := sha256.Sum256([]byte(k))
			h := hex.EncodeToString(sum[:])
			// One statement that takes the row's exclusive lock whether or not the row exists. INSERT IGNORE followed by
			// SELECT ... FOR UPDATE deadlocks under contention (waiters take a shared lock on the duplicate, then all
			// try to upgrade); measured: 72 of 80 contending transactions failed with error 1213.
			err = tx.WithContext(ctx).Exec("INSERT INTO "+AdvisoryLocksTable+" (lock_key) VALUES (?) ON DUPLICATE KEY UPDATE lock_key = lock_key", h).Error
		}
		if err != nil {
			return fmt.Errorf("relational: lock %q: %w", k, err)
		}
	}
	return nil
}
