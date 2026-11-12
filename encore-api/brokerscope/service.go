// Package brokerscope is the control-plane endpoint through which the host-side manager (tp-agent system role)
// learns which devices the Broker must know about. The manager never needs database credentials.
//
// The endpoint is public to Encore's gateway but guarded by a bearer token (GOTP_BROKER_SCOPE_TOKEN; unset = 404).
// It must NOT be routed by the public frontend proxy: nginx only forwards the explicit API prefixes.
//
// Devices still live in the legacy platform database, so the service opens the shared logical resource "platform"
// of pkg/relational (config keys relational.<driver>.platform.*, env TP_RELATIONAL_<DRIVER>_PLATFORM_*) with GORM,
// read-only, lazily on first request. No call goes through the old Gin process.
package brokerscope

import (
	"context"
	"net/http"
	"os"
	"sync"

	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"

	scope "encore.app/internal/brokerscope"
)

const resource = "platform"

// Service owns the lazily opened platform handle.
//
//encore:service
type Service struct {
	mu sync.Mutex
	db *gorm.DB
}

func initService() (*Service, error) { return &Service{}, nil }

// Shutdown closes the pool this service opened.
func (s *Service) Shutdown(force context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		_ = relational.CloseDB(s.db)
		s.db = nil
	}
}

func (s *Service) open() (*gorm.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db, nil
	}
	target, err := relational.ResolveTarget(relational.EnvSource(), resource)
	if err != nil {
		return nil, err
	}
	db, err := relational.Open(context.Background(), target)
	if err != nil {
		return nil, err
	}
	s.db = db
	return db, nil
}

// Scope serves GET /internal/broker-scope.
//
//encore:api public raw method=GET path=/internal/broker-scope
func (s *Service) Scope(w http.ResponseWriter, req *http.Request) {
	scope.Handler(func() string { return os.Getenv(scope.TokenEnv) }, s.open)(w, req)
}
