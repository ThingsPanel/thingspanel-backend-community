package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"project/internal/dal"
	"project/internal/storage"
	"project/internal/tdenginehistory"
)

const (
	historyBackendPostgres = "postgres"
	historyBackendTDengine = "tdengine"
)

// historyBackendSettings is read from storage.history_backend and storage.tdengine.*.
type historyBackendSettings struct {
	Backend      string
	ModeExplicit string // storage.history_mode as configured, if any
	URL          string
	User         string
	Password     string
	Database     string
	Experimental bool
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
	default:
		return nil, fmt.Errorf("unsupported storage.history_backend %q", s.Backend)
	}
	if !s.Experimental {
		return nil, errors.New("storage.history_backend=tdengine is incomplete (no aggregation, multi-device statistics or device-scope deletion); set storage.tdengine.experimental=true to accept that")
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
