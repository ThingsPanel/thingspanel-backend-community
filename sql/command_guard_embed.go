package sql

import (
	"embed"
	"fmt"
)

// CommandGuardMigration is the SQL asset owned by the automation command guard,
// checked in independently of sys_version so it can be applied and verified on
// every startup.
type CommandGuardMigration struct {
	FileName string
	Contents []byte
}

//go:embed 28.sql
var commandGuardMigrationFiles embed.FS

// CommandGuardMigrations returns the guard migrations in execution order.
func CommandGuardMigrations() ([]CommandGuardMigration, error) {
	fileNames := []string{"28.sql"}
	migrations := make([]CommandGuardMigration, 0, len(fileNames))
	for _, fileName := range fileNames {
		contents, err := commandGuardMigrationFiles.ReadFile(fileName)
		if err != nil {
			return nil, fmt.Errorf("read embedded command guard migration %s: %w", fileName, err)
		}
		migrations = append(migrations, CommandGuardMigration{FileName: fileName, Contents: contents})
	}
	return migrations, nil
}
