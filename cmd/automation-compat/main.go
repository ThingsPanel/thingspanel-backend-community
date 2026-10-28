// Command automation-compat runs the legacy compatibility adapter on its own.
//
// It exists for one reason: the adapter's value is "the App still works", and
// that claim can only be checked by sending the App's own requests at a real
// process talking to a real automation service. Running it standalone keeps that
// check out of the console's deployment: no database, no Redis, no MQTT, just
// the adapter and the new service.
//
// It is not the production mount. Production mounts the same handlers inside the
// console's router (see router/router_init.go and internal/automationcompat),
// because there the caller's JWT has already been validated and the tenant is
// known. This binary never validates a token itself — it forwards whatever it
// was given to the new service, which is the only component allowed to decide
// who the caller is.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"project/internal/automationcompat"

	"github.com/gin-gonic/gin"
)

func main() {
	var (
		addr    = flag.String("addr", ":14002", "listen address")
		core    = flag.String("core", "", "base URL of the new automation service")
		sunset  = flag.String("sunset", "", "last day the adapter answers (YYYY-MM-DD or RFC3339)")
		timeout = flag.Duration("timeout", 5*time.Second, "per-call timeout towards the automation service")
		shadow  = flag.Bool("shadow", true, "mount under /api/v1/compat instead of the real legacy paths")
	)
	flag.Parse()

	// Flags win over the environment so a rehearsal run cannot inherit a
	// production sunset date by accident.
	applyFlagDefaults(*core, *sunset, *shadow)
	cfg, err := automationcompat.ConfigFromEnv()
	if err != nil {
		log.Fatalf("automation-compat: %v", err)
	}
	if *timeout > 0 {
		cfg.Timeout = *timeout
	}

	h := automationcompat.NewHandlers(automationcompat.Deps{
		Core:   automationcompat.NewCoreClient(cfg.CoreBaseURL, cfg.Timeout),
		Sunset: cfg.Sunset,
	})

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	v1 := r.Group("/api/v1")
	if cfg.Mode == automationcompat.ModeTakeover {
		automationcompat.Mount(v1, h)
	} else {
		automationcompat.MountShadow(v1, h)
	}

	srv := &http.Server{Addr: *addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("automation-compat listening on %s (mode=%s, core=%s, sunset=%s)",
			*addr, cfg.Mode, cfg.CoreBaseURL, cfg.Sunset.Format(time.RFC3339))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("automation-compat: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// applyFlagDefaults copies the flags into the environment the config reader
// uses, but only when the flag was set explicitly.
func applyFlagDefaults(core, sunset string, shadow bool) {
	if core != "" {
		_ = os.Setenv(automationcompat.EnvCoreURL, core)
	}
	if sunset != "" {
		_ = os.Setenv(automationcompat.EnvSunset, sunset)
	}
	mode := automationcompat.ModeShadow
	if !shadow {
		mode = automationcompat.ModeTakeover
	}
	if os.Getenv(automationcompat.EnvMode) == "" {
		_ = os.Setenv(automationcompat.EnvMode, string(mode))
	}
}
