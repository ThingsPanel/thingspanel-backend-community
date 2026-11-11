package relational

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestClassify(t *testing.T) {
	pg := func(code string) error {
		return fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: code, ConstraintName: "c"})
	}
	my := func(n uint16) error { return fmt.Errorf("wrapped: %w", &mysqldriver.MySQLError{Number: n}) }
	cases := []struct {
		name string
		err  error
		want Kind
	}{
		{"nil", nil, KindOther},
		{"plain", fmt.Errorf("boom"), KindOther},
		{"sql no rows", fmt.Errorf("x: %w", sql.ErrNoRows), KindNotFound},
		{"gorm not found", gorm.ErrRecordNotFound, KindNotFound},
		{"deadline", fmt.Errorf("x: %w", context.DeadlineExceeded), KindTimeout},
		{"canceled", context.Canceled, KindTimeout},
		{"net timeout", fmt.Errorf("x: %w", timeoutErr{}), KindTimeout},
		{"pg unique", pg("23505"), KindUnique},
		{"pg fk", pg("23503"), KindForeignKey},
		{"pg deadlock", pg("40P01"), KindDeadlock},
		{"pg serialization", pg("40001"), KindDeadlock},
		{"pg cancelled statement", pg("57014"), KindTimeout},
		{"pg lock not available", pg("55P03"), KindTimeout},
		{"pg other", pg("42P01"), KindOther},
		{"my unique", my(1062), KindUnique},
		{"my fk child", my(1452), KindForeignKey},
		{"my fk parent", my(1451), KindForeignKey},
		{"my deadlock", my(1213), KindDeadlock},
		{"my lock wait", my(1205), KindTimeout},
		{"my max exec time", my(3024), KindTimeout},
		{"my other", my(1146), KindOther},
	}
	for _, c := range cases {
		if got := Classify(c.err); got != c.want {
			t.Errorf("%s: Classify = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRetryable(t *testing.T) {
	for k, want := range map[Kind]bool{KindDeadlock: true, KindTimeout: true, KindUnique: false, KindNotFound: false, KindOther: false, KindForeignKey: false} {
		if k.Retryable() != want {
			t.Errorf("%v.Retryable() = %v", k, !want)
		}
	}
}

func TestIsUniqueNamesTheConstraint(t *testing.T) {
	pg := &pgconn.PgError{Code: "23505", ConstraintName: "devices_number_key"}
	if !IsUnique(pg, "") || !IsUnique(pg, "devices_number_key") || IsUnique(pg, "other_key") {
		t.Error("postgres constraint matching")
	}
	for _, msg := range []string{"Duplicate entry 'a' for key 'devices.devices_number_key'", "Duplicate entry 'a' for key 'devices_number_key'"} {
		my := &mysqldriver.MySQLError{Number: 1062, Message: msg}
		if !IsUnique(my, "") || !IsUnique(my, "devices_number_key") || IsUnique(my, "number_key") {
			t.Errorf("mysql constraint matching: %s", msg)
		}
	}
	if IsUnique(&pgconn.PgError{Code: "23503"}, "") || IsUnique(nil, "") {
		t.Error("non-unique accepted")
	}
}

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{KindUnique: "unique", KindNotFound: "not_found", KindTimeout: "timeout", KindDeadlock: "deadlock", KindForeignKey: "foreign_key", KindOther: "other"} {
		if k.String() != want {
			t.Errorf("%d -> %s", k, k.String())
		}
	}
}
