package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"encore.dev/rlog"
)

type mqttBridge struct {
	client mqtt.Client
	latest sync.Map
}

func newMQTTBridgeFromEnv() (*mqttBridge, error) {
	broker := envAny("DEVICE_METADATA_MQTT_BROKER", "MQTT_BROKER", "TP_MQTT_BROKER")
	if broker == "" {
		return nil, nil
	}
	opts := mqtt.NewClientOptions().AddBroker(broker)
	opts.SetClientID("device-metadata-" + newID())
	if user := envAny("DEVICE_METADATA_MQTT_USERNAME", "MQTT_USERNAME", "TP_MQTT_USER"); user != "" {
		opts.SetUsername(user)
	}
	if pass := envAny("DEVICE_METADATA_MQTT_PASSWORD", "MQTT_PASSWORD", "TP_MQTT_PASS"); pass != "" {
		opts.SetPassword(pass)
	}
	opts.SetAutoReconnect(true)
	bridge := &mqttBridge{}
	opts.OnConnect = func(c mqtt.Client) {
		if err := bridge.subscribe(c); err != nil {
			rlog.Error("device metadata mqtt subscribe failed", "err", err)
		}
	}
	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(5 * time.Second) {
		return nil, fmt.Errorf("mqtt connect timeout")
	}
	if err := token.Error(); err != nil {
		return nil, err
	}
	bridge.client = client
	return bridge, nil
}

func envAny(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func (b *mqttBridge) subscribe(client mqtt.Client) error {
	handlers := map[string]mqtt.MessageHandler{
		"devices/+/property/up": b.handlePropertyUp,
		"devices/+/event/up":    b.handleEventUp,
		"devices/+/command/up":  b.handleCommandUp,
		"devices/+/status/up":   b.handleStatusUp,
	}
	for topic, handler := range handlers {
		token := client.Subscribe(topic, 0, handler)
		if !token.WaitTimeout(5 * time.Second) {
			return fmt.Errorf("subscribe timeout for %s", topic)
		}
		if err := token.Error(); err != nil {
			return err
		}
	}
	return nil
}

func extractSN(topic string) string {
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		return ""
	}
	return parts[1]
}

func (b *mqttBridge) handlePropertyUp(_ mqtt.Client, msg mqtt.Message) {
	sn := extractSN(msg.Topic())
	var payload propertyPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		rlog.Error("invalid property payload", "topic", msg.Topic(), "err", err)
		return
	}
	ctx := context.Background()
	device, err := getDeviceBySN(ctx, resolveTenantFromMessage(msg), sn)
	if err != nil {
		rlog.Error("property topic device resolve failed", "sn", sn, "err", err)
		return
	}
	items, err := itemsByTemplateSnapshot(ctx, device)
	if err != nil {
		rlog.Error("property topic snapshot load failed", "sn", sn, "err", err)
		return
	}
	accepted := map[string]any{}
	for ident, value := range payload.Data {
		if item, ok := items[ident]; ok {
			if err := validateItemValueType(item.ValueType, value); err == nil {
				accepted[ident] = value
			} else {
				rlog.Info("property value rejected", "sn", sn, "identifier", ident, "err", err)
			}
		} else {
			rlog.Info("property identifier missing", "sn", sn, "identifier", ident)
		}
	}
	if len(accepted) > 0 {
		b.latest.Store(device.ID, accepted)
	}
}

func (b *mqttBridge) handleEventUp(_ mqtt.Client, msg mqtt.Message) {
	var payload eventPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		rlog.Error("invalid event payload", "topic", msg.Topic(), "err", err)
		return
	}
	rlog.Info("event uplink accepted", "topic", msg.Topic(), "identifier", payload.Identifier, "level", payload.Level)
}

func (b *mqttBridge) handleCommandUp(_ mqtt.Client, msg mqtt.Message) {
	var payload commandRespPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		rlog.Error("invalid command response payload", "topic", msg.Topic(), "err", err)
		return
	}
	rlog.Info("command response accepted", "topic", msg.Topic(), "msg_id", payload.MsgID, "code", payload.Code)
}

func (b *mqttBridge) handleStatusUp(_ mqtt.Client, msg mqtt.Message) {
	sn := extractSN(msg.Topic())
	var payload statusPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		rlog.Error("invalid status payload", "topic", msg.Topic(), "err", err)
		return
	}
	ctx := context.Background()
	device, err := getDeviceBySN(ctx, resolveTenantFromMessage(msg), sn)
	if err != nil {
		rlog.Error("status topic device resolve failed", "sn", sn, "err", err)
		return
	}
	if err := updateDeviceOnline(ctx, device.TenantID, device.ID, payload.Online, payload.TS); err != nil {
		rlog.Error("device online state update failed", "sn", sn, "err", err)
	}
}

func resolveTenantFromMessage(_ mqtt.Message) string {
	if tenantID, err := mustTenantID(); err == nil {
		return tenantID
	}
	value := strings.TrimSpace(os.Getenv("DEVICE_METADATA_DEFAULT_TENANT"))
	if value == "" {
		value = "default"
	}
	return normalizePrincipalID(value)
}

func itemsByTemplateSnapshot(ctx context.Context, device *Device) (map[string]ThingModelItem, error) {
	tpl, err := getDeviceTemplate(ctx, device.TenantID, device.DeviceTemplateID)
	if err != nil {
		return nil, err
	}
	items, _, err := listThingModelItems(ctx, device.TenantID, tpl.ThingModelID, "", "", pageRequest{Page: 1, PageSize: 500})
	if err != nil {
		return nil, err
	}
	out := make(map[string]ThingModelItem, len(items))
	for _, item := range items {
		out[item.Identifier] = item
	}
	return out, nil
}

func (b *mqttBridge) publishCommandDown(deviceSN string, req *CommandPayload) error {
	if b == nil || b.client == nil {
		return fmt.Errorf("mqtt bridge not configured")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	topic := fmt.Sprintf("devices/%s/command/down", deviceSN)
	token := b.client.Publish(topic, 0, false, body)
	if !token.WaitTimeout(3 * time.Second) {
		return fmt.Errorf("publish timeout")
	}
	return token.Error()
}

func (b *mqttBridge) latestForDevice(deviceID string) map[string]any {
	if b == nil {
		return map[string]any{}
	}
	if value, ok := b.latest.Load(deviceID); ok {
		if typed, ok := value.(map[string]any); ok {
			return typed
		}
	}
	return map[string]any{}
}
