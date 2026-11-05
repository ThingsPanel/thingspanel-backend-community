// Package plugininstall is the single installation record and orchestration for managed service plugins
// (docs/PLUGIN-INSTALL-UI-DESIGN.md). It owns one row per (tenant, plugin) and drives the host Agent through a fixed,
// allow-listed set of operations. It does not depend on gin; relational access is GORM only.
package plugininstall

import (
	"encoding/json"
	"time"
)

// User-visible states.
const (
	StatePendingConfig = "pending_config" // row exists, required site information is missing; nothing runs
	StateInstalling    = "installing"     // an operation is in flight
	StateInstalled     = "installed"      // configured and stopped (disabled); identity revoked
	StateEnabled       = "enabled"        // container running, identity live, health checked
	StateFailed        = "failed"         // last operation failed at OpStep; retry resumes
	StateUninstalling  = "uninstalling"
	StateUninstalled   = "uninstalled" // tombstone: the generation counter and service_plugins row are kept
)

// Desired outcome a reconcile loop drives towards.
const (
	DesiredEnabled  = "enabled"
	DesiredDisabled = "disabled"
	DesiredAbsent   = "absent"
)

// Installation is the plugin_installations row. It never holds a secret value, only the names of secret keys that the
// Agent holds.
type Installation struct {
	ID             string     `gorm:"column:id;primaryKey;size:36" json:"id"`
	TenantID       string     `gorm:"column:tenant_id;size:36;not null;uniqueIndex:uq_plugin_install_tenant_plugin" json:"tenant_id"`
	PluginID       string     `gorm:"column:plugin_id;size:64;not null;uniqueIndex:uq_plugin_install_tenant_plugin" json:"plugin_id"`
	State          string     `gorm:"column:state;size:24;not null" json:"state"`
	Desired        string     `gorm:"column:desired;size:16;not null" json:"desired"`
	Generation     int64      `gorm:"column:generation;not null;default:0" json:"generation"`
	CatalogVersion string     `gorm:"column:catalog_version;size:64;not null;default:''" json:"catalog_version"`
	ConfigPublic   string     `gorm:"column:config_public;type:text;not null;default:'{}'" json:"-"`
	SecretKeys     string     `gorm:"column:secret_keys;type:text;not null;default:'[]'" json:"-"`
	Health         string     `gorm:"column:health;size:16;not null;default:''" json:"health"`
	OpID           string     `gorm:"column:op_id;size:36;not null;default:''" json:"op_id"`
	OpStep         string     `gorm:"column:op_step;size:32;not null;default:''" json:"op_step"`
	OpAttempts     int        `gorm:"column:op_attempts;not null;default:0" json:"op_attempts"`
	ErrorCode      string     `gorm:"column:error_code;size:48;not null;default:''" json:"error_code"`
	IdempotencyKey string     `gorm:"column:idempotency_key;size:128;not null;default:''" json:"-"`
	LeaseOwner     string     `gorm:"column:lease_owner;size:64;not null;default:''" json:"-"`
	LeaseUntil     *time.Time `gorm:"column:lease_until" json:"-"`
	MaintainedAt   *time.Time `gorm:"column:maintained_at" json:"maintained_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (Installation) TableName() string { return "plugin_installations" }

// Event is one step record of an operation. Messages are fixed codes, never values from the user's configuration.
type Event struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	InstallationID string    `gorm:"column:installation_id;size:36;not null;index" json:"installation_id"`
	OpID           string    `gorm:"column:op_id;size:36;not null" json:"op_id"`
	Step           string    `gorm:"column:step;size:32;not null" json:"step"`
	Outcome        string    `gorm:"column:outcome;size:16;not null" json:"outcome"` // ok | failed | waiting
	Code           string    `gorm:"column:code;size:48;not null;default:''" json:"code"`
	CreatedAt      time.Time `gorm:"column:created_at;not null" json:"created_at"`
}

func (Event) TableName() string { return "plugin_install_events" }

func (i *Installation) PublicConfig() map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal([]byte(i.ConfigPublic), &out)
	return out
}

func (i *Installation) SecretKeyList() []string {
	var out []string
	_ = json.Unmarshal([]byte(i.SecretKeys), &out)
	return out
}

func (i *Installation) setPublic(m map[string]string) {
	raw, _ := json.Marshal(m)
	i.ConfigPublic = string(raw)
}

func (i *Installation) setSecretKeys(keys []string) {
	if keys == nil {
		keys = []string{}
	}
	raw, _ := json.Marshal(keys)
	i.SecretKeys = string(raw)
}
