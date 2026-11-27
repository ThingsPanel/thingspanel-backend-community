package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/dal"
	"project/internal/iotdbhistory"
	"project/internal/storage"
)

func validIoTDB() historyBackendSettings {
	return historyBackendSettings{
		Backend: historyBackendIoTDB, IoTDBURL: "http://127.0.0.1:18080",
		IoTDBUser: "root", IoTDBPassword: "secret", IoTDBDatabase: "tp_history",
	}
}

func TestHistoryBackendIoTDBSelectionIsFailClosed(t *testing.T) {
	for name, mutate := range map[string]func(*historyBackendSettings){
		"conflicting mode":   func(s *historyBackendSettings) { s.ModeExplicit = "postgres" },
		"missing url":        func(s *historyBackendSettings) { s.IoTDBURL = "" },
		"non-http url":       func(s *historyBackendSettings) { s.IoTDBURL = "ftp://host" },
		"credentials in url": func(s *historyBackendSettings) { s.IoTDBURL = "http://root:secret@host:18080" },
		"missing user":       func(s *historyBackendSettings) { s.IoTDBUser = " " },
		"invalid database":   func(s *historyBackendSettings) { s.IoTDBDatabase = "x; DROP DATABASE y" },
		"empty database":     func(s *historyBackendSettings) { s.IoTDBDatabase = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s := validIoTDB()
			mutate(&s)
			config := storage.DefaultConfig()
			prepare, err := selectHistoryBackend(s, &config)
			if err == nil || prepare != nil {
				t.Fatalf("must be rejected, got prepare=%v err=%v", prepare != nil, err)
			}
			if config.HistoryMode != "" || config.HistorySink != nil {
				t.Fatal("rejected IoTDB settings changed storage config")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaks a credential: %v", err)
			}
		})
	}
}

func TestHistoryBackendIoTDBSelectionAndPrepare(t *testing.T) {
	var seen []iotdbRequestForTest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request iotdbRequestForTest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		seen = append(seen, request)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"message":"SUCCESS_STATUS"}`))
	}))
	defer server.Close()

	restore := dal.SetHistoryBackend(dal.History)
	defer restore()
	s := validIoTDB()
	s.IoTDBURL = server.URL
	config := storage.DefaultConfig()
	prepare, err := selectHistoryBackend(s, &config)
	if err != nil || prepare == nil {
		t.Fatalf("prepare=%v err=%v", prepare != nil, err)
	}
	if config.HistoryMode != storage.HistoryModeIntent {
		t.Fatalf("history mode = %q", config.HistoryMode)
	}
	if _, ok := config.HistorySink.(*storage.IoTDBHistorySink); !ok {
		t.Fatalf("history sink type = %T", config.HistorySink)
	}
	if _, ok := dal.History.(*iotdbhistory.Backend); ok {
		t.Fatal("selection must not install the reader before prepare")
	}
	if err := prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := dal.History.(*iotdbhistory.Backend); !ok {
		t.Fatalf("prepare installed %T", dal.History)
	}
	if len(seen) != 2 || seen[0].Database != "" || seen[1].Database != s.IoTDBDatabase {
		t.Fatalf("schema requests = %+v", seen)
	}
}

type iotdbRequestForTest struct {
	Database string `json:"database"`
	SQL      string `json:"sql"`
}
