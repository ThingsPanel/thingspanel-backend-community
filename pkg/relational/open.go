package relational

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver used for Postgres
)

func gormConfig(p PoolOptions) *gorm.Config {
	return &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.LogLevel(p.LogLevel)),
		SkipDefaultTransaction: true,
		NamingStrategy:         schema.NamingStrategy{SingularTable: false},
	}
}

func dialector(d Dialect, pool *sql.DB) (gorm.Dialector, error) {
	switch d {
	case Postgres:
		return gormpostgres.New(gormpostgres.Config{Conn: pool}), nil
	case MySQL:
		// SkipInitializeWithVersion: no statement is sent while the dialector initialises, and nothing here uses
		// AutoMigrate, the only feature that reads the server version.
		return gormmysql.New(gormmysql.Config{Conn: pool, SkipInitializeWithVersion: true}), nil
	}
	return nil, fmt.Errorf("relational: no valid dialect")
}

// Open creates the connection pool of one logical resource, verifies it with a ping, and returns the GORM handle.
// The caller owns the pool and must close it (CloseDB, or Registry.Close when the handle was registered).
func Open(ctx context.Context, t Target) (*gorm.DB, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	dsn, err := t.dsn()
	if err != nil {
		return nil, err
	}
	driverName := "pgx"
	if t.Dialect == MySQL {
		driverName = "mysql"
	}
	pool, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("relational: open %q (%s): %w", t.Name, t.Dialect, redact(err, t))
	}
	db, err := Adopt(t.Dialect, pool, t.Pool)
	if err != nil {
		_ = pool.Close()
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("relational: ping %q (%s): %w", t.Name, t.Dialect, redact(err, t))
	}
	return db, nil
}

// Adopt wraps a pool somebody else created (for example the *sql.DB behind an Encore sqldb.Database) in a GORM handle
// and applies the pool options. The pool's owner keeps closing it, so register a handle from Adopt with owned=false.
func Adopt(d Dialect, pool *sql.DB, p PoolOptions) (*gorm.DB, error) {
	if pool == nil {
		return nil, fmt.Errorf("relational: adopt: nil pool")
	}
	p = DefaultPool(p)
	dial, err := dialector(d, pool)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dial, gormConfig(p))
	if err != nil {
		return nil, fmt.Errorf("relational: gorm open (%s): %w", d, err)
	}
	pool.SetMaxOpenConns(p.MaxOpenConns)
	pool.SetMaxIdleConns(p.MaxIdleConns)
	pool.SetConnMaxLifetime(p.ConnMaxLifetime)
	if p.ConnMaxIdleTime > 0 {
		pool.SetConnMaxIdleTime(p.ConnMaxIdleTime)
	}
	return db, nil
}

// CloseDB closes the pool behind db. Call it only for a pool you created with Open.
func CloseDB(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	return pool.Close()
}

// redact keeps a connection error useful without echoing the DSN (and so the password) back.
func redact(err error, t Target) error {
	msg := err.Error()
	for _, secret := range []string{t.DSN, t.Password} {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "***")
		}
	}
	if msg == err.Error() {
		return err
	}
	return fmt.Errorf("%s", msg)
}
