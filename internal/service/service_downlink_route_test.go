package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"project/internal/model"
)

type fakeServiceDownlinkLookup struct {
	access       *model.ServiceAccess
	plugin       *model.ServicePlugin
	accessErr    error
	pluginErr    error
	accessCalls  int
	pluginCalls  int
	lastAccessID string
	lastPluginID string
}

func (f *fakeServiceDownlinkLookup) GetServiceAccessByID(id string) (*model.ServiceAccess, error) {
	f.accessCalls++
	f.lastAccessID = id
	return f.access, f.accessErr
}

func (f *fakeServiceDownlinkLookup) GetServicePluginByID(id string) (*model.ServicePlugin, error) {
	f.pluginCalls++
	f.lastPluginID = id
	return f.plugin, f.pluginErr
}

func ptr[T any](value T) *T { return &value }

func TestResolveServiceDownlinkRouteNativeDoesNotReadDatabase(t *testing.T) {
	for _, configID := range []*string{nil, ptr(""), ptr("model-config-1")} {
		lookup := &fakeServiceDownlinkLookup{}
		device := &model.Device{
			DeviceNumber:   "native-42",
			TenantID:       "tenant-a",
			DeviceConfigID: configID,
			AccessWay:      ptr("A"),
		}

		route, err := resolveServiceDownlinkRouteWithLookup(device, lookup)
		if err != nil {
			t.Fatalf("resolve native route with config %v: %v", configID, err)
		}
		if route.IsService || route.TopicPrefix != "" || route.DeviceNumber != "native-42" {
			t.Fatalf("native route with config %v = %+v, want no service prefix and preserved device number", configID, route)
		}
		if lookup.accessCalls != 0 || lookup.pluginCalls != 0 {
			t.Fatalf("native route with config %v unexpectedly read service records: access=%d plugin=%d", configID, lookup.accessCalls, lookup.pluginCalls)
		}
	}
}

func TestResolveServiceDownlinkRouteUsesServiceBindingAndPlatformNumber(t *testing.T) {
	for _, configID := range []*string{nil, ptr(""), ptr("model-config-1")} {
		lookup := &fakeServiceDownlinkLookup{
			access: &model.ServiceAccess{ID: "access-1", ServicePluginID: "plugin-1", TenantID: "tenant-a"},
			plugin: &model.ServicePlugin{ID: "plugin-1", ServiceType: 2, ServiceConfig: ptr(`{"sub_topic_prefix":"plugin/homeassistant/"}`)},
		}
		device := &model.Device{
			DeviceNumber:    "svc-platform-number",
			TenantID:        "tenant-a",
			DeviceConfigID:  configID,
			AccessWay:       ptr("B"),
			ServiceAccessID: ptr("access-1"),
		}

		route, err := resolveServiceDownlinkRouteWithLookup(device, lookup)
		if err != nil {
			t.Fatalf("resolve service route with config %v: %v", configID, err)
		}
		if !route.IsService || route.TopicPrefix != "plugin/homeassistant/" || route.DeviceNumber != "svc-platform-number" {
			t.Fatalf("service route with config %v = %+v, want plugin prefix and platform device number", configID, route)
		}
		if lookup.accessCalls != 1 || lookup.pluginCalls != 1 || lookup.lastAccessID != "access-1" || lookup.lastPluginID != "plugin-1" {
			t.Fatalf("unexpected lookup calls with config %v: %+v", configID, lookup)
		}
	}
}

func TestResolveServiceDownlinkRouteFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		device *model.Device
		lookup *fakeServiceDownlinkLookup
		want   string
	}{
		{
			name:   "service access B without id",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", AccessWay: ptr("B")},
			lookup: &fakeServiceDownlinkLookup{},
			want:   "no service_access_id",
		},
		{
			name:   "access lookup failure",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{accessErr: errors.New("database unavailable")},
			want:   "failed to resolve service access",
		},
		{
			name:   "cross tenant access",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{access: &model.ServiceAccess{ServicePluginID: "plugin-1", TenantID: "tenant-b"}},
			want:   "does not belong to device tenant",
		},
		{
			name:   "plugin lookup failure",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{access: &model.ServiceAccess{ServicePluginID: "plugin-1", TenantID: "tenant-a"}, pluginErr: errors.New("database unavailable")},
			want:   "failed to resolve service plugin",
		},
		{
			name:   "nil service config",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{access: &model.ServiceAccess{ServicePluginID: "plugin-1", TenantID: "tenant-a"}, plugin: &model.ServicePlugin{ServiceType: 2}},
			want:   "no valid service configuration",
		},
		{
			name:   "empty service config",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{access: &model.ServiceAccess{ServicePluginID: "plugin-1", TenantID: "tenant-a"}, plugin: &model.ServicePlugin{ServiceType: 2, ServiceConfig: ptr(" ")}},
			want:   "no valid service configuration",
		},
		{
			name:   "wrong plugin type",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{access: &model.ServiceAccess{ServicePluginID: "plugin-1", TenantID: "tenant-a"}, plugin: &model.ServicePlugin{ServiceType: 1, ServiceConfig: ptr(`{"sub_topic_prefix":"plugin/foo/"}`)}},
			want:   "no valid service configuration",
		},
		{
			name:   "invalid service config JSON",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{access: &model.ServiceAccess{ServicePluginID: "plugin-1", TenantID: "tenant-a"}, plugin: &model.ServicePlugin{ServiceType: 2, ServiceConfig: ptr("{")}},
			want:   "invalid service plugin configuration",
		},
		{
			name:   "missing prefix",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{access: &model.ServiceAccess{ServicePluginID: "plugin-1", TenantID: "tenant-a"}, plugin: &model.ServicePlugin{ServiceType: 2, ServiceConfig: ptr(`{"sub_topic_prefix":" "}`)}},
			want:   "no valid sub_topic_prefix",
		},
		{
			name:   "wildcard prefix",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{access: &model.ServiceAccess{ServicePluginID: "plugin-1", TenantID: "tenant-a"}, plugin: &model.ServicePlugin{ServiceType: 2, ServiceConfig: ptr(`{"sub_topic_prefix":"plugin/#"}`)}},
			want:   "no valid sub_topic_prefix",
		},
		{
			name:   "slash-only prefix",
			device: &model.Device{DeviceNumber: "dev-1", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{access: &model.ServiceAccess{ServicePluginID: "plugin-1", TenantID: "tenant-a"}, plugin: &model.ServicePlugin{ServiceType: 2, ServiceConfig: ptr(`{"sub_topic_prefix":"///"}`)}},
			want:   "no valid sub_topic_prefix",
		},
		{
			name:   "invalid device number",
			device: &model.Device{DeviceNumber: "bad/number", TenantID: "tenant-a", ServiceAccessID: ptr("access-1")},
			lookup: &fakeServiceDownlinkLookup{},
			want:   "invalid device_number",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route, err := resolveServiceDownlinkRouteWithLookup(test.device, test.lookup)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("route=%+v error=%v, want error containing %q", route, err, test.want)
			}
			if test.device.AccessWay != nil && *test.device.AccessWay == "B" && test.lookup.accessCalls != 0 {
				t.Fatalf("invalid service binding unexpectedly queried access: %+v", test.lookup)
			}
		})
	}
}

func TestBuildCommandDataPreservesGenericCommandPayload(t *testing.T) {
	value := `"on"`
	data, err := buildCommandData("switch", &value)
	if err != nil {
		t.Fatalf("build command data: %v", err)
	}
	if data["method"] != "switch" || string(data["params"].(json.RawMessage)) != `"on"` {
		t.Fatalf("command payload = %#v, want method=switch params=\"on\"", data)
	}
	transformed, err := transformCommandDataForMultiLevelGateway(data, &model.Device{DeviceNumber: "native-42"}, "1")
	if err != nil {
		t.Fatalf("transform native command: %v", err)
	}
	if transformed["method"] != "switch" || string(transformed["params"].(json.RawMessage)) != `"on"` {
		t.Fatalf("native command was unexpectedly nested: %#v", transformed)
	}
	if _, err := buildCommandData("switch", ptr("not-json")); err == nil {
		t.Fatal("invalid JSON command value was accepted")
	}
}
