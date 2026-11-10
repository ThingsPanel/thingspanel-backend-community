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

// Flavor identifies the concrete server family selected behind the MySQL
// wire protocol. MariaDB uses GORM's MySQL dialector, so Dialect remains MySQL
// while Flavor carries the migration and capability differences.
type Flavor string

const (
	FlavorMySQL   Flavor = "mysql"
	FlavorMariaDB Flavor = "mariadb"
)

// ParseDialect reads the value of relational.driver. Empty means Postgres. MariaDB is
// selected as a flavor of the mysql driver, not as a separate ORM dialect.
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

// ParseFlavor reads relational.mysql.flavor. Empty means MySQL for backwards
// compatibility. MariaDB is deliberately an explicit opt-in.
func ParseFlavor(raw string) (Flavor, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(FlavorMySQL):
		return FlavorMySQL, nil
	case string(FlavorMariaDB):
		return FlavorMariaDB, nil
	default:
		return "", fmt.Errorf("unsupported relational.mysql.flavor %q (want %q or %q)", raw, FlavorMySQL, FlavorMariaDB)
	}
}

// Valid reports whether d is one of the two engines.
func (d Dialect) Valid() bool { return d == Postgres || d == MySQL }

func (f Flavor) Valid() bool { return f == FlavorMySQL || f == FlavorMariaDB }

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

// FlavorOf reports the explicitly configured MySQL-family server flavor.
// Handles opened through legacy Adopt calls are MySQL by default.
func FlavorOf(db *gorm.DB) (Flavor, error) {
	if db == nil || db.Dialector == nil {
		return "", fmt.Errorf("relational: nil database handle")
	}
	if db.Dialector.Name() != "mysql" {
		return "", fmt.Errorf("relational: flavor is only defined for mysql-family dialectors")
	}
	if d, ok := db.Dialector.(interface{ RelationalFlavor() Flavor }); ok {
		return d.RelationalFlavor(), nil
	}
	return FlavorMySQL, nil
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
