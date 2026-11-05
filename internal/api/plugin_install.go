package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"project/internal/pluginidentity"
	"project/internal/plugininstall"
	"project/pkg/global"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Plugin one-click installation (docs/PLUGIN-INSTALL-UI-DESIGN.md). Handlers are thin: the single implementation of
// the installation flow is internal/plugininstall. Only a tenant administrator may use these routes. The host Agent is
// configured by TP_AGENT_SOCKET and TP_AGENT_TOKEN_FILE; without them the feature answers 503 and nothing else changes.

var (
	installOnce   sync.Once
	installEngine *plugininstall.Engine
)

func pluginInstallEngine() *plugininstall.Engine {
	installOnce.Do(func() {
		socket, tokenFile := os.Getenv("TP_AGENT_SOCKET"), os.Getenv("TP_AGENT_TOKEN_FILE")
		if socket == "" || tokenFile == "" {
			return
		}
		db := func() *gorm.DB { return global.DB }
		host, _ := os.Hostname()
		installEngine = &plugininstall.Engine{
			Repo: plugininstall.NewRepo(db),
			Agent: plugininstall.NewUnixAgent(socket, func() (string, error) {
				raw, err := os.ReadFile(tokenFile)
				return strings.TrimSpace(string(raw)), err
			}),
			IDs:      plugininstall.NewStoreIdentities(pluginidentity.NewStore(db)),
			Services: plugininstall.NewGormServices(db),
			Health:   plugininstall.HTTPHealth{},
			Owner:    "plugininstall@" + host, // stable per process host: a restarted process takes its own unfinished leases back at once
			Timing:   plugininstall.DefaultTiming(),
		}
	})
	return installEngine
}

// StartPluginInstallEngine runs the reconcile loop until ctx ends. It is a no-op when the Agent is not configured.
func StartPluginInstallEngine(ctx context.Context) {
	if engine := pluginInstallEngine(); engine != nil {
		logrus.Info("plugin install engine started")
		go engine.Run(ctx, 30*time.Second)
	}
}

