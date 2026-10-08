package test

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"project/initialize"
)

func databaseTestConfig(dsn string) (*initialize.DbConfig, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, fmt.Errorf("TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("TEST_DATABASE_URL must use a loopback host")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("TEST_DATABASE_URL must include a valid port")
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("TEST_DATABASE_URL must include a username")
	}
	password, hasPassword := u.User.Password()
	if !hasPassword || password == "" {
		return nil, fmt.Errorf("TEST_DATABASE_URL must include a password")
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if strings.Contains(dbName, "/") || !strings.HasPrefix(dbName, "thingspanel_test_") {
		return nil, fmt.Errorf("TEST_DATABASE_URL database name must start with thingspanel_test_")
	}
	return &initialize.DbConfig{
		Host: host, Port: port, DbName: dbName, Username: u.User.Username(),
		Password: password, TimeZone: "Asia/Shanghai", LogLevel: 1,
	}, nil
}

func TestDatabaseTestConfig(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{name: "isolated local database", dsn: "postgres://tester:secret@127.0.0.1:5432/thingspanel_test_local"},
		{name: "missing URL", wantErr: true},
		{name: "remote host", dsn: "postgres://tester:secret@example.com:5432/thingspanel_test_ci", wantErr: true},
		{name: "non-test database", dsn: "postgres://tester:secret@127.0.0.1:5432/ThingsPanel", wantErr: true},
		{name: "missing password", dsn: "postgres://tester@127.0.0.1:5432/thingspanel_test_local", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := databaseTestConfig(tt.dsn)
			if (err != nil) != tt.wantErr {
				t.Fatalf("databaseTestConfig() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err == nil && (cfg.DbName != "thingspanel_test_local" || cfg.Host != "127.0.0.1" || cfg.Port != 5432) {
				t.Fatalf("unexpected database config: host=%q port=%d db=%q", cfg.Host, cfg.Port, cfg.DbName)
			}
		})
	}
}
