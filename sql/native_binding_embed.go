package sql

import (
	"embed"
	"fmt"
)

// NativeBindingMigration is a checked-in SQL migration owned by the native
// binding component, independently of sys_version.
type NativeBindingMigration struct {
	FileName string
	Contents []byte
}

//go:embed 26.sql 27.sql
var nativeBindingMigrationFiles embed.FS

// NativeBindingMigrations returns the component migrations in execution order.
func NativeBindingMigrations() ([]NativeBindingMigration, error) {
	fileNames := []string{"26.sql", "27.sql"}
	migrations := make([]NativeBindingMigration, 0, len(fileNames))
	for _, fileName := range fileNames {
		contents, err := nativeBindingMigrationFiles.ReadFile(fileName)
		if err != nil {
			return nil, fmt.Errorf("read embedded native binding migration %s: %w", fileName, err)
		}
		migrations = append(migrations, NativeBindingMigration{FileName: fileName, Contents: contents})
	}
	return migrations, nil
}
