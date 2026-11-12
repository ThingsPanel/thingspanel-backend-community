// Package brokerscope computes the device scope the Broker's service identities are built from, and authenticates
// the host-side manager (tp-agent system role) that asks for it. It has no Encore dependency so it can be tested
// with plain `go test`; the Encore service in ../../brokerscope is a thin wrapper.
//
// The devices live in the platform database (logical resource "platform" of pkg/relational), read with GORM only.
package brokerscope

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"gorm.io/gorm"
)

// TokenEnv names the environment variable holding the manager's bearer token. Unset or shorter than MinTokenLen
// disables the endpoint (404).
const TokenEnv = "GOTP_BROKER_SCOPE_TOKEN"

// MinTokenLen is the shortest accepted token.
const MinTokenLen = 32

// MaxDevices bounds each list; the manager refuses larger scopes.
const MaxDevices = 1000

// Device is one device the Broker must know about.
type Device struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

// Scope: Targets are directly connected MQTT devices; Plugins are devices reached through a protocol/service
// plugin. It contains no credentials.
type Scope struct {
	Targets []Device `json:"targets"`
	Plugins []Device `json:"plugins"`
}

func base(db *gorm.DB) *gorm.DB {
	return db.Table("devices AS d").
		Joins("LEFT JOIN device_configs c ON c.id = d.device_config_id AND c.tenant_id = d.tenant_id").
		Where("d.is_enabled = ? AND d.activate_flag = ?", "enabled", "active").
		Select("d.id AS id, d.tenant_id AS tenant_id").
		Order("d.id").Limit(MaxDevices)
}

// Query computes the scope. The conditions are the ones the installer's former SQL used, expressed through GORM.
func Query(ctx context.Context, db *gorm.DB) (Scope, error) {
	scope := Scope{Targets: []Device{}, Plugins: []Device{}}
	db = db.WithContext(ctx)
	if err := base(db).
		Where("(d.parent_id IS NULL OR d.parent_id = '')").
		Where("(d.service_access_id IS NULL OR d.service_access_id = '')").
		Where("COALESCE(d.access_way, 'A') <> 'B'").
		Where("COALESCE(c.protocol_type, 'MQTT') IN ('MQTT', '')").
		Scan(&scope.Targets).Error; err != nil {
		return Scope{}, err
	}
	if err := base(db).
		Where("((d.service_access_id IS NOT NULL AND d.service_access_id <> '') OR (c.protocol_type IS NOT NULL AND c.protocol_type NOT IN ('MQTT', '')))").
		Scan(&scope.Plugins).Error; err != nil {
		return Scope{}, err
	}
	return scope, nil
}

// Handler serves the scope. token is read per request (so a rotated token needs no restart); open returns the
// platform database handle and may fail while the database is unreachable.
func Handler(token func() string, open func() (*gorm.DB, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		want := strings.TrimSpace(token())
		if len(want) < MinTokenLen {
			http.NotFound(w, r)
			return
		}
		header := r.Header.Get("Authorization")
		got := strings.TrimPrefix(header, "Bearer ")
		if !strings.HasPrefix(header, "Bearer ") || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "invalid manager identity"})
			return
		}
		db, err := open()
		if err != nil || db == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": 503})
			return
		}
		scope, err := Query(r.Context(), db)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500})
			return
		}
		writeJSON(w, http.StatusOK, scope)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
