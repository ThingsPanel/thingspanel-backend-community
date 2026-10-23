package authcore

import (
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Config struct {
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
	Core      *Service
	Providers map[string]*Provider
	Origin    string
}

var runtimeOnce sync.Once
var runtimeInstance *Runtime
var runtimeError error

func Enabled() bool { return os.Getenv("TP_AUTH_MODE") == "native" }
func Load() (*Runtime, error) {
	if !privacyReady() {
		return nil, ErrUnavailable
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
		return nil, ErrUnavailable
	}
	var c Config
	if json.Unmarshal(raw, &c) != nil || len(c.SigningKey) < 32 || c.SessionSeconds <= 0 || c.SessionSeconds > 30*24*3600 || c.MaxFailures < 1 || c.LockSeconds < 1 || !validHTTPS(c.PublicOrigin) {
		return nil, ErrUnavailable
	}
	origin, _ := url.Parse(c.PublicOrigin)
	if (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" {
		return nil, ErrUnavailable
	}
	c.PublicOrigin = strings.TrimRight(c.PublicOrigin, "/")
	db, e := sql.Open("pgx", c.DatabaseURL)
	if e != nil {
		return nil, ErrUnavailable
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
	s := &Service{Accounts: &SQLAccounts{DB: db}, Redis: redis.NewClient(opts), Key: []byte(c.SigningKey), TTL: time.Duration(c.SessionSeconds) * time.Second, MaxFailures: c.MaxFailures, LockDuration: time.Duration(c.LockSeconds) * time.Second}
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
	for _, cfg := range c.Providers {
		if cfg.Kind != "wechat-mini" && cfg.RedirectURL != c.PublicOrigin+"/api/v1/auth/external/callback" {
			cleanup()
			return nil, ErrUnavailable
		}
		p, e := NewProvider(cfg)
		if e != nil || r.Providers[cfg.ID] != nil {
			cleanup()
			return nil, ErrUnavailable
		}
		r.Providers[cfg.ID] = p
	}
	return r, nil
}
