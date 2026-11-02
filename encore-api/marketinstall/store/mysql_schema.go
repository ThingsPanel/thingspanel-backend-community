package store

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
)

type mysqlColumn struct{ name, kind, nullable, defaultValue string }
type mysqlIndex struct {
	unique  bool
	columns string
}
type mysqlShape struct {
	columns    []mysqlColumn
	indexes    map[string]mysqlIndex
	checks     map[string]string
	generated  map[string]string
	foreignKey bool
}

func mysqlShapes() map[string]mysqlShape {
	return map[string]mysqlShape{
		mysqlMigrationLedger: {
			columns: []mysqlColumn{{"version", "varbinary(64)", "NO", "<NULL>"}, {"checksum", "varbinary(64)", "NO", "<NULL>"}, {"state", "varbinary(16)", "NO", "<NULL>"}, {"started_at", "datetime(6)", "NO", "current_timestamp(6)"}, {"applied_at", "datetime(6)", "YES", "<NULL>"}},
			indexes: map[string]mysqlIndex{"PRIMARY": {true, "version"}},
			checks:  map[string]string{"market_install_migration_state": "state in ('applying','applied')"},
		},
		"market_install_operations": {
			columns: []mysqlColumn{
				{"id", "varbinary(1024)", "NO", "<NULL>"}, {"tenant_id", "varbinary(1024)", "NO", "<NULL>"}, {"subject_id", "varbinary(1024)", "NO", "<NULL>"}, {"authorization_ref", "text", "NO", "<NULL>"},
				{"request_key", "varbinary(128)", "NO", "<NULL>"}, {"request_digest", "varbinary(64)", "NO", "<NULL>"}, {"artifact_digest", "varbinary(71)", "NO", "<NULL>"}, {"plan", "longtext", "NO", "<NULL>"},
				{"state", "varbinary(16)", "NO", "<NULL>"}, {"generation", "bigint", "NO", "0"}, {"lease_until", "datetime(6)", "YES", "<NULL>"}, {"error_code", "varchar(80)", "NO", ""},
				{"bundle_key", "text", "NO", "<NULL>"}, {"bundle_version", "text", "NO", "<NULL>"}, {"created_at", "datetime(6)", "NO", "current_timestamp(6)"}, {"updated_at", "datetime(6)", "NO", "current_timestamp(6)"},
			},
			indexes: map[string]mysqlIndex{"PRIMARY": {true, "id"}, "market_install_tenant_key": {true, "tenant_id,request_key"}, "market_install_claim": {false, "state,lease_until,created_at"}},
			checks:  map[string]string{"market_install_operation_state": "state in ('queued','running','paused','completed','rejected')", "market_install_generation": "generation >= 0", "market_install_plan_json": "json_valid(plan)", "market_install_request_hash": "octet_length(request_digest) = 64", "market_install_artifact_hash": "octet_length(artifact_digest) = 71"},
		},
		"market_install_steps": {
			columns:    []mysqlColumn{{"operation_id", "varbinary(1024)", "NO", "<NULL>"}, {"ordinal", "int", "NO", "<NULL>"}, {"step_id", "varbinary(200)", "NO", "<NULL>"}, {"idempotency_key", "varbinary(64)", "NO", "<NULL>"}, {"payload_hash", "varbinary(64)", "NO", "<NULL>"}, {"state", "varbinary(16)", "NO", "<NULL>"}, {"receipt", "longtext", "YES", "<NULL>"}, {"updated_at", "datetime(6)", "NO", "current_timestamp(6)"}},
			indexes:    map[string]mysqlIndex{"PRIMARY": {true, "operation_id,ordinal"}, "market_install_operation_step": {true, "operation_id,step_id"}, "market_install_step_key": {true, "idempotency_key"}},
			checks:     map[string]string{"market_install_ordinal": "ordinal >= 0", "market_install_step_state": "state in ('pending','dispatched','confirmed','unknown','rejected')", "market_install_receipt_json": "receipt is null or json_valid(receipt)", "market_install_step_hash": "octet_length(payload_hash) = 64", "market_install_idempotency_hash": "octet_length(idempotency_key) = 64"},
			foreignKey: true,
		},
	}
}

