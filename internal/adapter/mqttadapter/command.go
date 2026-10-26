package mqttadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"project/internal/brokercommands"
	bc "project/internal/brokercontract"
	"project/internal/downlink"
	"time"
)

// PublishDurableCommand preserves the final mapped wire bytes inside a private
// frame. Broker READ/MESSAGE_OUT checks expiry and route before unwrapping.
func (a *Adapter) PublishDurableCommand(ctx context.Context, row brokercommands.Row, msg *downlink.Message, payload []byte) error {
	if a.inputProfile != "ingress_v1" || msg == nil || row.RouteEpoch <= 0 || row.ID == "" {
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
	canonicalTopic := topic
	if msg.TopicPrefix != "" {
		if !bc.ValidPluginPrefix(msg.TopicPrefix) {
			return errors.New("invalid plugin socket prefix")
		}
		topic = msg.TopicPrefix + topic
		canonicalTopic = topic
	}
	var prepared brokercommands.PreparedCommand
	if err := json.Unmarshal(row.Payload, &prepared); err != nil {
		return errors.New("durable command mapping proof is unavailable")
	}
	if msg.DeviceID != prepared.Message.DeviceID || msg.DeviceNumber != prepared.Message.DeviceNumber || msg.DeviceConfigID != prepared.Message.DeviceConfigID || msg.Type != prepared.Message.Type || msg.TopicPrefix != prepared.Message.TopicPrefix || !bytes.Equal(payload, prepared.Encoded) {
		return errors.New("durable command payload does not match its accepted snapshot")
	}
	version := prepared.MappingVersion
	if version == "" {
		version = "standard-v1" // compatibility for pre-mapping standard durable rows
	}
	var wirePayload = payload
	if version != "standard-v1" {
		if msg.TopicPrefix != "" || row.TargetDeviceID != msg.DeviceID {
			return errors.New("custom mapping is unsupported for plugin-prefixed command routes")
		}
		repo, err := a.mappingRepository()
		if err != nil {
			return err
		}
		snapshot, err := repo.Load(ctx, version)
		if err != nil {
			return ErrMappingVersionUnavailable
		}
		topic, wirePayload, err = mapDownlinkWithDeviceSnapshot(snapshot, msg.DeviceID, msg.DeviceNumber, canonicalTopic, payload)
		if err != nil {
			return err
		}
	}
	canonicalProof := ""
	targetNumberProof := ""
	if version != "standard-v1" {
		canonicalProof = canonicalTopic
		targetNumberProof = msg.DeviceNumber
	}
	frame, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: row.ID, TenantID: row.TenantID, TargetDeviceID: row.TargetDeviceID, TargetDeviceNumber: targetNumberProof, RouteEpoch: uint64(row.RouteEpoch), MappingVersion: version, CanonicalTopic: canonicalProof, WireTopic: topic, ExpiresAt: row.ExpiresAt.UnixMilli(), Payload: wirePayload}, time.Now())
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