type installView struct {
	ID           string                `json:"id"`
	PluginID     string                `json:"plugin_id"`
	State        string                `json:"state"`
	Desired      string                `json:"desired"`
	Generation   int64                 `json:"generation"`
	Version      string                `json:"version"`
	Health       string                `json:"health"`
	OpID         string                `json:"op_id"`
	OpStep       string                `json:"op_step"`
	ErrorCode    string                `json:"error_code"`
	Config       map[string]string     `json:"config"`      // non-secret values only
	SecretsSet   []string              `json:"secrets_set"` // names only, never values
	MaintainedAt *time.Time            `json:"maintained_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
	Events       []plugininstall.Event `json:"events,omitempty"`
}

func toInstallView(i *plugininstall.Installation, events []plugininstall.Event) *installView {
	if i == nil {
		return nil
	}
	secrets := i.SecretKeyList()
	if secrets == nil {
		secrets = []string{}
	}
	return &installView{ID: i.ID, PluginID: i.PluginID, State: i.State, Desired: i.Desired, Generation: i.Generation, Version: i.CatalogVersion,
		Health: i.Health, OpID: i.OpID, OpStep: i.OpStep, ErrorCode: i.ErrorCode, Config: i.PublicConfig(), SecretsSet: secrets,
		MaintainedAt: i.MaintainedAt, UpdatedAt: i.UpdatedAt, Events: events}
}

func installFail(c *gin.Context, err error) {
	status, code, msg := http.StatusInternalServerError, "PLUGIN_INSTALL_FAILED", "request failed"
	switch {
	case errors.Is(err, plugininstall.ErrNotFound), errors.Is(err, plugininstall.ErrUnknownPlugin):
		status, code, msg = http.StatusNotFound, "NOT_FOUND", "not found"
	case errors.Is(err, plugininstall.ErrInvalidConfig):
		status, code, msg = http.StatusBadRequest, "INVALID_CONFIG", err.Error()
	case errors.Is(err, plugininstall.ErrBadState):
		status, code, msg = http.StatusConflict, "BAD_STATE", "not allowed in the current state"
	case errors.Is(err, plugininstall.ErrAgentDown):
		status, code, msg = http.StatusServiceUnavailable, "AGENT_OFFLINE", "the host agent is not reachable"
	}
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, gin.H{"code": status, "message": msg, "data": gin.H{"errorCode": code}})
}

func installGuard(c *gin.Context) (string, *plugininstall.Engine, bool) {
	claims, ok := tenantAdminClaims(c)
	if !ok {
		return "", nil, false
	}
	engine := pluginInstallEngine()
	if engine == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "plugin installation is not configured on this host",
			"data": gin.H{"errorCode": "PLUGIN_INSTALL_NOT_CONFIGURED"}})
		return "", nil, false
	}
	c.Header("Cache-Control", "no-store")
	return claims.TenantID, engine, true
}

// GET /api/v1/plugin/catalog
func PluginInstallCatalog(c *gin.Context) {
	tenant, engine, ok := installGuard(c)
	if !ok {
		return
	}
	items, err := engine.Catalog(c.Request.Context(), tenant)
	if err != nil {
		installFail(c, err)
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{"id": it.ID, "display_name": it.DisplayName, "version": it.Version, "description": it.Description,
			"service_identifier": it.ServiceIdentifier, "fields": it.EnvUser, "installation": toInstallView(it.Installation, nil)})
	}
	c.Set("data", out)
}

type installBody struct {
	PluginID string            `json:"plugin_id"`
	Config   map[string]string `json:"config"`
}

// POST /api/v1/plugin/installs  (header Idempotency-Key)
func PluginInstallCreate(c *gin.Context) {
	tenant, engine, ok := installGuard(c)
	if !ok {
		return
	}
	var body installBody
	if err := c.ShouldBindJSON(&body); err != nil || body.PluginID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid request"})
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if len(key) > 128 {
		key = key[:128]
	}
	row, err := engine.Submit(c.Request.Context(), tenant, body.PluginID, key, body.Config)
	if err != nil {
		installFail(c, err)
		return
	}
	c.Set("data", toInstallView(row, nil))
}

// GET /api/v1/plugin/installs
func PluginInstallList(c *gin.Context) {
	tenant, engine, ok := installGuard(c)
	if !ok {
		return
	}
	rows, err := engine.List(tenant)
	if err != nil {
		installFail(c, err)
		return
	}
	out := make([]*installView, 0, len(rows))
	for i := range rows {
		out = append(out, toInstallView(&rows[i], nil))
	}
	c.Set("data", out)
}

// GET /api/v1/plugin/installs/:id
func PluginInstallGet(c *gin.Context) {
	tenant, engine, ok := installGuard(c)
	if !ok {
		return
	}
	row, events, err := engine.Get(tenant, c.Param("id"))
	if err != nil {
		installFail(c, err)
		return
	}
	c.Set("data", toInstallView(row, events))
}

// PUT /api/v1/plugin/installs/:id/config   {config:{...}}  (omitted secret fields stay unchanged)
func PluginInstallConfig(c *gin.Context) {
	tenant, engine, ok := installGuard(c)
	if !ok {
		return
	}
	var body installBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid request"})
		return
	}
	row, err := engine.UpdateConfig(c.Request.Context(), tenant, c.Param("id"), body.Config)
	if err != nil {
		installFail(c, err)
		return
	}
	c.Set("data", toInstallView(row, nil))
}

func installAction(do func(*plugininstall.Engine, context.Context, string, string) (*plugininstall.Installation, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenant, engine, ok := installGuard(c)
		if !ok {
			return
		}
		row, err := do(engine, c.Request.Context(), tenant, c.Param("id"))
		if err != nil {
			installFail(c, err)
			return
		}
			c.Set("data", toInstallView(row, nil))
	}
}

var (
	PluginInstallEnable    = installAction((*plugininstall.Engine).Enable)
	PluginInstallDisable   = installAction((*plugininstall.Engine).Disable)
	PluginInstallRetry     = installAction((*plugininstall.Engine).Retry)
	PluginInstallUninstall = installAction((*plugininstall.Engine).Uninstall)
)
