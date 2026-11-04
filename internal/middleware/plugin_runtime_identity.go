package middleware

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"project/pkg/global"

	"github.com/gin-gonic/gin"
)

// Managed plugin runtime identities replace the closed anonymous plugin runtime entrypoints.
//
// An identity is issued per plugin instance and generation by an authenticated tenant administrator
// (normally the host installer when it installs the plugin), bound to one service identifier and one
// tenant, time-limited and revocable. Issuing a higher generation revokes every lower one, so an old
// instance loses platform access at once. Only the secret's SHA-256 is stored.
const (
	PluginRuntimeIdentityKey = "plugin_runtime_identity"
	pluginIdentitySchema     = `CREATE TABLE IF NOT EXISTS plugin_runtime_identities (
		instance_id varchar(64) PRIMARY KEY,
		plugin_instance varchar(128) NOT NULL,
		service_identifier varchar(100) NOT NULL,
		tenant_id varchar(36) NOT NULL,
		generation bigint NOT NULL,
		secret_sha256 char(64) NOT NULL,
		created_at timestamptz NOT NULL DEFAULT now(),
		expires_at timestamptz NOT NULL,
		revoked_at timestamptz
	);
	CREATE INDEX IF NOT EXISTS idx_plugin_runtime_identities_instance ON plugin_runtime_identities(plugin_instance, generation)`
)

type PluginRuntimeIdentity struct {
	InstanceID        string
	PluginInstance    string
	ServiceIdentifier string
	TenantID          string
	Generation        int64
}

var (
	pluginIdentityOnce sync.Once
	pluginIdentityErr  error
	identityIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_]{8,64}$`)
	identitySecretExpr = regexp.MustCompile(`^[0-9a-f]{32,128}$`)
)

func EnsurePluginRuntimeIdentityTable() error {
	pluginIdentityOnce.Do(func() {
		if global.DB == nil {
			pluginIdentityErr = io.ErrClosedPipe
			return
		}
		pluginIdentityErr = global.DB.Exec(pluginIdentitySchema).Error
	})
	return pluginIdentityErr
}

func HashPluginSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func denyPluginRuntime(c *gin.Context, status int) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, gin.H{
		"code":    status,
		"message": "managed plugin instance authorization is required",
		"data":    gin.H{"state": "blocked", "errorCode": "PLUGIN_RUNTIME_IDENTITY_REQUIRED", "automatic_retry": false},
	})
}

// RequireManagedPluginRuntime authenticates a plugin instance by its bearer identity. With bindServiceInBody the
// request body's service_identifier, when present, must equal the identity's service.
func RequireManagedPluginRuntime(bindServiceInBody bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if EnsurePluginRuntimeIdentityTable() != nil {
			denyPluginRuntime(c, http.StatusServiceUnavailable)
			return
		}
		header := c.GetHeader("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		id, secret, found := strings.Cut(token, ".")
		if !strings.HasPrefix(header, "Bearer ") || !found || !identityIDPattern.MatchString(id) || !identitySecretExpr.MatchString(secret) {
			denyPluginRuntime(c, http.StatusUnauthorized)
			return
		}
		var row struct {
			InstanceID        string
			PluginInstance    string
			ServiceIdentifier string
			TenantID          string
			Generation        int64
			SecretSha256      string
		}
		err := global.DB.Raw(`SELECT instance_id, plugin_instance, service_identifier, tenant_id, generation, secret_sha256
			FROM plugin_runtime_identities WHERE instance_id = ? AND revoked_at IS NULL AND expires_at > now()`, id).Scan(&row).Error
		if err != nil || row.InstanceID == "" || subtle.ConstantTimeCompare([]byte(row.SecretSha256), []byte(HashPluginSecret(secret))) != 1 {
			denyPluginRuntime(c, http.StatusUnauthorized)
			return
		}
		if bindServiceInBody {
			raw, readErr := io.ReadAll(io.LimitReader(c.Request.Body, 64<<10))
			if readErr != nil {
				denyPluginRuntime(c, http.StatusBadRequest)
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(raw))
			var body struct {
				ServiceIdentifier string `json:"service_identifier"`
			}
			if json.Unmarshal(raw, &body) == nil && body.ServiceIdentifier != "" && body.ServiceIdentifier != row.ServiceIdentifier {
				denyPluginRuntime(c, http.StatusForbidden)
				return
			}
		}
		c.Set(PluginRuntimeIdentityKey, &PluginRuntimeIdentity{InstanceID: row.InstanceID, PluginInstance: row.PluginInstance,
			ServiceIdentifier: row.ServiceIdentifier, TenantID: row.TenantID, Generation: row.Generation})
		c.Next()
	}
}
