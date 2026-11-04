package initialize

import (
	"time"
)

const (
	mysqlLedgerTable      = "native_component_migration_ledger"
	mysqlGlobalLockName   = "tp:schema-migrations"
	mysqlCoreModule       = "mysql-core"
	mysqlAuthModule       = "mysql-auth"
	defaultMySQLLockWait  = 60 * time.Second
	mysqlLedgerCreateStmt = "CREATE TABLE IF NOT EXISTS " + mysqlLedgerTable + ` (
		module_name VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
		file_name VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
		checksum CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		PRIMARY KEY (module_name, file_name)
	) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`
)
