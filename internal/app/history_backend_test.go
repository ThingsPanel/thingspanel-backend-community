package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"project/internal/dal"
	"project/internal/storage"
	"project/internal/tdenginehistory"
)

func validTDengine() historyBackendSettings {
	return historyBackendSettings{Backend: "tdengine", URL: "http://127.0.0.1:6041", User: "root", Password: "pw-8Zq1", Database: "tp_history", Experimental: true}
}

func TestHistoryBackendSelectionIsFailClosed(t *testing.T) {
	for name, mutate := range map[string]func(*historyBackendSettings){
		"unknown backend":      func(s *historyBackendSettings) { s.Backend = "influx" },
		"not acknowledged":     func(s *historyBackendSettings) { s.Experimental = false },
		"conflicting mode":     func(s *historyBackendSettings) { s.ModeExplicit = "postgres" },
		"missing url":          func(s *historyBackendSettings) { s.URL = "" },
		"non-http url":         func(s *historyBackendSettings) { s.URL = "ftp://host" },
		"credentials in url":   func(s *historyBackendSettings) { s.URL = "http://root:pw-8Zq1@host:6041" },
		"missing user":         func(s *historyBackendSettings) { s.User = " " },
		"injection in db name": func(s *historyBackendSettings) { s.Database = "x; DROP DATABASE y" },
		"empty db name":        func(s *historyBackendSettings) { s.Database = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s := validTDengine()
			mutate(&s)
			config := storage.DefaultConfig()
			prepare, err := selectHistoryBackend(s, &config)
			if err == nil || prepare != nil {
				t.Fatalf("must be rejected, got prepare=%v err=%v", prepare != nil, err)
			}
			if config.HistoryMode != "" || config.HistorySink != nil {
				t.Fatal("a rejected selection must not change the storage config")
			}
			if strings.Contains(err.Error(), "pw-8Zq1") {
				t.Fatalf("error leaks a credential: %v", err)
			}
		})
	}
}

func TestHistoryBackendDefaultsChangeNothing(t *testing.T) {
	for _, backend := range []string{"", "postgres"} {
		config := storage.DefaultConfig()
		prepare, err := selectHistoryBackend(historyBackendSettings{Backend: backend}, &config)
		if err != nil || prepare != nil || config.HistoryMode != "" || config.HistorySink != nil {
			t.Fatalf("backend %q: prepare=%v err=%v config=%+v", backend, prepare != nil, err, config)
		}
	}
}

func TestHistoryBackendTDengineSelectionAndPrepare(t *testing.T) {
	base := os.Getenv("TP_TDENGINE_TEST_URL")
	if base == "" {
		t.Skip("TP_TDENGINE_TEST_URL required")
	}
	restore := dal.SetHistoryBackend(dal.History)
	defer restore()
	s := validTDengine()
	s.URL, s.User, s.Password = base, "root", "taosdata"
	s.Database = "app_sel_" + strings.ReplaceAll(strings.ToLower(t.Name()[:3]), "-", "") + "_x"
	config := storage.DefaultConfig()
	prepare, err := selectHistoryBackend(s, &config)
	if err != nil {
		t.Fatal(err)
	}
	if config.HistoryMode != storage.HistoryModeIntent || config.HistorySink == nil {
		t.Fatalf("selection must switch to intent mode with a sink: %+v", config)
	}
	sink := config.HistorySink.(*storage.TDengineHistorySink)
	t.Cleanup(func() { _, _ = sink.Query(context.Background(), "DROP DATABASE IF EXISTS "+s.Database) })
	if _, ok := dal.History.(*tdenginehistory.Backend); ok {
		t.Fatal("selecting must not switch readers before prepare runs")
	}
	if err := prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := dal.History.(*tdenginehistory.Backend); !ok {
		t.Fatalf("prepare must install the TDengine reader, got %T", dal.History)
	}
	rows, err := sink.Query(context.Background(), "SHOW "+s.Database+".STABLES")
	if err != nil || len(rows) != 1 {
		t.Fatalf("prepare must create the super table: rows=%v err=%v", rows, err)
	}
	if err := prepare(context.Background()); err != nil {
		t.Fatalf("prepare must be repeatable: %v", err)
	}
	bad := s
	bad.URL = "http://127.0.0.1:1"
	badConfig := storage.DefaultConfig()
	badPrepare, err := selectHistoryBackend(bad, &badConfig)
	if err != nil {
		t.Fatal(err)
	}
	restoreAgain := dal.SetHistoryBackend(dal.History)
	defer restoreAgain()
	if err := badPrepare(context.Background()); err == nil {
		t.Fatal("an unreachable TDengine must fail start-up")
	}
}
