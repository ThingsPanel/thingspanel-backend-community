package authcore

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// Opt-in developer-tool integration fixture. Real WeChat proof, isolated local
// account/session state. Never loads a production account or logs credentials.
func TestWeChatLiveHTTPFixture(t *testing.T) {
	if os.Getenv("TP_AUTH_WECHAT_LIVE") != "1" {
		t.Skip("explicit real WeChat integration only")
	}
	raw, e := os.ReadFile(os.Getenv("TP_AUTH_WECHAT_CONFIG_FILE"))
	if e != nil {
		t.Fatal("private configuration unavailable")
	}
	var cfg ProviderConfig
	if json.Unmarshal(raw, &cfg) != nil || cfg.AppID == "" || cfg.AppSecret == "" {
		t.Fatal("incomplete private configuration")
	}
	cfg.ID = "wechat-live"
	cfg.Label = "WeChat live fixture"
	cfg.Kind = "wechat-mini"
	cfg.TenantIDs = []string{"tenant-1"}
	cfg.ReviewID = "isolated-real-wechat-20261023"
	provider, e := NewProvider(cfg)
	if e != nil {
		t.Fatal("provider configuration invalid")
	}
	s, _, _ := fixture(t)
	handler := &HTTP{Runtime: &Runtime{Core: s, Providers: map[string]*Provider{cfg.ID: provider}, Origin: "https://localhost"}}
	done := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/_qa/ready", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("/_qa/complete", func(w http.ResponseWriter, r *http.Request) {
		select {
		case done <- struct{}{}:
		default:
		}
		w.WriteHeader(204)
	})
	mux.Handle("/", handler)
	listener, e := net.Listen("tcp", ":24181")
	if e != nil {
		t.Fatal("fixture listener unavailable")
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	t.Log("real-provider fixture ready; account and Redis state are isolated")
	select {
	case <-done:
	case <-time.After(180 * time.Second):
		t.Fatal("developer tool client did not complete")
	}
}
