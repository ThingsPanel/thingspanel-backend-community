package relational

import (
	"context"
	"database/sql"
	"errors"
	"net"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Kind is the engine-independent class of a database error. Repositories branch on Kind, never on a vendor code.
type Kind int

const (
	// KindOther is every error not listed below; it is passed through unchanged.
	KindOther Kind = iota
	// KindUnique is a unique or primary key violation (PostgreSQL 23505, MySQL 1062).
	KindUnique
	// KindNotFound means no row matched (sql.ErrNoRows, gorm.ErrRecordNotFound).
	KindNotFound
	// KindTimeout is a cancelled or expired statement, or a lock wait that gave up: context deadline, PostgreSQL
	// 57014 and 55P03, MySQL 1205 and 3024, a network timeout.
	KindTimeout
	// KindDeadlock is a deadlock victim or a serialization failure; the whole transaction is safe to retry
	// (PostgreSQL 40P01 and 40001, MySQL 1213).
	KindDeadlock
	// KindForeignKey is a foreign key violation (PostgreSQL 23503, MySQL 1451 and 1452).
	KindForeignKey
)

func (k Kind) String() string {
	switch k {
	case KindUnique:
		return "unique"
	case KindNotFound:
		return "not_found"
	case KindTimeout:
		return "timeout"
	case KindDeadlock:
		return "deadlock"
	case KindForeignKey:
		return "foreign_key"
	}
	return "other"
}

// Retryable reports whether re-running the whole transaction may succeed: a deadlock victim, or a lock wait timeout.
func (k Kind) Retryable() bool { return k == KindDeadlock || k == KindTimeout }

// Classify maps err to a Kind. A nil error is KindOther.
func Classify(err error) Kind {
	if err == nil {
		return KindOther
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, gorm.ErrRecordNotFound) {
		return KindNotFound
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return KindTimeout
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return KindUnique
		case "23503":
			return KindForeignKey
		case "40P01", "40001":
			return KindDeadlock
		case "57014", "55P03":
			return KindTimeout
		}
		return KindOther
	}
	var my *mysqldriver.MySQLError
	if errors.As(err, &my) {
		switch my.Number {
		case 1062:
			return KindUnique
		case 1451, 1452:
			return KindForeignKey
		case 1213:
			return KindDeadlock
		case 1205, 3024, 1317:
			return KindTimeout
		}
		return KindOther
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return KindTimeout
	}
	return KindOther
}

// Is reports whether err is of kind k.
func Is(err error, k Kind) bool { return err != nil && Classify(err) == k }

// IsUnique reports a unique violation. When constraint is not empty it must also name the violated constraint
// (PostgreSQL constraint name) or index (MySQL, from the "table.index" form of its message); that is how a caller
// tells "duplicate device number" from "duplicate idempotency key" when one INSERT can hit either.
func IsUnique(err error, constraint string) bool {
	if !Is(err, KindUnique) {
		return false
	}
	if constraint == "" {
		return true
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.ConstraintName == constraint
	}
	var my *mysqldriver.MySQLError
	if errors.As(err, &my) {
		return containsIndex(my.Message, constraint)
	}
	return false
}

// containsIndex matches the index name in MySQL's "Duplicate entry 'x' for key 'table.index'" (8.0 and later) or
// "... for key 'index'" (older) message.
func containsIndex(msg, index string) bool {
	return hasSuffixQuoted(msg, "'"+index+"'") || hasSuffixQuoted(msg, "."+index+"'")
}

func hasSuffixQuoted(msg, suffix string) bool {
	if len(msg) < len(suffix) {
		return false
	}
	return msg[len(msg)-len(suffix):] == suffix
}
