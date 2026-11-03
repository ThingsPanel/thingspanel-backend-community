package initialize

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrMySQLTemporaryTablePrivilege is returned when the application account can create temporary
// tables. A session-local table named like device_id_reservations shadows the real one inside the
// device identity triggers and defeats the fence; PostgreSQL avoids this by pinning pg_temp last,
// MySQL has no equivalent, so the privilege must be withheld.
var ErrMySQLTemporaryTablePrivilege = errors.New("the MySQL application account holds CREATE TEMPORARY TABLES, which can bypass the device identity fence; revoke it")

// RequireNoTemporaryTablePrivilege probes the real privilege instead of parsing grants (roles and
// wildcard grants make parsing unreliable): it tries to create a temporary table and treats
// success as failure of the guard.
func RequireNoTemporaryTablePrivilege(db *gorm.DB) error {
	pool, err := db.DB()
	if err != nil {
		return err
	}
	conn, err := pool.Conn(db.Statement.Context)
	if err != nil {
		return err
	}
	defer conn.Close()
	name := "tp_priv_probe_" + uuid.NewString()[:8]
	if _, err := conn.ExecContext(db.Statement.Context, fmt.Sprintf("CREATE TEMPORARY TABLE %s (id INT)", name)); err != nil {
		return nil // refused: the guard holds
	}
	_, _ = conn.ExecContext(db.Statement.Context, "DROP TEMPORARY TABLE "+name)
	return ErrMySQLTemporaryTablePrivilege
}
