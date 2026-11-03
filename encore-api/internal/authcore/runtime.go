package authcore

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	mysqldriver "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Config struct {
	// DatabaseDriver is "postgres" (default) or "mysql". For mysql, DatabaseURL is a go-sql-driver DSN
	// that must set parseTime=true and clientFoundRows=true.
	DatabaseDriver    string           `json:"databaseDriver"`
	DatabaseURL       string           `json:"databaseUrl"`
	RedisURL          string           `json:"redisUrl"`
	SigningKey        string           `json:"signingKey"`
	SessionSeconds    int              `json:"sessionSeconds"`
	MaxFailures       int              `json:"maxFailures"`
	LockSeconds       int              `json:"lockSeconds"`
	RSAPrivateKeyFile string           `json:"rsaPrivateKeyFile"`
	PublicOrigin      string           `json:"publicOrigin"`
	Providers         []ProviderConfig `json:"providers"`
}
type Runtime struct {
	Core        *Service
	Providers   map[string]*Provider
	Origin      string
	Settings    *ProviderSettings
	Diagnostics []map[string]string
}

var runtimeOnce sync.Once
var runtimeInstance *Runtime
var runtimeError error

func Enabled() bool { return os.Getenv("TP_AUTH_MODE") == "native" }
func Load() (*Runtime, error) {
	if !privacyReady() {
		return nil, fmt.Errorf("auth_privacy_profile_invalid: %w", ErrUnavailable)
	}
	runtimeOnce.Do(func() { runtimeInstance, runtimeError = load() })
	return runtimeInstance, runtimeError
}
func load() (*Runtime, error) {
	if !Enabled() || os.Getenv("TP_AUTH_PRIVACY_READY") != "1" {
		return nil, ErrUnavailable
	}
	raw, e := os.ReadFile(os.Getenv("TP_AUTH_CONFIG_FILE"))
	if e != nil || len(raw) > 128<<10 {
		return nil, fmt.Errorf("auth_config_file_unavailable: %w", ErrUnavailable)
	}
	var c Config
	if json.Unmarshal(raw, &c) != nil {
		return nil, fmt.Errorf("auth_config_json_invalid: %w", ErrUnavailable)
	}
	if len(c.SigningKey) < 32 {
		return nil, fmt.Errorf("auth_signing_key_too_short: %w", ErrUnavailable)
	}
	if c.SessionSeconds <= 0 || c.SessionSeconds > 30*24*3600 || c.MaxFailures < 1 || c.LockSeconds < 1 || !validHTTPS(c.PublicOrigin) {
		return nil, fmt.Errorf("auth_session_or_origin_config_invalid: %w", ErrUnavailable)
	}
	origin, _ := url.Parse(c.PublicOrigin)
	if (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" {
		return nil, ErrUnavailable
	}
	c.PublicOrigin = strings.TrimRight(c.PublicOrigin, "/")
	db, dialect, e := openAccountsDB(c)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	opts, e := redis.ParseURL(c.RedisURL)
	if e != nil {
		db.Close()
		return nil, ErrUnavailable
	}
	opts.DialTimeout = 3 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second
	s := &Service{Accounts: &SQLAccounts{DB: db, Dialect: dialect}, Redis: redis.NewClient(opts), Key: []byte(c.SigningKey), TTL: time.Duration(c.SessionSeconds) * time.Second, MaxFailures: c.MaxFailures, LockDuration: time.Duration(c.LockSeconds) * time.Second}
	cleanup := func() { db.Close(); s.Redis.Close() }
	if c.RSAPrivateKeyFile != "" {
		b, e := os.ReadFile(c.RSAPrivateKeyFile)
		if e != nil {
			cleanup()
			return nil, ErrUnavailable
		}
		block, _ := pem.Decode(b)
		if block == nil {
			cleanup()
			return nil, ErrUnavailable
		}
		s.PrivateKey, e = x509.ParsePKCS1PrivateKey(block.Bytes)
		if e != nil {
			cleanup()
			return nil, ErrUnavailable
		}
	}
	r := &Runtime{Core: s, Providers: map[string]*Provider{}, Origin: c.PublicOrigin}
	r.Providers, r.Diagnostics = configuredProviders(c.Providers, c.PublicOrigin)
	if path := os.Getenv("TP_AUTH_PROVIDER_KEY_FILE"); path != "" {
		key, err := os.ReadFile(path)
		if err == nil {
			key, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(key)))
		}
		if err == nil && len(key) == 32 {
			r.Settings = &ProviderSettings{DB: db, Dialect: dialect, Key: key, Origin: r.Origin}
			if len(r.Providers) > 0 {
				r.Diagnostics = append(r.Diagnostics, map[string]string{"id": "settings", "code": "file_providers_require_import"})
				r.Providers = map[string]*Provider{}
			}
		} else {
			r.Providers = map[string]*Provider{}
			r.Diagnostics = append(r.Diagnostics, map[string]string{"id": "settings", "code": "provider_key_invalid"})
		}
	}
	return r, nil
}

