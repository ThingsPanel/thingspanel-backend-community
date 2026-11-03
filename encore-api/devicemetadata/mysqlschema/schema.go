// Package mysqlschema holds the MySQL form of the device_metadata schema. It is generated from the final
// PostgreSQL schema (scripts/pg2mysql_platform.py --standalone, run on a database built by ../migrations) and lives
// in its own package so tests can use it without the Encore runtime that package devicemetadata needs.
package mysqlschema

import (
	"embed"
	"strings"
)

//go:embed 001_schema.sql 002_guards.sql
var files embed.FS

// Statements returns the statements of the schema in order. They are separated by lines holding only "-- @@".
// Some use session variables (the guarded foreign keys), so they must run on one connection.
func Statements() []string {
	var out []string
	for _, name := range []string{"001_schema.sql", "002_guards.sql"} {
		raw, err := files.ReadFile(name)
		if err != nil {
			panic(err)
		}
		out = append(out, split(string(raw))...)
	}
	return out
}

func split(schema string) []string {
	var out []string
	for _, part := range strings.Split(strings.ReplaceAll(schema, "\r\n", "\n"), "\n-- @@\n") {
		var kept []string
		for _, line := range strings.Split(part, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			kept = append(kept, line)
		}
		if statement := strings.TrimSpace(strings.Join(kept, "\n")); statement != "" {
			out = append(out, statement)
		}
	}
	return out
}
