package mqttadapter

import (
	"context"
	"errors"
	"project/internal/brokercommands"
	bc "project/internal/brokercontract"
	"project/internal/downlink"
	"time"
)

// PublishDurableCommand preserves the final mapped wire bytes inside a private
// frame. Broker READ/MESSAGE_OUT checks expiry and route before unwrapping.
func (a *Adapter) PublishDurableCommand(ctx context.Context, row brokercommands.Row, msg *downlink.Message, payload []byte) error {
	if a.inputProfile != "ingress_v1" || msg == nil || row.RouteEpoch <= 0 || row.ID == "" || msg.TopicPrefix != "" {
		return errors.New("durable MQTT command profile unavailable")
	}
	if !row.ExpiresAt.After(time.Now()) {
		return errors.New("command expired before publish")
	}
	var topic string
	switch msg.Type {
	case downlink.MessageTypeCommand:
		topic = BuildCommandTopic(msg.DeviceNumber, row.ID)
		if msg.DeviceType != "1" {
			topic = BuildGatewayCommandTopic(msg.DeviceNumber, row.ID)
		}
	case downlink.MessageTypeAttributeSet:
		topic = BuildAttributeSetTopic(msg.DeviceNumber, row.ID)
		if msg.DeviceType != "1" {
			topic = BuildGatewayAttributeSetTopic(msg.DeviceNumber, row.ID)
		}
	case downlink.MessageTypeAttributeGet:
		topic = BuildAttributeGetTopic(msg.DeviceNumber)
		if msg.DeviceType != "1" {
			topic = BuildGatewayAttributeGetTopic(msg.DeviceNumber)
		}
	case downlink.MessageTypeTelemetry:
		topic = BuildTelemetryControlTopic(msg.DeviceNumber)
		if msg.DeviceType != "1" {
			topic = BuildGatewayTelemetryControlTopic(msg.DeviceNumber)
		}
	default:
		return errors.New("unsupported durable downlink family")
	}
	version, ok := a.rememberedMapping(row.TargetDeviceID)
	if !ok {
		return ErrMappingVersionUnavailable
	}
	// Encoded bytes were frozen by the command owner. Applying a subsequently
	// remembered custom mapping here would reinterpret an already approved
	// intent. Custom command mapping needs a separately frozen mapping proof.
	if version != "standard-v1" {
		return errors.New("custom command mappings are unsupported for durable commands")
	}
	frame, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: row.ID, TenantID: row.TenantID, TargetDeviceID: row.TargetDeviceID, RouteEpoch: uint64(row.RouteEpoch), MappingVersion: version, WireTopic: topic, ExpiresAt: row.ExpiresAt.UnixMilli(), Payload: payload}, time.Now())
	if err != nil {
		return err
	}
	// Do not queue a new publish while the service connection is reconnecting.
	// A disconnect after this check remains ambiguous and is never auto-retried.
	if !a.mqttClient.IsConnectionOpen() {
		return errors.New("command transport is disconnected")
	}
	token := a.mqttClient.Publish(topic, 1, false, frame)
	return WaitTokenContext(ctx, token, 5*time.Second)
}
