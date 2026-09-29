package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"project/internal/downlink"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
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

func TestResolveServiceDownlinkRouteUsesGenericPluginPrefix(t *testing.T) {
	lookup := &fakeServiceDownlinkLookup{
		access: &model.ServiceAccess{ID: "access-1", ServicePluginID: "plugin-1", TenantID: "tenant-a"},
		plugin: &model.ServicePlugin{ID: "plugin-1", ServiceType: 2, ServiceConfig: ptr(`{"sub_topic_prefix":"plugin/sample-service/"}`)},
	}
	route, err := resolveServiceDownlinkRouteWithLookup(&model.Device{
		DeviceNumber:    "service-device-1",
		TenantID:        "tenant-a",
		AccessWay:       ptr("B"),
		ServiceAccessID: ptr("access-1"),
	}, lookup)
	if err != nil {
		t.Fatalf("resolve generic service route: %v", err)
	}
	if !route.IsService || route.TopicPrefix != "plugin/sample-service/" {
		t.Fatalf("generic service route = %+v, want plugin/sample-service/ prefix", route)
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

func TestDownlinkEntrypointsLoadServiceRelationFromDatabase(t *testing.T) {
	oldDevice := query.Device
	oldRedis := global.REDIS
	db, err := gorm.Open(sqlite.Open("file:downlink-device-route?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Device{}); err != nil {
		t.Fatalf("migrate devices: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get test database connection: %v", err)
	}
	testQuery := query.Use(db)
	query.Device = &testQuery.Device
	global.REDIS = nil
	t.Cleanup(func() {
		query.Device = oldDevice
		global.REDIS = oldRedis
		_ = sqlDB.Close()
	})

	device := &model.Device{
		ID:           "service-device-1",
		Voucher:      "service-device-1",
		TenantID:     "tenant-a",
		IsEnabled:    "enabled",
		ActivateFlag: "active",
		DeviceNumber: "svc-platform-number",
		AccessWay:    ptr("B"),
		// Intentionally incomplete service relation: every route must fail closed.
	}
	if err := query.Device.Create(device); err != nil {
		t.Fatalf("create service device: %v", err)
	}

	commandValue := `"on"`
	command := &CommandData{}
	command.SetDownlinkBus(downlink.NewBus(1))
	_, commandErr := command.CommandPutMessageWithResult(context.Background(), "user-1", &model.PutMessageForCommand{
		DeviceID: device.ID,
		Identify: "switch",
		Value:    &commandValue,
	}, "1", device.TenantID)

	attributeErr := (&AttributeData{}).AttributePutMessage(context.Background(), "user-1", &model.AttributePutMessage{
		DeviceID: device.ID,
		Value:    `{"switch":true}`,
	}, "1")

	telemetryErr := (&TelemetryData{}).TelemetryPutMessage(context.Background(), "user-1", &model.PutMessage{
		DeviceID: device.ID,
		Value:    `{"switch":true}`,
	}, "1")

	for name, callErr := range map[string]error{
		"command":   commandErr,
		"attribute": attributeErr,
		"telemetry": telemetryErr,
	} {
		if callErr == nil || !strings.Contains(callErr.Error(), "no service_access_id") {
			t.Errorf("%s downlink error = %v, want missing service_access_id from the database-loaded service device", name, callErr)
		}
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
