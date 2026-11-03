package sql

import "embed"

//go:embed history_intent/001_intents.sql
var historyIntentMigrationFiles embed.FS

func HistoryIntentMigrations() ([]NativeBindingMigration, error) {
	const name = "history_intent/001_intents.sql"
	contents, err := historyIntentMigrationFiles.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return []NativeBindingMigration{{FileName: name, Contents: contents}}, nil
}
