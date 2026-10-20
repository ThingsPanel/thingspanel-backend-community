package mqttadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/spf13/viper"
	"project/initialize"
	bc "project/internal/brokercontract"
	"project/internal/brokerstate"
	"project/internal/uplink"
	"project/pkg/global"
)

// IngressAcceptor must return only after the reliable acceptance transaction has
// committed. Implementations must not equate an in-memory bus send with durability.
type IngressAcceptor interface {
	Accept(context.Context, *uplink.DeviceMessage) error
}

// SetContext is called before publishing this Adapter to reconnect callbacks.
func (a *Adapter) SetContext(ctx context.Context)       { a.ctx = ctx }
func (a *Adapter) SetIngressAcceptor(s IngressAcceptor) { a.ingressAcceptor = s }
func (a *Adapter) ConfigureIngressFromConfig() error {
	a.inputProfile = viper.GetString("mqtt.input_profile")
	if a.inputProfile == "" {
		a.inputProfile = "legacy"
	}
	switch a.inputProfile {
	case "legacy":
		return nil
	case "ingress_v1":
		a.ingressTrust = bc.TrustedMetadata{BrokerCell: viper.GetString("mqtt.broker_cell"), BrokerInstance: viper.GetString("mqtt.broker_instance")}
		if a.ingressTrust.BrokerCell == "" || a.ingressTrust.BrokerInstance == "" {
			return errors.New("ingress_v1 requires broker_cell and broker_instance")
		}
		a.retainedProfile = bc.RetainedProfile(viper.GetString("mqtt.retained_profile"))
		if a.retainedProfile == "" {
			a.retainedProfile = bc.RetainedLiveIngress
		}
		if a.retainedProfile != bc.RetainedLiveIngress && a.retainedProfile != bc.RetainedPlatform {
			return errors.New("uncertified retained profile")
		}
		return a.initMapper()
	default:
		return fmt.Errorf("unsupported MQTT input profile %q", a.inputProfile)
	}
}
func (a *Adapter) adapterContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// RegisterIngressRoutes must run before CONNECT when resuming a persistent
// session. A broker may send queued packets immediately after CONNACK, before
// an asynchronous OnConnect callback or a new SUBSCRIBE can install handlers.
func (a *Adapter) RegisterIngressRoutes(client mqtt.Client) {
	if a.inputProfile == "ingress_v1" {
		client.AddRoute(bc.IngressPrefix+"/#", a.handleIngressMessage)
		client.AddRoute(bc.StatePrefix+"/#", a.handleStateMessage)
	}
}

// SubscribeAllContext selects exactly one business ingress. Legacy raw and v1
// subscriptions cannot be active together in one configured Adapter.
func (a *Adapter) SubscribeAllContext(ctx context.Context, client mqtt.Client) error {
	if a.inputProfile == "ingress_v1" {
		// A previous legacy durable session must not leak raw subscriptions across
		// profiles. Operators must use a new service ClientID at cutover.
		token := client.Subscribe(genSharedTopic(bc.IngressPrefix+"/#"), 1, a.handleIngressMessage)
		if err := WaitTokenContext(ctx, token, 5*time.Second); err != nil {
			return err
		}
		token = client.Subscribe(genSharedTopic(bc.StatePrefix+"/#"), 1, a.handleStateMessage)
		return WaitTokenContext(ctx, token, 5*time.Second)
	}
	if err := a.SubscribeResponseTopics(client); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.SubscribeDeviceTopics(client); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.SubscribeGatewayTopics(client)
}
func (a *Adapter) handleIngressMessage(_ mqtt.Client, m mqtt.Message) {
	a.consumeTrustedMessage(m, func() error { return a.HandleIngressMessage(m.Topic(), m.Payload(), m.Retained()) })
}

