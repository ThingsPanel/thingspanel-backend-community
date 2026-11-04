// Package relationaldb is the Encore side of pkg/relational: it reads relational.driver once per process and hands each
// service a *gorm.DB for its logical resource.
//
//	postgres: the handle adopts the pool behind the Encore sqldb.Database (Encore owns and closes it)
//	mysql:    the handle opens its own pool from relational.mysql.<resource>.* (TP_RELATIONAL_MYSQL_<RESOURCE>_*);
//	          the owning service closes it in Shutdown with Close
//
// The sqldb.NewDatabase declaration and migrations/ directory of a service stay as they are: they define the
// PostgreSQL resource. In MySQL mode the declaration is never dereferenced here (Stdlib is not called).
package relationaldb

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"encore.dev/storage/sqldb"
	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
)

var (
	mu       sync.Mutex
	resolved bool
	dialect  relational.Dialect
	resolveE error
	registry = relational.NewRegistry()
	source   = relational.EnvSource()
)

// Dialect returns the engine selected by relational.driver (resolved on first use, once per process).
func Dialect() (relational.Dialect, error) {
	mu.Lock()
	defer mu.Unlock()
	return resolveLocked()
}

func resolveLocked() (relational.Dialect, error) {
	if !resolved {
		dialect, resolveE = relational.ResolveDialect(source)
		resolved = true
	}
	return dialect, resolveE
}

// Open returns the handle of a resource and registers it. Calling Open twice for one resource is an error (one pool
// per resource). pg is the service's Encore declaration; it is only used in PostgreSQL mode.
func Open(resource string, pg *sqldb.Database) (*gorm.DB, error) {
	return open(context.Background(), resource, func() (*sql.DB, error) {
		if pg == nil {
			return nil, fmt.Errorf("relationaldb: resource %q has no Encore database declaration", resource)
		}
		return pg.Stdlib(), nil
	})
}

func open(ctx context.Context, resource string, pgPool func() (*sql.DB, error)) (*gorm.DB, error) {
	mu.Lock()
	defer mu.Unlock()
	d, err := resolveLocked()
	if err != nil {
		return nil, err
	}
	if _, dup := registry.Get(resource); dup {
		return nil, fmt.Errorf("relationaldb: resource %q is already open", resource)
	}
	var (
		db    *gorm.DB
		owned bool
	)
	switch d {
	case relational.Postgres:
		pool, err := pgPool()
		if err != nil {
			return nil, err
		}
		db, err = relational.Adopt(relational.Postgres, pool, relational.PoolOptions{})
		if err != nil {
			return nil, err
		}
	default:
		target, err := relational.ResolveTarget(source, resource)
		if err != nil {
			return nil, err
		}
		db, err = relational.Open(ctx, target)
		if err != nil {
			return nil, err
		}
		owned = true
	}
	if err := registry.Register(resource, db, owned); err != nil {
		if owned {
			_ = relational.CloseDB(db)
		}
		return nil, err
	}
	return db, nil
}

// Get returns a handle another service already opened (thingmodel reads device_metadata this way). It never opens.
func Get(resource string) (*gorm.DB, bool) { return registry.Get(resource) }

// Close ends the resource: a MySQL pool this package opened is closed; an adopted PostgreSQL pool is left to Encore.
// Closing an unknown resource is a no-op.
func Close(resource string) error { return registry.CloseOne(resource) }