// Recovery checks the database's actual metadata, not text searches in a SQL
// fixture. Missing/extra columns, weakened indexes, disabled/changed CHECKs,
// another engine, unexpected generated expressions, and foreign-owner references
// fail closed.
func verifyMySQLTable(ctx context.Context, conn *sql.Conn, database, table string) error {
	expected, known := mysqlShapes()[table]
	if !known {
		return fmt.Errorf("unknown A08 MySQL table")
	}
	return verifyMySQLTableShape(ctx, conn, database, table, expected)
}

func mysqlMigrationShape(name string) (mysqlShape, error) {
	shapes := mysqlShapes()
	switch name {
	case "0001_operations.sql":
		return shapes["market_install_operations"], nil
	case "0002_steps.sql":
		return shapes["market_install_steps"], nil
	case "0003_exact_plan.sql":
		shape := shapes["market_install_operations"]
		shape.columns = append(shape.columns, mysqlColumn{"plan_encoding", "varbinary(40)", "YES", "<NULL>"}, mysqlColumn{"plan_exact", "longtext", "YES", "<NULL>"})
		shape.checks["market_install_plan_encoding"] = "plan_encoding in ('json-number-lexeme-v1')"
		shape.checks["market_install_plan_exact_json"] = "plan_exact is null or json_valid(plan_exact)"
		return shape, nil
	case "0004_claim_ready.sql":
		shape, err := mysqlMigrationShape("0003_exact_plan.sql")
		if err != nil {
			return mysqlShape{}, err
		}
		shape.columns = append(shape.columns, mysqlColumn{"claim_ready_at", "datetime(6)", "YES", "<NULL>"})
		shape.generated = map[string]string{"claim_ready_at": "CASE state WHEN _binary'queued' THEN created_at WHEN _binary'running' THEN lease_until ELSE NULL END"}
		shape.indexes["market_install_ready"] = mysqlIndex{false, "claim_ready_at,id"}
		return shape, nil
	default:
		return mysqlShape{}, fmt.Errorf("unknown A08 MySQL migration schema")
	}
}

func verifyMySQLTableShape(ctx context.Context, conn *sql.Conn, database, table string, expected mysqlShape) error {
	var engine, collation, kind string
	if err := conn.QueryRowContext(ctx, `SELECT ENGINE,TABLE_COLLATION,TABLE_TYPE FROM information_schema.TABLES WHERE CAST(TABLE_SCHEMA AS BINARY)=CAST(? AS BINARY) AND CAST(TABLE_NAME AS BINARY)=CAST(? AS BINARY)`, database, table).Scan(&engine, &collation, &kind); err != nil {
		return err
	}
	if engine != "InnoDB" || collation != "utf8mb4_bin" || kind != "BASE TABLE" {
		return fmt.Errorf("A08 table %s has an unsupported engine, collation or type", table)
	}
	rows, err := conn.QueryContext(ctx, `SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COLUMN_DEFAULT,EXTRA,COALESCE(CHARACTER_SET_NAME,''),COALESCE(COLLATION_NAME,''),COALESCE(GENERATION_EXPRESSION,'') FROM information_schema.COLUMNS WHERE CAST(TABLE_SCHEMA AS BINARY)=CAST(? AS BINARY) AND CAST(TABLE_NAME AS BINARY)=CAST(? AS BINARY) ORDER BY ORDINAL_POSITION`, database, table)
	if err != nil {
		return err
	}
	var actual []mysqlColumn
	for rows.Next() {
		var column mysqlColumn
		var defaultValue sql.NullString
		var extra, charset, columnCollation, generated string
		if err := rows.Scan(&column.name, &column.kind, &column.nullable, &defaultValue, &extra, &charset, &columnCollation, &generated); err != nil {
			rows.Close()
			return err
		}
		column.defaultValue = "<NULL>"
		if defaultValue.Valid {
			column.defaultValue = strings.ToLower(defaultValue.String)
		}
		text := strings.Contains(column.kind, "text") || strings.HasPrefix(column.kind, "varchar(")
		attributesValid := (extra == "" || extra == "DEFAULT_GENERATED") && generated == ""
		if expression, known := expected.generated[column.name]; known {
			attributesValid = extra == "STORED GENERATED" && mysqlCheckSignature(expression) == mysqlCheckSignature(generated)
		}
		if !attributesValid || text && (charset != "utf8mb4" || columnCollation != "utf8mb4_bin") || !text && (charset != "" || columnCollation != "") {
			rows.Close()
			return fmt.Errorf("A08 table %s has unexpected column attributes", table)
		}
		actual = append(actual, column)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected.columns) {
		return fmt.Errorf("A08 table %s columns drifted", table)
	}
	if err := verifyMySQLIndexes(ctx, conn, database, table, expected.indexes); err != nil {
		return err
	}
	if err := verifyMySQLChecks(ctx, conn, database, table, expected.checks); err != nil {
		return err
	}
	return verifyMySQLForeignKey(ctx, conn, database, table, expected.foreignKey)
}

