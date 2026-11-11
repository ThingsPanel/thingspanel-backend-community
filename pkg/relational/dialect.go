// Package relational is the one place the platform decides which relational database it talks to.
//
// It does four things and nothing else: read the configuration (Driver, Target), open a pooled *gorm.DB (Open, Adopt),
// name the dialect so a repository can choose between two explicit implementations (Dialect, DialectOf), and classify
// driver errors (Classify). It never rewrites SQL text and is not a second ORM; see docs/RELATIONAL-ADAPTER-DESIGN.md.
//
// The package has no dependency on encore.dev, so both the platform module and the encore-api module import it.
package relational

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Dialect names a relational engine. The zero value is not valid; use Postgres or MySQL.
type Dialect string

const (
	// Postgres is the default engine (the PostgreSQL + TimescaleDB distribution).
	Postgres Dialect = "postgres"
	// MySQL is the configurable replacement (MySQL 8.4).
	MySQL Dialect = "mysql"
)

// ParseDialect reads the value of relational.driver. Empty means Postgres. Anything else that is not a known name
// is an error, so a typo never silently selects a database. "postgresql", "pg" and "mariadb" are not accepted.
func ParseDialect(raw string) (Dialect, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(Postgres):
		return Postgres, nil
	case string(MySQL):
		return MySQL, nil
	default:
		return "", fmt.Errorf("unsupported relational.driver %q (want %q or %q)", raw, Postgres, MySQL)
	}
}

// Valid reports whether d is one of the two engines.
func (d Dialect) Valid() bool { return d == Postgres || d == MySQL }

func (d Dialect) String() string { return string(d) }

// DialectOf reports the engine behind db. It reads the GORM dialector, so a repository switches on the handle it was
// given rather than on a global. It returns an error for a handle opened with any other dialector.
func DialectOf(db *gorm.DB) (Dialect, error) {
	if db == nil || db.Dialector == nil {
		return "", fmt.Errorf("relational: nil database handle")
	}
	switch name := db.Dialector.Name(); name {
	case "postgres":
		return Postgres, nil
	case "mysql":
		return MySQL, nil
	default:
		return "", fmt.Errorf("relational: unsupported gorm dialector %q", name)
	}
}

// MustDialectOf is DialectOf for constructors that cannot proceed on an unknown engine; it panics, which is a
// programming error (the handle did not come from Open or Adopt).
func MustDialectOf(db *gorm.DB) Dialect {
	d, err := DialectOf(db)
	if err != nil {
		panic(err)
	}
	return d
}
