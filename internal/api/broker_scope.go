package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// BrokerScopeTokenEnv names the environment variable holding the bearer token
// of the host-side manager (tp-agent system role). Unset disables the route.
const BrokerScopeTokenEnv = "GOTP_BROKER_SCOPE_TOKEN"

// maxBrokerScopeDevices bounds each list; the manager refuses larger scopes.
const maxBrokerScopeDevices = 1000

// BrokerScopeDevice is one device the Broker must know about.
type BrokerScopeDevice struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

// BrokerScope is the device scope the Broker's service identities are built from.
// Targets are directly connected MQTT devices; Plugins are devices reached
// through a protocol/service plugin. It contains no credentials.
type BrokerScope struct {
	Targets []BrokerScopeDevice `json:"targets"`
	Plugins []BrokerScopeDevice `json:"plugins"`
}

// RegisterBrokerScopeRoutes serves the scope to the host-side manager. The
// platform computes the scope with GORM, so the manager never needs database
// credentials or knowledge of the device tables.
func RegisterBrokerScopeRoutes(router *gin.Engine, db *gorm.DB) {
	router.GET("/internal/broker-scope", func(c *gin.Context) {
		want := strings.TrimSpace(os.Getenv(BrokerScopeTokenEnv))
		if len(want) < 32 {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		got := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid manager identity"})
			return
		}
		if db == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 503})
			return
		}
		scope, err := QueryBrokerScope(c.Request.Context(), db)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": 500})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, scope)
	})
}

func scopeBase(db *gorm.DB) *gorm.DB {
	return db.Table("devices AS d").
		Joins("LEFT JOIN device_configs c ON c.id = d.device_config_id AND c.tenant_id = d.tenant_id").
		Where("d.is_enabled = ? AND d.activate_flag = ?", "enabled", "active").
		Select("d.id AS id, d.tenant_id AS tenant_id").
		Order("d.id").Limit(maxBrokerScopeDevices)
}

// QueryBrokerScope computes the scope. The conditions are the ones the
// installer's former SQL used, expressed through GORM.
func QueryBrokerScope(ctx context.Context, db *gorm.DB) (BrokerScope, error) {
	scope := BrokerScope{Targets: []BrokerScopeDevice{}, Plugins: []BrokerScopeDevice{}}
	db = db.WithContext(ctx)
	if err := scopeBase(db).
		Where("(d.parent_id IS NULL OR d.parent_id = '')").
		Where("(d.service_access_id IS NULL OR d.service_access_id = '')").
		Where("COALESCE(d.access_way, 'A') <> 'B'").
		Where("COALESCE(c.protocol_type, 'MQTT') IN ('MQTT', '')").
		Scan(&scope.Targets).Error; err != nil {
		return BrokerScope{}, err
	}
	if err := scopeBase(db).
		Where("((d.service_access_id IS NOT NULL AND d.service_access_id <> '') OR (c.protocol_type IS NOT NULL AND c.protocol_type NOT IN ('MQTT', '')))").
		Scan(&scope.Plugins).Error; err != nil {
		return BrokerScope{}, err
	}
	return scope, nil
}
