package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"project/internal/dal"
	"project/internal/iotdbhistory"
	"project/internal/storage"
	"project/internal/tdenginehistory"
)

const (
	historyBackendPostgres = "postgres"
	historyBackendTDengine = "tdengine"
	historyBackendIoTDB    = "iotdb"
)

// historyBackendSettings is read from storage.history_backend and the selected backend's settings.
type historyBackendSettings struct {
	Backend       string
	ModeExplicit  string // storage.history_mode as configured, if any
	URL           string
	User          string
	Password      string
	Database      string
	IoTDBURL      string
	IoTDBUser     string
	IoTDBPassword string
	IoTDBDatabase string
}

// selectHistoryBackend validates the settings and, for a non-default backend, adjusts the
// storage config. The returned prepare function must run before the storage service starts.
// Any invalid or inconsistent combination is an error: the process must not fall back to
// another store on its own.
func selectHistoryBackend(s historyBackendSettings, config *storage.Config) (prepare func(context.Context) error, err error) {
	switch s.Backend {
	case "", historyBackendPostgres:
		return nil, nil
	case historyBackendTDengine:
	case historyBackendIoTDB:
		return selectIoTDBHistoryBackend(s, config)
	default:
		return nil, fmt.Errorf("unsupported storage.history_backend %q", s.Backend)
	}
	if s.ModeExplicit != "" && s.ModeExplicit != storage.HistoryModeIntent {
		return nil, fmt.Errorf("storage.history_mode=%q conflicts with storage.history_backend=tdengine", s.ModeExplicit)
	}
	parsed, err := url.Parse(s.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("storage.tdengine.url must be an http(s) URL without credentials")
	}
	if strings.TrimSpace(s.User) == "" {
		return nil, errors.New("storage.tdengine.user is required")
	}
	if !storage.ValidTDengineDatabaseName(s.Database) {
		return nil, errors.New("storage.tdengine.database must be a plain identifier")
	}
	sink := &storage.TDengineHistorySink{BaseURL: strings.TrimRight(s.URL, "/"), User: s.User, Password: s.Password, Database: s.Database}
	config.HistoryMode = storage.HistoryModeIntent
	config.HistorySink = sink
	return func(ctx context.Context) error {
		if err := sink.EnsureSchema(ctx); err != nil {
			return fmt.Errorf("prepare tdengine history: %w", err)
		}
		dal.SetHistoryBackend(&tdenginehistory.Backend{Sink: sink})
		return nil
	}, nil
}

func selectIoTDBHistoryBackend(s historyBackendSettings, config *storage.Config) (func(context.Context) error, error) {
	if s.ModeExplicit != "" && s.ModeExplicit != storage.HistoryModeIntent {
		return nil, fmt.Errorf("storage.history_mode=%q conflicts with storage.history_backend=iotdb", s.ModeExplicit)
	}
	parsed, err := url.Parse(s.IoTDBURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("storage.iotdb.url must be an http(s) URL without credentials")
	}
	if strings.TrimSpace(s.IoTDBUser) == "" {
		return nil, errors.New("storage.iotdb.user is required")
	}
	if !storage.ValidIoTDBDatabaseName(s.IoTDBDatabase) {
		return nil, errors.New("storage.iotdb.database must be a plain identifier")
	}
	sink := &storage.IoTDBHistorySink{BaseURL: strings.TrimRight(s.IoTDBURL, "/"), User: s.IoTDBUser, Password: s.IoTDBPassword, Database: s.IoTDBDatabase}
	config.HistoryMode = storage.HistoryModeIntent
	config.HistorySink = sink
	return func(ctx context.Context) error {
		if err := sink.EnsureSchema(ctx); err != nil {
			return fmt.Errorf("prepare iotdb history: %w", err)
		}
		dal.SetHistoryBackend(&iotdbhistory.Backend{Sink: sink})
		return nil
	}, nil
}
