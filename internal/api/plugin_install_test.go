package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"project/internal/plugininstall"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type stubAgent struct{ secrets map[string]string }

func (a *stubAgent) Catalog(context.Context) ([]plugininstall.CatalogEntry, error) {
	return []plugininstall.CatalogEntry{{ID: "homeassistant", Version: "1", ServiceIdentifier: "homeassistant",
		EnvUser: map[string]plugininstall.FieldSpec{}}}, nil
}
func (a *stubAgent) PutSecret(_ context.Context, i, k, v string) error {
	a.secrets[i+k] = v
	return nil
}
func (a *stubAgent) SecretKeys(context.Context, string) ([]string, error)        { return nil, nil }
func (a *stubAgent) ClearConfig(context.Context, string) error                   { return nil }
func (a *stubAgent) AcceptGeneration(context.Context, string, uint64) error      { return nil }
func (a *stubAgent) Prepare(context.Context, plugininstall.PrepareRequest) error { return nil }
func (a *stubAgent) Grant(context.Context, string, uint64, string, string, time.Duration) error {
	return nil
}
func (a *stubAgent) Renew(context.Context, string, uint64, string, string, time.Duration) error {
	return nil
}
func (a *stubAgent) Activate(context.Context, string, uint64, string) error { return nil }
func (a *stubAgent) Revoke(context.Context, string, uint64, string) error   { return nil }
func (a *stubAgent) Retire(context.Context, string, uint64) error           { return nil }
func (a *stubAgent) Status(context.Context) error                           { return nil }

func installRouter(t *testing.T, authority string) (*gin.Engine, *stubAgent) {
	return installRouterWithClaims(t, utils.UserClaims{Authority: authority, TenantID: "tn1"})
}

func installRouterWithClaims(t *testing.T, claims utils.UserClaims) (*gin.Engine, *stubAgent) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&plugininstall.Installation{}, &plugininstall.Event{}); err != nil {
		t.Fatal(err)
	}
	agent := &stubAgent{secrets: map[string]string{}}
	installOnce, installEngine = sync.Once{}, nil
	installOnce.Do(func() {
		installEngine = &plugininstall.Engine{Repo: plugininstall.NewRepo(func() *gorm.DB { return db }), Agent: agent, Owner: "t", Timing: plugininstall.DefaultTiming()}
	})
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("claims", &claims)
		c.Next()
		if d, ok := c.Get("data"); ok && !c.Writer.Written() {
			c.JSON(c.Writer.Status(), gin.H{"code": 200, "data": d})
		}
	})
	r.GET("/plugin/catalog", PluginInstallCatalog)
	r.POST("/plugin/installs", PluginInstallCreate)
	r.GET("/plugin/installs/:id", PluginInstallGet)
	r.DELETE("/plugin/installs/:id", PluginInstallUninstall)
	r.POST("/plugin/installs/:id/upgrade", PluginInstallUpgrade)
	return r, agent
}

func doJSON(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPluginInstallRequiresPlatformAdmin(t *testing.T) {
	for _, authority := range []string{"TENANT_ADMIN", "TENANT_USER"} {
		r, _ := installRouter(t, authority)
		if w := doJSON(r, "GET", "/plugin/catalog", ""); w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", authority, w.Code)
		}
		if w := doJSON(r, "POST", "/plugin/installs", `{"plugin_id":"homeassistant"}`); w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", authority, w.Code)
		}
	}
}

func TestPluginInstallRejectsDelegatedPlatformAdmin(t *testing.T) {
	r, _ := installRouterWithClaims(t, utils.UserClaims{Authority: "SYS_ADMIN", TenantID: "", Delegated: true})
	if w := doJSON(r, "GET", "/plugin/catalog", ""); w.Code != http.StatusForbidden {
		t.Fatalf("delegated platform admin must not read the install catalog: %d", w.Code)
	}
	if w := doJSON(r, "POST", "/plugin/installs", `{"plugin_id":"homeassistant"}`); w.Code != http.StatusForbidden {
		t.Fatalf("delegated platform admin must not install plugins: %d", w.Code)
	}
}

func TestPluginInstallNotConfiguredIsUnavailable(t *testing.T) {
	r, _ := installRouter(t, "SYS_ADMIN")
	installOnce, installEngine = sync.Once{}, nil
	installOnce.Do(func() {})
	if w := doJSON(r, "GET", "/plugin/catalog", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("%d", w.Code)
	}
}

func TestPluginInstallDoesNotCollectTenantCredentials(t *testing.T) {
	r, agent := installRouter(t, "SYS_ADMIN")
	w := doJSON(r, "POST", "/plugin/installs", `{"plugin_id":"homeassistant"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "TOPSECRET-1") || len(agent.secrets) != 0 {
		t.Fatalf("secret handling wrong: %s", w.Body)
	}
	var out struct {
		Data struct {
			ID, State  string
			SecretsSet []string `json:"secrets_set"`
			Config     map[string]string
		}
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Data.State != "installing" || len(out.Data.SecretsSet) != 0 || len(out.Data.Config) != 0 {
		t.Fatalf("%+v", out)
	}
	if g := doJSON(r, "GET", "/plugin/installs/"+out.Data.ID, ""); g.Code != 200 || strings.Contains(g.Body.String(), "TOPSECRET-1") {
		t.Fatalf("%d %s", g.Code, g.Body)
	}
}

func TestPluginUpgradeRouteIsPlatformAdminOnlyAndMapsStates(t *testing.T) {
	for _, authority := range []string{"TENANT_ADMIN", "TENANT_USER"} {
		r, _ := installRouter(t, authority)
		if w := doJSON(r, "POST", "/plugin/installs/x/upgrade", ""); w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", authority, w.Code)
		}
	}
	r, _ := installRouter(t, "SYS_ADMIN")
	if w := doJSON(r, "POST", "/plugin/installs/does-not-exist/upgrade", ""); w.Code != http.StatusNotFound {
		t.Fatalf("an unknown installation is 404: %d %s", w.Code, w.Body)
	}
	created := doJSON(r, "POST", "/plugin/installs", `{"plugin_id":"homeassistant"}`)
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &out); err != nil || out.Data.ID == "" {
		t.Fatalf("%v %s", err, created.Body)
	}
	// not running yet (waiting for configuration): the state does not allow an upgrade
	if w := doJSON(r, "POST", "/plugin/installs/"+out.Data.ID+"/upgrade", ""); w.Code != http.StatusConflict {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
