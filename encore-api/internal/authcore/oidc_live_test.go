package authcore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

// Opt-in HTTP fixture for a real locally deployed OIDC provider. Uses the actual
// SQLAccounts implementation in a random schema and isolated Redis. No production
// user, role, tenant, account database or Encore runtime is changed by this test.
func TestOIDCLiveHTTPFixture(t *testing.T) {
	if os.Getenv("TP_AUTH_OIDC_LIVE") != "1" {
		t.Skip("explicit local identity-provider integration only")
	}
	cfg, e := pgx.ParseConfig(os.Getenv("TP_AUTH_TEST_DSN"))
	if e != nil || cfg.Database != "notification_test" {
		t.Fatal("requires isolated notification_test database")
	}
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	schema := "auth_oidc_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec("CREATE SCHEMA " + schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	_, e = db.Exec(`CREATE TABLE users(id varchar(36) PRIMARY KEY,email text,name text,tenant_id text,authority text,status text,password text,phone_number text DEFAULT '',remark text DEFAULT '',additional_info jsonb DEFAULT '{}',organization text DEFAULT '',timezone text DEFAULT 'Asia/Shanghai',default_language text DEFAULT 'zh-CN',created_at timestamptz DEFAULT now(),updated_at timestamptz DEFAULT now(),password_last_updated timestamptz DEFAULT now(),last_visit_time timestamptz DEFAULT now(),last_visit_ip text DEFAULT '',last_visit_device text DEFAULT '',password_fail_count int DEFAULT 0,avatar_url text DEFAULT '');
 CREATE TABLE app_push_devices(id text,user_id text,active boolean);
 CREATE TABLE sys_function(name text,enable_flag text);
 CREATE TABLE casbin_rule(ptype text,v0 text,v1 text);
 CREATE TABLE user_address(user_id text);
 INSERT INTO sys_function VALUES('shared_account','enable');`)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"001_identity.sql", "002_self_unlink.sql"} {
		raw, e := os.ReadFile("../../../sql/auth/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	password, e := bcrypt.GenerateFromPassword([]byte("Local-platform-2026!"), bcrypt.MinCost)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`INSERT INTO users(id,email,name,tenant_id,authority,status,password) VALUES('local-user','local@example.invalid','Local authentication tester','local-tenant','TENANT_ADMIN','N',$1),('setup-sentinel','sentinel@example.invalid','Setup complete','','SYS_ADMIN','F','!no-login')`, string(password))
	if e != nil {
		t.Fatal(e)
	}
	s, _, _ := fixture(t)
	s.Accounts = &SQLAccounts{DB: db}
	raw, e := os.ReadFile(os.Getenv("TP_AUTH_OIDC_CONFIG_FILE"))
	if e != nil {
		t.Fatal("missing local configuration")
	}
	var pc ProviderConfig
	if json.Unmarshal(raw, &pc) != nil {
		t.Fatal("invalid local configuration")
	}
	p, e := NewProvider(pc)
	if e != nil {
		t.Fatal("invalid local provider")
	}
	ca, e := os.ReadFile(os.Getenv("TP_AUTH_OIDC_CA_FILE"))
	if e != nil {
		t.Fatal(e)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		t.Fatal("invalid fixture CA")
	}
	// The browser's localhost is the Windows host. Map only the fixture's TLS
	// listener in Docker; keep certificate hostname verification fully enabled.
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "localhost:24443" {
			address = "host.docker.internal:24443"
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
	}}
	p.HTTP = &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	p.Verifier = oidc.NewVerifier(pc.Issuer, oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), p.HTTP), pc.JWKSURL), &oidc.Config{ClientID: pc.ClientID, SupportedSigningAlgs: []string{"RS256"}})
	handler := &HTTP{Runtime: &Runtime{Core: s, Providers: map[string]*Provider{pc.ID: p}, Origin: "https://localhost:24444"}}
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
	server := &http.Server{Addr: ":24445", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	listener, e := net.Listen("tcp", server.Addr)
	if e != nil {
		t.Fatal(e)
	}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	t.Log("local Casdoor HTTP fixture ready; SQL schema and sessions isolated")
	select {
	case <-done:
	case <-time.After(90 * time.Minute):
		t.Fatal("browser acceptance did not complete")
	}
}
