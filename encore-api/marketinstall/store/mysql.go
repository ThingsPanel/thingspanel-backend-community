package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// OpenMySQL owns a dedicated connection pool for the existing A08 owner. It
// does not run migrations or select a different business owner. The trusted
// runtime supplies a DSN selected by the deployment's relational profile.
// Credentials and the DSN must never be placed in logs or public API errors.
func OpenMySQL(ctx context.Context, dsn string) (*Store, error) {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid installation MySQL connection configuration")
	}
	config, err = mysqlConfiguration(config)
	if err != nil {
		return nil, err
	}
	connector, err := mysql.NewConnector(config)
	if err != nil {
		return nil, fmt.Errorf("invalid installation MySQL connector configuration")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(4 * time.Minute)
	if err := validateMySQL(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db, dialect: MySQL}, nil
}

func mysqlConfiguration(original *mysql.Config) (*mysql.Config, error) {
	if original == nil || original.DBName == "" {
		return nil, fmt.Errorf("installation MySQL database must be explicit")
	}
	config := original.Clone()
	// Every new pooled connection gets the same behavior. In particular a
	// repeated dispatch must count its matching row even at the same timestamp.
	config.ClientFoundRows = true
	config.ParseTime = true
	config.Loc = time.UTC
	// v1.7.0's protocol handshake only represents collation IDs <=255.
	// Opaque identities use VARBINARY in the owner schema, independently of
	// connection collation, including significant case and trailing spaces.
	config.Collation = "utf8mb4_bin"
	config.MultiStatements = false
	config.InterpolateParams = false
	config.AllowAllFiles = false
	config.AllowCleartextPasswords = false
	config.AllowOldPasswords = false
	config.RejectReadOnly = true
	if config.Params == nil {
		config.Params = map[string]string{}
	}
	config.Params["charset"] = "utf8mb4"
	config.Params["character_set_client"] = "'utf8mb4'"
	config.Params["character_set_connection"] = "'utf8mb4'"
	config.Params["character_set_results"] = "'utf8mb4'"
	config.Params["collation_connection"] = "'utf8mb4_bin'"
	config.Params["time_zone"] = "'+00:00'"
	config.Params["autocommit"] = "1"
	config.Params["foreign_key_checks"] = "1"
	config.Params["unique_checks"] = "1"
	config.Params["sql_mode"] = "'STRICT_ALL_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION'"
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 30 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 30 * time.Second
	}
	return config, nil
}

func validateMySQL(ctx context.Context, db *sql.DB) error {
	var version, database, zone, mode string
	var autocommit, foreignKeys, uniqueKeys int
	if err := db.QueryRowContext(ctx, `SELECT VERSION(),DATABASE(),@@SESSION.time_zone,@@SESSION.sql_mode,@@SESSION.autocommit,@@SESSION.foreign_key_checks,@@SESSION.unique_checks`).Scan(&version, &database, &zone, &mode, &autocommit, &foreignKeys, &uniqueKeys); err != nil {
		return fmt.Errorf("installation MySQL capability check failed: %w", err)
	}
	if !supportedMySQLVersion(version) || database == "" || zone != "+00:00" || !strings.Contains(","+mode+",", ",STRICT_ALL_TABLES,") || autocommit != 1 || foreignKeys != 1 || uniqueKeys != 1 {
		return fmt.Errorf("installation ledger requires MySQL 8.0.16+ in the 8.x series, strict mode and UTC")
	}
	return nil
}

func supportedMySQLVersion(version string) bool {
	if strings.Contains(strings.ToLower(version), "mariadb") {
		return false
	}
	parts := strings.SplitN(strings.SplitN(version, "-", 2)[0], ".", 4)
	if len(parts) != 3 {
		return false
	}
	major, e1 := strconv.Atoi(parts[0])
	minor, e2 := strconv.Atoi(parts[1])
	patch, e3 := strconv.Atoi(parts[2])
	return e1 == nil && e2 == nil && e3 == nil && major == 8 && minor >= 0 && patch >= 0 && (minor > 0 || patch >= 16)
}

func (s *Store) isDuplicate(err error) bool {
	if s.dialect != MySQL {
		return false
	}
	var duplicate *mysql.MySQLError
	return errors.As(err, &duplicate) && duplicate.Number == 1062
}
