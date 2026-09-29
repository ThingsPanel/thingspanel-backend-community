package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"project/internal/dal"
	"project/internal/model"
)

// serviceDownlinkLookup is the small persistence boundary needed to resolve a
// service-owned device's MQTT namespace. Keeping it explicit makes routing
// policy testable without replacing package globals or opening the database.
type serviceDownlinkLookup interface {
	GetServiceAccessByID(id string) (*model.ServiceAccess, error)
	GetServicePluginByID(id string) (*model.ServicePlugin, error)
}

type dalServiceDownlinkLookup struct{}

func (dalServiceDownlinkLookup) GetServiceAccessByID(id string) (*model.ServiceAccess, error) {
	return dal.GetServiceAccessByID(id)
}

func (dalServiceDownlinkLookup) GetServicePluginByID(id string) (*model.ServicePlugin, error) {
	return dal.GetServicePluginByID(id)
}

type serviceDownlinkRoute struct {
	DeviceNumber string
	TopicPrefix  string
	IsService    bool
}

// resolveServiceDownlinkRoute resolves transport ownership independently of the
// optional device configuration. A broken service binding must never fall back
// to the native MQTT namespace.
func resolveServiceDownlinkRoute(device *model.Device) (serviceDownlinkRoute, error) {
	return resolveServiceDownlinkRouteWithLookup(device, dalServiceDownlinkLookup{})
}

func resolveServiceDownlinkRouteWithLookup(device *model.Device, lookup serviceDownlinkLookup) (serviceDownlinkRoute, error) {
	route := serviceDownlinkRoute{DeviceNumber: device.DeviceNumber}
	accessID := ""
	if device.ServiceAccessID != nil {
		accessID = strings.TrimSpace(*device.ServiceAccessID)
	}
	isService := accessID != "" || (device.AccessWay != nil && *device.AccessWay == "B")
	if !isService {
		return route, nil
	}
	route.IsService = true
	if accessID == "" {
		return route, fmt.Errorf("service device has no service_access_id")
	}
	if device.DeviceNumber == "" || strings.ContainsAny(device.DeviceNumber, "/+#\x00") {
		return route, fmt.Errorf("service device has an invalid device_number")
	}
	access, err := lookup.GetServiceAccessByID(accessID)
	if err != nil {
		return route, fmt.Errorf("failed to resolve service access: %w", err)
	}
	if access == nil {
		return route, fmt.Errorf("service access not found")
	}
	if access.TenantID != device.TenantID {
		return route, fmt.Errorf("service access does not belong to device tenant")
	}
	plugin, err := lookup.GetServicePluginByID(access.ServicePluginID)
	if err != nil {
		return route, fmt.Errorf("failed to resolve service plugin: %w", err)
	}
	if plugin == nil || plugin.ServiceType != 2 || plugin.ServiceConfig == nil || strings.TrimSpace(*plugin.ServiceConfig) == "" {
		return route, fmt.Errorf("service plugin has no valid service configuration")
	}
	var config model.ServiceAccessConfig
	if err := json.Unmarshal([]byte(*plugin.ServiceConfig), &config); err != nil {
		return route, fmt.Errorf("invalid service plugin configuration: %w", err)
	}
	prefix := strings.TrimSpace(config.SubTopicPrefix)
	if prefix == "" || prefix == "/" || strings.ContainsAny(prefix, "+#\x00") {
		return route, fmt.Errorf("service plugin has no valid sub_topic_prefix")
	}
	if strings.TrimRight(prefix, "/") == "" {
		return route, fmt.Errorf("service plugin has no valid sub_topic_prefix")
	}
	// The adapter concatenates this prefix with devices/<message type>/... .
	route.TopicPrefix = strings.TrimRight(prefix, "/") + "/"
	return route, nil
}