// A failed durable transaction never acknowledges the broker->adapter hop.
// The broker's bounded inflight window supplies backpressure while the same
// handler retries; shutdown cancellation leaves the persistent delivery pending.
func (a *Adapter) consumeTrustedMessage(m mqtt.Message, handle func() error) {
	delay := 100 * time.Millisecond
	for {
		if a.adapterContext().Err() != nil {
			return
		}
		if err := handle(); err == nil {
			m.Ack()
			return
		} else {
			a.logger.WithError(err).WithField("topic", m.Topic()).Warn("Trusted MQTT delivery pending; not acknowledged")
		}
		timer := time.NewTimer(delay)
		select {
		case <-a.adapterContext().Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		if delay < 2*time.Second {
			delay *= 2
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
		}
	}
}

// HandleIngressMessage verifies the authenticated transport before interpreting
// any business JSON. Retained snapshots and transport clears have no live effect.
func (a *Adapter) HandleIngressMessage(topic string, body []byte, retained bool) error {
	if a.inputProfile != "ingress_v1" {
		return errors.New("ingress_v1 is not enabled")
	}
	if len(body) == 0 {
		if a.retainedProfile != bc.RetainedPlatform {
			return bc.ErrInvalidEnvelope
		}
		parts := strings.Split(topic, "/")
		if len(parts) != 6 || strings.Join(parts[:3], "/") != bc.IngressPrefix {
			return bc.ErrInvalidTopic
		}
		// Empty clears carry no identity/epoch; they never enter business dispatch.
		if parts[3] == "" || parts[4] == "" || parts[5] == "" {
			return bc.ErrInvalidTopic
		}
		return nil
	}
	env, err := bc.ParseIngress(topic, body, a.ingressTrust)
	if err != nil {
		return err
	}
	if retained || env.Metadata.RetainedReplay {
		return nil
	}
	var pluginCanonicalTopic string
	var pluginIngress bool
	env, pluginCanonicalTopic, pluginIngress, err = a.normalizePluginIngress(env)
	if err != nil {
		return err
	}
	if pluginIngress && isPluginStatusTopic([]pluginTopicCandidate{{canonical: pluginCanonicalTopic}}) {
		return a.handleNormalizedPluginStatus(env, pluginCanonicalTopic)
	}
	device, err := initialize.GetDeviceCacheById(env.DeviceID)
	if err != nil {
		return err
	}
	if device.TenantID != env.Metadata.TenantID {
		return bc.ErrUnauthorized
	}
	wire, err := base64.StdEncoding.DecodeString(env.Metadata.OriginalTopicB64)
	if err != nil {
		return err
	}
	wireTopic := string(wire)
	canonicalTopic := pluginCanonicalTopic
	if !pluginIngress {
		canonicalTopic, err = a.mapIngress(env.Metadata.MappingVersion, device, wireTopic)
		if err != nil {
			return err
		}
	}
	if err = validateCanonicalTopicScope(canonicalTopic, device.DeviceNumber); err != nil {
		return a.rejectTrustedIngress(topic, body, env, "mapped_topic_scope_denied", err)
	}
	if err = a.RememberMapping(device.ID, env.Metadata.MappingVersion); err != nil {
		return err
	}
	kind, messageID, err := classifyIngressTopic(canonicalTopic)
	if err != nil {
		return a.rejectTrustedIngress(topic, body, env, "unsupported_business_topic", err)
	}
	if len(env.Values) == 0 {
		return a.rejectTrustedIngress(topic, body, env, "empty_business_payload", errors.New("empty business payload"))
	}
	if (strings.HasPrefix(kind, "gateway_") && env.Metadata.PrincipalKind != bc.PrincipalGateway) || env.Metadata.PrincipalKind == bc.PrincipalService {
		return bc.ErrUnauthorized
	}
	metadata := map[string]interface{}{"device_id": env.DeviceID, "topic": canonicalTopic, "original_topic": wireTopic, "source_protocol": "mqtt", "transport_profile": "ingress_v1", "broker_cell": env.Metadata.BrokerCell, "broker_instance": env.Metadata.BrokerInstance, "connection_id": env.Metadata.ConnectionID, "route_epoch": env.Metadata.RouteEpoch, "policy_version": env.Metadata.PolicyVersion, "mapping_version": env.Metadata.MappingVersion, "observed_at": env.Metadata.ObservedAt, "message_id": messageID}
	if env.Metadata.IngestionID != "" {
		metadata["ingestion_id"] = env.Metadata.IngestionID
	}
	payload := env.Values
	switch kind {
	case uplink.MessageTypeCommandResponse, uplink.MessageTypeGatewayCommandResponse:
		// Resolve by authenticated device and tenant. message_id alone is correlation,
		// never proof of who published the ACK.
		logicalDevice, lookupErr := a.commandResponseScope(env.Metadata.TenantID, env.DeviceID, messageID, env.Metadata.RouteEpoch)
		if lookupErr != nil {
			if errors.Is(lookupErr, bc.ErrUnauthorized) {
				return a.rejectTrustedIngress(topic, body, env, "command_ack_scope_denied", lookupErr)
			}
			return lookupErr
		}
		var ackDevice string
		payload, ackDevice, err = decodeCommandAckPayload(env.Values)
		if err != nil {
			return a.rejectTrustedIngress(topic, body, env, "invalid_command_ack", err)
		}
		if ackDevice != "" && ackDevice != env.DeviceID && ackDevice != logicalDevice {
			return a.rejectTrustedIngress(topic, body, env, "command_ack_payload_scope_denied", bc.ErrUnauthorized)
		}
	case uplink.MessageTypeAttributeSetResponse, uplink.MessageTypeGatewayAttributeSetResponse:
		var ack struct {
			Result *int `json:"result"`
		}
		if err = json.Unmarshal(payload, &ack); err != nil || ack.Result == nil || (*ack.Result != 0 && *ack.Result != 1) {
			return a.rejectTrustedIngress(topic, body, env, "invalid_attribute_ack", errors.New("invalid attribute ACK"))
		}
	case uplink.MessageTypeStatus:
		// State transitions are emitted through the dedicated authenticated state
		// observer, never accepted from a device's ordinary wire topic.
		return a.rejectTrustedIngress(topic, body, env, "device_supplied_state", errors.New("device supplied status is not a connection observation"))
	}
	msg := &uplink.DeviceMessage{Type: kind, DeviceID: env.DeviceID, TenantID: env.Metadata.TenantID, Timestamp: env.Metadata.ObservedAt.UnixMilli(), Payload: payload, Metadata: metadata}
	reliable := kind == "event" || kind == "gateway_event" || kind == "attribute" || kind == "gateway_attribute" || strings.HasSuffix(kind, "response")
	if reliable {
		if a.ingressAcceptor == nil {
			return errors.New("durable ingress acceptor is unavailable")
		}
		if err = a.ingressAcceptor.Accept(a.adapterContext(), msg); err != nil {
			if reason := permanentAcceptanceReason(err); reason != "" {
				if rejectErr := a.rejectTrustedIngress(topic, body, env, reason, err); rejectErr != nil {
					return rejectErr
				}
				failure := errors.New("business message rejected; see platform receipt diagnostics")
				if kind == "event" || kind == "gateway_event" {
					a.publishEventResponse(device.DeviceNumber, messageID, a.parseEventMethod(payload), failure)
				}
				if kind == "attribute" || kind == "gateway_attribute" {
					a.publishAttributeResponse(device.DeviceNumber, messageID, failure)
				}
				return nil
			}
			return err
		}
		if kind == "event" || kind == "gateway_event" {
			a.publishEventResponse(device.DeviceNumber, messageID, a.parseEventMethod(payload), nil)
		}
		if kind == "attribute" || kind == "gateway_attribute" {
			a.publishAttributeResponse(device.DeviceNumber, messageID, nil)
		}
		return nil
	}
	return a.bus.Publish(msg)
}
func classifyIngressTopic(topic string) (string, string, error) {
	parts := strings.Split(topic, "/")
	if len(parts) < 2 {
		return "", "", bc.ErrInvalidTopic
	}
	prefix := ""
	if parts[0] == "gateway" {
		prefix = "gateway_"
	} else if parts[0] != "devices" {
		return "", "", bc.ErrInvalidTopic
	}
	switch {
	case len(parts) == 2 && parts[1] == "telemetry":
		return prefix + "telemetry", "", nil
	case len(parts) == 3 && parts[1] == "telemetry" && parts[2] != "":
		return prefix + "telemetry", parts[2], nil
	case len(parts) == 4 && parts[1] == "telemetry" && parts[2] != "" && parts[3] != "":
		return prefix + "telemetry", parts[3], nil
	case len(parts) == 3 && (parts[1] == "event" || parts[1] == "attributes") && parts[2] != "":
		kind := parts[1]
		if kind == "attributes" {
			kind = "attribute"
		}
		return prefix + kind, parts[2], nil
	case len(parts) == 4 && parts[1] == "command" && parts[2] == "response" && parts[3] != "":
		return prefix + "command_response", parts[3], nil
	case len(parts) == 5 && parts[1] == "attributes" && parts[2] == "set" && parts[3] == "response" && parts[4] != "":
		return prefix + "attribute_set_response", parts[4], nil
	}
	return "", "", bc.ErrInvalidTopic
}

func validateCanonicalTopicScope(topic, deviceNumber string) error {
	parts := strings.Split(topic, "/")
	if len(parts) == 4 && (parts[0] == "devices" || parts[0] == "gateway") && parts[1] == "telemetry" && parts[2] != deviceNumber {
		return bc.ErrUnauthorized
	}
	return nil
}

func (a *Adapter) handleStateMessage(_ mqtt.Client, m mqtt.Message) {
	a.consumeTrustedMessage(m, func() error { return a.HandleStateMessage(m.Topic(), m.Payload(), m.Retained()) })
}
func (a *Adapter) HandleStateMessage(topic string, body []byte, retained bool) error {
	if a.inputProfile != "ingress_v1" || retained {
		return errors.New("state profile or replay is not allowed")
	}
	observation, err := bc.ParseState(topic, body, a.ingressTrust)
	if err != nil {
		return err
	}
	decision, err := brokerstate.New(global.DB).Apply(a.adapterContext(), observation)
	if err != nil {
		return err
	}
	if !decision.Changed || !decision.Fresh {
		return nil
	}
	if observation.Online {
		if err = a.RememberMapping(observation.Principal.DeviceID, observation.MappingVersion); err != nil {
			return err
		}
	}
	value := []byte("0")
	if decision.Status == "online" {
		value = []byte("1")
	}
	return a.bus.Publish(&uplink.DeviceMessage{Type: uplink.MessageTypeStatus, DeviceID: observation.Principal.DeviceID, TenantID: observation.Principal.TenantID, Timestamp: observation.ObservedAt.UnixMilli(), Payload: value, Metadata: map[string]interface{}{"device_id": observation.Principal.DeviceID, "source": "broker_connection", "source_protocol": "mqtt", "transport_profile": "ingress_v1", "connection_id": observation.ConnectionID, "node_boot_id": observation.NodeBootID, "event_seq": observation.EventSeq, "route_epoch": observation.RouteEpoch}})
}
