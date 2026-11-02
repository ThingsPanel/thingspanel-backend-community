// Package marketinstall is the sole Encore A08 installation host. It owns a
// PostgreSQL intent ledger, while business resources stay with their providers.
package marketinstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"encore.app/l1-access/authn"
	"encore.app/marketinstall/app"
	"encore.app/marketinstall/store"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

var db = sqldb.NewDatabase("market_install", sqldb.DatabaseConfig{Migrations: "./migrations"})

//encore:service
type Service struct {
	coordinator *app.Coordinator
	ledger      *store.Store
}
type workerLifecycle struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

var lifecycle workerLifecycle

func initService() (*Service, error) {
	enabled := os.Getenv("TP_MARKETINSTALL_ENABLED")
	if enabled != "" && enabled != "true" && enabled != "false" {
		return nil, fmt.Errorf("invalid TP_MARKETINSTALL_ENABLED")
	}
	service := &Service{ledger: store.New(db.Stdlib())}
	if enabled != "true" {
		return service, nil
	}
	provider, err := newHTTPProvider(os.Getenv("TP_MARKETINSTALL_PROVIDER_BASE_URL"), os.Getenv("TP_MARKETINSTALL_PROVIDER_TOKEN"))
	if err != nil {
		return nil, err
	}
	service.coordinator = &app.Coordinator{Store: service.ledger, Provider: provider, NewID: uuid.NewString, LeaseSeconds: 60}
	ctx, cancel := context.WithCancel(context.Background())
	lifecycle.cancel = cancel
	lifecycle.done = make(chan struct{})
	go service.runWorker(ctx, lifecycle.done)
	return service, nil
}
func (s *Service) runWorker(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for batch := 0; batch < 8; batch++ {
			stepCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			worked, err := s.coordinator.RunOne(stepCtx)
			cancel()
			if err != nil {
				rlog.Error("market installation worker could not acknowledge progress", "error", err)
				break
			}
			if !worked {
				break
			}
		}
	}
}
func (s *Service) Shutdown(force context.Context) {
	if lifecycle.cancel == nil {
		return
	}
	lifecycle.once.Do(lifecycle.cancel)
	select {
	case <-lifecycle.done:
	case <-force.Done():
	}
}

type InstallRequest struct {
	IdempotencyKey   string      `header:"Idempotency-Key"`
	AuthorizationRef string      `json:"authorizationRef"`
	Request          app.Request `json:"request"`
}

// Install persists an operation plan from the trusted provider. The request
// cannot supply its own execution steps, tenant, subject, or provider URL.
//
//encore:api auth method=POST path=/api/v2/market-installations
func (s *Service) Install(ctx context.Context, req *InstallRequest) (*app.Operation, error) {
	identity, err := installationIdentity()
	if err != nil {
		return nil, err
	}
	if s.coordinator == nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "market installation owner is disabled"}
	}
	if req == nil {
		return nil, apiError(app.ErrInvalid)
	}
	if err := requireCurrentInstallationManagement(ctx, identity); err != nil {
		return nil, err
	}
	identity.AuthorizationRef = req.AuthorizationRef
	op, err := s.coordinator.Submit(ctx, identity, req.IdempotencyKey, req.Request)
	if err != nil {
		return nil, apiError(err)
	}
	return &op, nil
}

//encore:api auth method=GET path=/api/v2/market-installations/:id
func (s *Service) Get(ctx context.Context, id string) (*app.Operation, error) {
	identity, err := installationIdentity()
	if err != nil {
		return nil, err
	}
	if err := requireCurrentInstallationManagement(ctx, identity); err != nil {
		return nil, err
	}
	op, err := s.ledger.GetByID(ctx, identity.TenantID, identity.SubjectID, id)
	if err != nil {
		return nil, apiError(err)
	}
	return &op, nil
}

// Resume only requeues the original intent for an original-key query. It never
// clears receipts, replaces identity, changes payload, or generates new keys.
//
//encore:api auth method=POST path=/api/v2/market-installations/:id/resume
func (s *Service) Resume(ctx context.Context, id string) error {
	identity, err := installationIdentity()
	if err != nil {
		return err
	}
	if s.coordinator == nil {
		return &errs.Error{Code: errs.Unavailable, Message: "market installation owner is disabled"}
	}
	if err := requireCurrentInstallationManagement(ctx, identity); err != nil {
		return err
	}
	return apiError(s.ledger.Resume(ctx, identity.TenantID, identity.SubjectID, id))
}
func installationIdentity() (app.Identity, error) {
	data := authn.CurrentData()
	if data == nil || strings.TrimSpace(data.TenantID) == "" || strings.TrimSpace(data.UserID) == "" {
		return app.Identity{}, &errs.Error{Code: errs.Unauthenticated, Message: "authentication required"}
	}
	for _, role := range data.Roles {
		switch strings.ToUpper(role) {
		case "TENANT_ADMIN", "SYS_ADMIN", "PLATFORM_ADMIN":
			return app.Identity{TenantID: data.TenantID, SubjectID: data.UserID}, nil
		}
	}
	return app.Identity{}, &errs.Error{Code: errs.PermissionDenied, Message: "installation management permission required"}
}
func apiError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, app.ErrInvalid):
		return &errs.Error{Code: errs.InvalidArgument, Message: "invalid installation request"}
	case errors.Is(err, app.ErrConflict):
		return &errs.Error{Code: errs.Aborted, Message: "installation key conflicts with its original request"}
	case errors.Is(err, app.ErrNotFound):
		return &errs.Error{Code: errs.NotFound, Message: "installation not found"}
	case errors.Is(err, app.ErrLeaseLost):
		return &errs.Error{Code: errs.Aborted, Message: "installation state changed; query the original operation"}
	default:
		return &errs.Error{Code: errs.Unavailable, Message: "installation outcome is unknown; retain the original key and query before retrying"}
	}
}
