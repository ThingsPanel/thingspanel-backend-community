package northbound

import (
	"context"
	"log"
	"sync"
	"time"

	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
)

// cfg is process-wide configuration. It is read once from the environment; no
// request path mutates it.
var cfg = loadConfig()

//encore:service
type Service struct{}

// runtimeState holds the worker goroutines. It is package level because an
// Encore service type may not contain channels or timers.
type runtimeState struct {
	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

var rt = &runtimeState{stop: make(chan struct{})}

func initService() (*Service, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Registering the fixed official plugins is idempotent and does not call
	// out to them; health is probed on its own schedule.
	if err := bootstrapInstallations(ctx, cfg.Connectors); err != nil {
		return nil, err
	}
	rt.startWorkers()
	return &Service{}, nil
}

// Shutdown stops the delivery loop. In-flight plugin calls are allowed to
// finish; unfinished leases expire and are re-claimed by the next process.
func (s *Service) Shutdown(force context.Context) {
	rt.stopOnce.Do(func() { close(rt.stop) })
	done := make(chan struct{})
	go func() {
		rt.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-force.Done():
		log.Printf("northbound: shutdown deadline reached with work in flight")
	}
}

// ---------------------------------------------------------------------------
// Identity and authorization
// ---------------------------------------------------------------------------

// principal returns the authenticated caller. Tenant identity always comes from
// the verified token, never from the request body.
func principal() (*authn.Data, error) {
	data := authn.CurrentData()
	if data == nil || data.UserID == "" || data.TenantID == "" {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "authentication required"}
	}
	return data, nil
}

// requireAction is the single authorization decision point for this service.
// The Community runtime does not pre-authorize northbound actions; it only
// forwards the caller's token.
func requireAction(data *authn.Data, action string) error {
	if !authz.Can(data, "northbound", action) {
		return errDenied("role does not allow northbound:" + action)
	}
	return nil
}

// requireInternal authenticates the Community runtime on the source handoff.
// It fails closed when no token is configured.
func requireInternal(token string) error {
	if cfg.ServiceToken == "" {
		return errUnavailable("northbound service token is not configured")
	}
	if !subtleCompare(token, cfg.ServiceToken) {
		return &errs.Error{Code: errs.Unauthenticated, Message: "invalid service token"}
	}
	return nil
}
