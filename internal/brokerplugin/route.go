// Package brokerplugin resolves the existing plugin socket without changing its
// public SDK contract. All decisions use the caller's database transaction.
package brokerplugin

import (
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	bc "project/internal/brokercontract"
	"project/internal/model"
)

type Route struct {
	AccessID    string `json:"access_id,omitempty"`
	TenantID    string `json:"tenant_id"`
	PluginID    string `json:"plugin_id"`
	Identifier  string `json:"identifier"`
	ServiceType int32  `json:"service_type"`
	Config      string `json:"config"`
	TopicPrefix string `json:"topic_prefix"`
}

// Resolve returns nil for a native MQTT device. A broken plugin relationship
// returns an error so it cannot silently fall back to the MQTT namespace.
func Resolve(tx *gorm.DB, device *model.Device) (*Route, error) {
	if tx == nil || device == nil {
		return nil, errors.New("plugin route input missing")
	}
	var plugin model.ServicePlugin
	accessID := ""
	if device.ServiceAccessID != nil {
		accessID = strings.TrimSpace(*device.ServiceAccessID)
	}
	if accessID != "" || (device.AccessWay != nil && *device.AccessWay == "B") {
		if accessID == "" {
			return nil, errors.New("service device has no service access")
		}
		var access model.ServiceAccess
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=?", accessID, device.TenantID).Take(&access).Error; err != nil {
			return nil, err
		}
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND service_type=2", access.ServicePluginID).Take(&plugin).Error; err != nil {
			return nil, err
		}
	} else {
		if device.DeviceConfigID == nil || *device.DeviceConfigID == "" {
			return nil, nil
		}
		var config model.DeviceConfig
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=?", *device.DeviceConfigID, device.TenantID).Take(&config).Error; err != nil {
			return nil, err
		}
		if config.ProtocolType == nil || *config.ProtocolType == "" || strings.EqualFold(*config.ProtocolType, "MQTT") {
			return nil, nil
		}
		// The existing DAL resolves protocol_type through service_identifier.
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("service_identifier=? AND service_type=1", *config.ProtocolType).Take(&plugin).Error; err != nil {
			return nil, err
		}
	}
	if plugin.ServiceConfig == nil {
		return nil, errors.New("plugin has no socket configuration")
	}
	var config struct {
		Prefix string `json:"sub_topic_prefix"`
	}
	if json.Unmarshal([]byte(*plugin.ServiceConfig), &config) != nil {
		return nil, errors.New("invalid plugin socket configuration")
	}
	prefix := strings.TrimRight(strings.TrimSpace(config.Prefix), "/") + "/"
	if !bc.ValidPluginPrefix(prefix) {
		return nil, errors.New("invalid plugin socket prefix")
	}
	return &Route{AccessID: accessID, TenantID: device.TenantID, PluginID: plugin.ID, Identifier: plugin.ServiceIdentifier, ServiceType: plugin.ServiceType, Config: *plugin.ServiceConfig, TopicPrefix: prefix}, nil
}