func configuredProviders(configs []ProviderConfig, origin string) (map[string]*Provider, []map[string]string) {
	out := map[string]*Provider{}
	diagnostics := []map[string]string{}
	counts := map[string]int{}
	for _, c := range configs {
		if c.Enabled {
			counts[c.ID]++
		}
	}
	for _, c := range configs {
		if !c.Enabled {
			continue
		}
		if counts[c.ID] != 1 || !providerID.MatchString(c.ID) || (c.Kind != "wechat-mini" && c.RedirectURL != origin+"/api/v1/auth/external/callback") {
			diagnostics = append(diagnostics, map[string]string{"id": c.ID, "code": "provider_configuration_invalid"})
			continue
		}
		p, e := NewProvider(c)
		if e != nil {
			diagnostics = append(diagnostics, map[string]string{"id": c.ID, "code": "provider_configuration_invalid"})
			continue
		}
		out[c.ID] = p
	}
	return out, diagnostics
}

func (r *Runtime) providerSnapshot(ctx context.Context) (*Runtime, error) {
	if r.Settings == nil {
		return r, nil
	}
	providers, e := r.Settings.Resolve(ctx, r.Providers)
	if e != nil {
		return nil, e
	}
	copy := *r
	copy.Providers = providers
	return &copy, nil
}

func DisabledLoginOptions() map[string]any {
	return map[string]any{"password": map[string]bool{"enabled": true}, "external": map[string]any{"enabled": false, "providers": []map[string]string{}}}
}

// openAccountsDB opens the account database for the configured driver. A MySQL DSN is refused unless it
// sets the options the account queries rely on: parseTime (DATETIME columns scan into time.Time) and
// clientFoundRows (RowsAffected means "rows matched", as in PostgreSQL).
func openAccountsDB(c Config) (*sql.DB, Dialect, error) {
	switch c.DatabaseDriver {
	case "", "postgres":
		db, e := sql.Open("pgx", c.DatabaseURL)
		if e != nil {
			return nil, DialectPostgres, ErrUnavailable
		}
		return db, DialectPostgres, nil
	case "mysql":
		cfg, e := mysqldriver.ParseDSN(c.DatabaseURL)
		if e != nil {
			return nil, DialectMySQL, fmt.Errorf("auth_database_dsn_invalid: %w", ErrUnavailable)
		}
		if !cfg.ParseTime || !cfg.ClientFoundRows {
			return nil, DialectMySQL, fmt.Errorf("auth_database_dsn_needs_parseTime_and_clientFoundRows: %w", ErrUnavailable)
		}
		db, e := sql.Open("mysql", cfg.FormatDSN())
		if e != nil {
			return nil, DialectMySQL, ErrUnavailable
		}
		return db, DialectMySQL, nil
	default:
		return nil, DialectPostgres, fmt.Errorf("auth_database_driver_unsupported: %w", ErrUnavailable)
	}
}
