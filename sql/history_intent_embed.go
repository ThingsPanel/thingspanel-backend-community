package sql

import "embed"

//go:embed history_intent/001_intents.sql history_intent/002_deletions.sql
var historyIntentMigrationFiles embed.FS

func HistoryIntentMigrations() ([]NativeBindingMigration, error) {
	var migrations []NativeBindingMigration
	for _, name := range []string{"history_intent/001_intents.sql", "history_intent/002_deletions.sql"} {
		contents, err := historyIntentMigrationFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, NativeBindingMigration{FileName: name, Contents: contents})
	}
	return migrations, nil
}