func verifyMySQLIndexes(ctx context.Context, conn *sql.Conn, database, table string, expected map[string]mysqlIndex) error {
	rows, err := conn.QueryContext(ctx, `SELECT INDEX_NAME,NON_UNIQUE,SEQ_IN_INDEX,COLUMN_NAME,SUB_PART,INDEX_TYPE,IS_VISIBLE,EXPRESSION,COLLATION FROM information_schema.STATISTICS WHERE CAST(TABLE_SCHEMA AS BINARY)=CAST(? AS BINARY) AND CAST(TABLE_NAME AS BINARY)=CAST(? AS BINARY) ORDER BY INDEX_NAME,SEQ_IN_INDEX`, database, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	actual := map[string]mysqlIndex{}
	for rows.Next() {
		var name, kind, visible, direction string
		var nonUnique, sequence int
		var column, expression sql.NullString
		var prefix sql.NullInt64
		if err := rows.Scan(&name, &nonUnique, &sequence, &column, &prefix, &kind, &visible, &expression, &direction); err != nil {
			return err
		}
		if !column.Valid || prefix.Valid || expression.Valid || kind != "BTREE" || visible != "YES" || direction != "A" || (nonUnique != 0 && nonUnique != 1) {
			return fmt.Errorf("A08 table %s index is partial, hidden or unsupported", table)
		}
		item, present := actual[name]
		if !present {
			if sequence != 1 {
				return fmt.Errorf("invalid A08 index sequence")
			}
			item = mysqlIndex{unique: nonUnique == 0}
		} else if item.unique != (nonUnique == 0) || sequence != strings.Count(item.columns, ",")+2 {
			return fmt.Errorf("invalid A08 index columns")
		}
		if item.columns != "" {
			item.columns += ","
		}
		item.columns += column.String
		actual[name] = item
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("A08 table %s indexes drifted", table)
	}
	return nil
}

// The known checks use only one function/comparison or an IN list (plus the
// receipt's null alternative). The generated readiness expression is one flat
// CASE with literal branches and column results. Parentheses added by the server
// can be removed for these fixed expressions. Literal bytes remain exact.
func mysqlCheckSignature(expression string) string {
	var out []byte
	trimIntroducer := func() {
		for _, prefix := range []string{"_utf8mb4", "_utf8mb3", "_utf8", "_binary", "_ascii"} {
			if strings.HasSuffix(string(out), prefix) {
				out = out[:len(out)-len(prefix)]
				break
			}
		}
	}
	for i := 0; i < len(expression); i++ {
		c := expression[i]
		// MySQL 8.4 INFORMATION_SCHEMA.CHECK_CONSTRAINTS escapes the quote
		// delimiters as backslash+quote (verified against the actual HEX value),
		// while SHOW CREATE TABLE uses ordinary quotes. Decode only this known
		// outer representation; preserve every literal byte between delimiters.
		if c == '\\' && i+1 < len(expression) && expression[i+1] == '\'' {
			trimIntroducer()
			out = append(out, '\'')
			i += 2
			closed := false
			for ; i < len(expression); i++ {
				if expression[i] == '\\' && i+1 < len(expression) && expression[i+1] == '\'' {
					out = append(out, '\'')
					i++
					closed = true
					break
				}
				out = append(out, expression[i])
			}
			if !closed {
				return "unsupported-mysql-check-literal:" + expression
			}
			continue
		}
		if c == '\'' {
			trimIntroducer()
			out = append(out, c)
			for i++; i < len(expression); i++ {
				c = expression[i]
				out = append(out, c)
				if c == '\\' && i+1 < len(expression) {
					i++
					out = append(out, expression[i])
					continue
				}
				if c == '\'' {
					if i+1 < len(expression) && expression[i+1] == '\'' {
						i++
						out = append(out, expression[i])
						continue
					}
					break
				}
			}
			continue
		}
		if c == '(' && strings.HasSuffix(string(out), "octet_length") {
			// MySQL prints the documented LENGTH synonym in I_S metadata.
			out = append(out[:len(out)-len("octet_length")], []byte("length")...)
		}
		switch c {
		case '`', '(', ')', ' ', '\t', '\r', '\n', '\v', '\f':
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

func verifyMySQLChecks(ctx context.Context, conn *sql.Conn, database, table string, expected map[string]string) error {
	rows, err := conn.QueryContext(ctx, `SELECT tc.CONSTRAINT_NAME,tc.ENFORCED,cc.CHECK_CLAUSE FROM information_schema.TABLE_CONSTRAINTS tc JOIN information_schema.CHECK_CONSTRAINTS cc ON CAST(cc.CONSTRAINT_SCHEMA AS BINARY)=CAST(tc.CONSTRAINT_SCHEMA AS BINARY) AND CAST(cc.CONSTRAINT_NAME AS BINARY)=CAST(tc.CONSTRAINT_NAME AS BINARY) WHERE CAST(tc.TABLE_SCHEMA AS BINARY)=CAST(? AS BINARY) AND CAST(tc.TABLE_NAME AS BINARY)=CAST(? AS BINARY) AND tc.CONSTRAINT_TYPE='CHECK'`, database, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var name, enforced, clause string
		if err := rows.Scan(&name, &enforced, &clause); err != nil {
			return err
		}
		expression, known := expected[name]
		if !known || seen[name] || enforced != "YES" || mysqlCheckSignature(expression) != mysqlCheckSignature(clause) {
			return fmt.Errorf("A08 table %s CHECK constraint drifted or is disabled: %s", table, name)
		}
		seen[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("A08 table %s CHECK constraints are incomplete", table)
	}
	return nil
}

func verifyMySQLForeignKey(ctx context.Context, conn *sql.Conn, database, table string, required bool) error {
	rows, err := conn.QueryContext(ctx, `SELECT k.CONSTRAINT_NAME,k.COLUMN_NAME,k.REFERENCED_TABLE_SCHEMA,k.REFERENCED_TABLE_NAME,k.REFERENCED_COLUMN_NAME,k.ORDINAL_POSITION,r.DELETE_RULE,r.UPDATE_RULE FROM information_schema.KEY_COLUMN_USAGE k JOIN information_schema.REFERENTIAL_CONSTRAINTS r ON CAST(r.CONSTRAINT_SCHEMA AS BINARY)=CAST(k.CONSTRAINT_SCHEMA AS BINARY) AND CAST(r.CONSTRAINT_NAME AS BINARY)=CAST(k.CONSTRAINT_NAME AS BINARY) WHERE CAST(k.TABLE_SCHEMA AS BINARY)=CAST(? AS BINARY) AND CAST(k.TABLE_NAME AS BINARY)=CAST(? AS BINARY) AND k.REFERENCED_TABLE_NAME IS NOT NULL`, database, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var name, column, schema, target, reference, onDelete, onUpdate string
		var ordinal int
		if err := rows.Scan(&name, &column, &schema, &target, &reference, &ordinal, &onDelete, &onUpdate); err != nil {
			return err
		}
		count++
		if !required || count != 1 || name != "market_install_step_operation" || column != "operation_id" || schema != database || target != "market_install_operations" || reference != "id" || ordinal != 1 || onDelete != "NO ACTION" && onDelete != "RESTRICT" || onUpdate != "NO ACTION" && onUpdate != "RESTRICT" {
			return fmt.Errorf("A08 table %s foreign-key ownership drifted", table)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if required && count != 1 {
		return fmt.Errorf("A08 step ownership foreign key is missing")
	}
	return nil
}
