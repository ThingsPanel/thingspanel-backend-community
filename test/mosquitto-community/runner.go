package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	bc "project/internal/brokercontract"
)

const (
	qaCell     = "qa-community"
	qaInstance = "mosquitto-plugin-qa"
)

type runner struct {
	addr         string
	fixtureURL   string
	timeout      time.Duration
	rawPath      string
	ingress      []string
	states       []string
	issues       []string
	commandCases []commandLedgerCase
}

func (r *runner) run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := waitHTTP(ctx, r.addr); err != nil {
		return err
	}
	for _, version := range []byte{4, 5} {
		if err := r.testIngressAndACL(version); err != nil {
			return fmt.Errorf("MQTT %d wire contract: %w", version, err)
		}
		if err := r.testMaxIngressBoundary(version); err != nil {
			return fmt.Errorf("MQTT %d maximum ingress boundary: %w", version, err)
		}
		label := "3.1.1"
		if version == 5 {
			label = "5"
		}
		fmt.Printf("PASS: MQTT %s binary/Unicode/empty and max-boundary ingress, identity, ACL, and live-ingress retain denial\n", label)
	}
	if err := r.testAuthReject(); err != nil {
		return fmt.Errorf("explicit auth rejection: %w", err)
	}
	fmt.Println("PASS: fixture denial is rejected at CONNECT")
	if err := r.testPreAuthDisconnect(); err != nil {
		r.issues = append(r.issues, "immediate pre-CONNACK disconnect cancellation unsupported on this Mosquitto 2.1.2 path: "+err.Error())
		fmt.Println("UNSUPPORTED: pre-CONNACK client close did not immediately cancel the owner request")
	} else {
		fmt.Println("OBSERVED: pre-CONNACK close canceled the owner request in this run; not treated as a capability guarantee")
	}
	if err := r.testAuthTimeoutCancellationAndLateResponse(); err != nil {
		return fmt.Errorf("bounded auth cancellation and late response: %w", err)
	}
	fmt.Println("PASS: owner hard timeout cancels delayed request, releases pending capacity, and late response cannot bind a reused ClientID")
	if err := r.testAuthTimeout(); err != nil {
		return fmt.Errorf("auth timeout: %w", err)
	}
	fmt.Println("PASS: owner HTTP timeout fails closed")
	if err := r.testPendingLimit(); err != nil {
		return fmt.Errorf("pending-auth limit: %w", err)
	}
	fmt.Println("PASS: concurrent delayed CONNECTs remain bounded by MaxPending")
	if err := r.testRefreshRevocation(); err != nil {
		return fmt.Errorf("grant refresh revocation: %w", err)
	}
	fmt.Println("PASS: refreshed denial blocks delivery to an existing subscription")
	if err := r.testStateEvents(); err != nil {
		return fmt.Errorf("trusted connection state: %w", err)
	}
	fmt.Println("PASS: trusted connect/offline state uses stable connection ID and sequence 1/2")
	if err := r.testFreshConnectionNonce(); err != nil {
		return fmt.Errorf("fresh connection identity: %w", err)
	}
	fmt.Println("PASS: fresh connections get distinct nonce-bound connection IDs")
	for _, version := range []byte{4, 5} {
		if err := r.testNarrowServiceFilter(version); err != nil {
			return fmt.Errorf("MQTT %d narrow service filter: %w", version, err)
		}
		if err := r.testSharedServiceFilter(version); err != nil {
			return fmt.Errorf("MQTT %d shared service filter: %w", version, err)
		}
		fmt.Printf("PASS: MQTT %d service narrow-filter and explicit shared-group authorization\n", version)
	}
	for _, version := range []byte{4, 5} {
		if err := r.testCommandFrame(version); err != nil {
			return fmt.Errorf("MQTT %d durable command frame delivery: %w", version, err)
		}
	}
	for _, version := range []byte{4, 5} {
		if err := r.testImmediateWill(version); err != nil {
			return fmt.Errorf("MQTT %d immediate Will: %w", version, err)
		}
		if err := r.testGracefulWill(version); err != nil {
			return fmt.Errorf("MQTT %d graceful Will: %w", version, err)
		}
		if err := r.testInternalWillDenied(version); err != nil {
			return fmt.Errorf("MQTT %d forged internal Will: %w", version, err)
		}
		if err := r.testCommandWillDenied(version); err != nil {
			return fmt.Errorf("MQTT %d command-frame Will injection: %w", version, err)
		}
		if err := r.testPlatformRetained(version); err != nil {
			return fmt.Errorf("MQTT %d platform retained: %w", version, err)
		}
		fmt.Printf("PASS: MQTT %d Will and platform-retained snapshot/clear/RAP0 checks\n", version)
	}
	if err := r.testExpiredAndRevokedWill(); err != nil {
		return fmt.Errorf("expired/revoked Will: %w", err)
	}
	if err := r.testDelayedWillDenied(); err != nil {
		return fmt.Errorf("delayed Will denial: %w", err)
	}
	fmt.Println("PASS: expired/revoked Wills denied; delayed Will fail-closed observation only (unsupported)")
	r.states = []string{"state-online-connection-seq1", "state-offline-connection-seq2"}
	if err := writeRunLedger(r.rawPath, time.Now().UTC().Format(time.RFC3339Nano), r.ingress, r.states, r.commandCases); err != nil {
		return fmt.Errorf("write raw QA ledger: %w", err)
	}
	if len(r.issues) > 0 {
		return fmt.Errorf("known unsupported/failing checks: %s", strings.Join(r.issues, "; "))
	}
	return nil
}

func (r *runner) runSmoke() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := waitHTTP(ctx, r.addr); err != nil {
		return err
	}
	service, err := r.connect("asan-smoke-service", "qa-service", 4)
	if err != nil {
		return err
	}
	defer service.disconnect()
	codes, err := service.subscribe(bc.IngressPrefix + "/#")
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		return fmt.Errorf("smoke service SUBACK=%v err=%v", codes, err)
	}
	device, err := r.connect("asan-smoke-device", "qa-device-device-a", 4)
	if err != nil {
		return err
	}
	defer device.disconnect()
	topic, payload := "telemetry/device-a/asan-smoke", []byte{0, 0xff, 'A', 'S', 'A', 'N', 0x80}
	if reason, err := device.publish(topic, payload, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("smoke PUBLISH reason=%#x err=%v", reason, err)
	}
	if err := r.checkIngress(service, topic, payload); err != nil {
		return err
	}
	if err := writeRunLedger(r.rawPath, time.Now().UTC().Format(time.RFC3339Nano), []string{"asan-mqtt-ingress-smoke"}, nil, nil); err != nil {
		return err
	}
	fmt.Println("PASS: sanitizer MQTT ingress smoke (C broker + C shim + Go c-shared callbacks)")
	return nil
}

func (r *runner) testIngressAndACL(version byte) error {
	proto := versionLabel(version)
	service, err := r.connect(proto+"-service", "qa-service", version)
	if err != nil {
		return err
	}
	defer service.disconnect()
	serviceCodes, err := service.subscribe(bc.IngressPrefix + "/#")
	if err != nil || len(serviceCodes) != 1 || serviceCodes[0] != 1 {
		return fmt.Errorf("service ingress SUBACK=%v err=%v", serviceCodes, err)
	}

	device, err := r.connect(proto+"-device-a", "qa-device-device-a", version)
	if err != nil {
		return err
	}
	defer device.disconnect()
	filters := []string{"commands/device-a/#", "commands/device-b/#", bc.IngressPrefix + "/#"}
	for i := 0; i < 17; i++ {
		filters = append(filters, fmt.Sprintf("commands/other-%02d/#", i))
	}
	codes, err := device.subscribe(filters...)
	if err != nil {
		return err
	}
	denied := byte(0x80)
	if version == 5 {
		denied = 0x87
	}
	if len(codes) != 20 || codes[0] != 1 {
		return fmt.Errorf("mixed SUBACK codes=%#v, expected 20 entries with granted first filter", codes)
	}
	for i := 1; i < len(codes); i++ {
		if codes[i] != denied {
			return fmt.Errorf("mixed SUBACK filter %q code=%#x, want %#x", filters[i], codes[i], denied)
		}
	}
	// Reverse ordering proves a denied filter does not mask a later allowed one.
	reverse, err := device.subscribe("commands/device-b/#", "commands/device-a/#")
	if err != nil || len(reverse) != 2 || reverse[0] != denied || reverse[1] != 1 {
		return fmt.Errorf("reverse mixed SUBACK codes=%#v err=%v", reverse, err)
	}

	topic := "telemetry/device-a/温度"
	payload := []byte{0x00, 0xff, 'T', 'P', '-', 0xe6, 0xb8, 0xa9, 0x00}
	if reason, err := device.publish(topic, payload, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("valid binary publish reason=%#x err=%v", reason, err)
	}
	if err := r.checkIngress(service, topic, payload); err != nil {
		return err
	}
	r.ingress = append(r.ingress, "mqtt-"+versionLabel(version)+"-binary-unicode-ingress")
	if reason, err := device.publish("telemetry/device-a/empty", []byte{}, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("empty publish reason=%#x err=%v", reason, err)
	}
	if err := r.checkIngress(service, "telemetry/device-a/empty", []byte{}); err != nil {
		return err
	}
	r.ingress = append(r.ingress, "mqtt-"+versionLabel(version)+"-empty-ingress")

	for _, deniedPublish := range []struct {
		topic  string
		retain bool
	}{
		{topic: "telemetry/device-b/forged"},
		{topic: bc.IngressPrefix + "/forged"},
		{topic: "telemetry/device-a/retained", retain: true},
	} {
		if err := r.expectPublishDenied(version, deniedPublish.topic, deniedPublish.retain); err != nil {
			return err
		}
		if err := expectNoPacket(service, 200*time.Millisecond); err != nil {
			return fmt.Errorf("denied publish reached ingress: %w", err)
		}
	}
	return nil
}

func (r *runner) testStateEvents() error {
	service, err := r.connect("qa-state-service", "qa-service", 4)
	if err != nil {
		return err
	}
	defer service.disconnect()
	codes, err := service.subscribe(bc.StatePrefix + "/#")
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		return fmt.Errorf("service state SUBACK=%v err=%v", codes, err)
	}
	device, err := r.connect("qa-state-device", "qa-device-state-device", 4)
	if err != nil {
		return err
	}
	wantedTopic, err := bc.StateTopic(bc.Principal{TenantID: "qa-tenant", DeviceID: "state-device", Kind: bc.PrincipalDevice})
	if err != nil {
		device.close()
		return err
	}
	auth, err := r.authRecord("qa-device-state-device", "qa-state-device", 1)
	if err != nil {
		device.close()
		return err
	}
	wantedConnectionID := auth.ConnectionID
	if err := r.readState(service, wantedTopic, wantedConnectionID, 1, true); err != nil {
		device.close()
		return fmt.Errorf("online state: %w", err)
	}
	device.disconnect()
	if err := r.readState(service, wantedTopic, wantedConnectionID, 2, false); err != nil {
		return fmt.Errorf("offline state: %w", err)
	}
	return nil
}

func (r *runner) testMaxIngressBoundary(version byte) error {
	label := versionLabel(version)
	service, err := r.connect("max-boundary-service-"+label, "qa-service", version)
	if err != nil {
		return err
	}
	defer service.disconnect()
	codes, err := service.subscribe(bc.IngressPrefix + "/#")
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		return fmt.Errorf("maximum-boundary service SUBACK=%v err=%v", codes, err)
	}
	device, err := r.connect("max-boundary-device-"+label, "qa-device-device-a", version)
	if err != nil {
		return err
	}
	defer device.disconnect()
	const topicPrefix = "telemetry/device-a/"
	if len(topicPrefix) >= bc.MaxWireTopicBytes {
		return fmt.Errorf("test topic prefix exceeds MaxWireTopicBytes=%d", bc.MaxWireTopicBytes)
	}
	topic := topicPrefix + strings.Repeat("x", bc.MaxWireTopicBytes-len(topicPrefix))
	if len([]byte(topic)) != bc.MaxWireTopicBytes {
		return fmt.Errorf("wire topic length=%d, want %d", len([]byte(topic)), bc.MaxWireTopicBytes)
	}
	payload := bytes.Repeat([]byte{0x00, 0xff, 0x7f, 'M'}, bc.MaxPayloadBytes/4)
	if len(payload) != bc.MaxPayloadBytes {
		return fmt.Errorf("payload length=%d, want %d", len(payload), bc.MaxPayloadBytes)
	}
	if reason, err := device.publish(topic, payload, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("maximum-boundary PUBLISH reason=%#x err=%v", reason, err)
	}
	if err := r.checkIngressTimeout(service, topic, payload, 15*time.Second); err != nil {
		return fmt.Errorf("maximum-boundary envelope round trip: %w", err)
	}
	r.ingress = append(r.ingress, "mqtt-"+label+"-max-payload-max-topic-ingress")
	fmt.Printf("PASS: MQTT %s transmitted %d-byte payload and %d-byte wire topic through hashed ingress topic\n", label, len(payload), len([]byte(topic)))
	return nil
}

func (r *runner) readState(service *mqttClient, wantedTopic, connectionID string, seq uint64, online bool) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		topic, body, err := service.readPublish(time.Until(deadline))
		if err != nil {
			return err
		}
		if topic != wantedTopic {
			continue // state notifications from other fixture clients are unrelated
		}
		var state bc.StateObservation
		if err := json.Unmarshal(body, &state); err != nil {
			return fmt.Errorf("decode state observation: %w", err)
		}
		if state.ConnectionID != connectionID || state.EventSeq != seq || state.Online != online ||
			state.Principal.TenantID != "qa-tenant" || state.Principal.DeviceID != "state-device" ||
			state.Principal.Kind != bc.PrincipalDevice || state.BrokerCell != qaCell || state.BrokerInstance != qaInstance || state.NodeBootID == "" {
			return fmt.Errorf("unexpected trusted state: %+v", state)
		}
		return nil
	}
	return errors.New("timed out waiting for matching state observation")
}

func (r *runner) connect(clientID, username string, version byte) (*mqttClient, error) {
	c, err := dialMQTT(r.addr, version)
	if err != nil {
		return nil, err
	}
	reason, err := c.connect(clientID, username)
	if err != nil {
		c.close()
		return nil, err
	}
	if reason != 0 {
		c.close()
		return nil, fmt.Errorf("CONNECT rejected for %s, reason=%#x", username, reason)
	}
	return c, nil
}

func (r *runner) connectSession(clientID, username string, version byte, clean bool, expiry uint32) (*mqttClient, byte, error) {
	c, err := dialMQTT(r.addr, version)
	if err != nil {
		return nil, 0xff, err
	}
	present, reason, err := c.connectSession(clientID, username, clean, expiry)
	if err != nil || reason != 0 {
		c.close()
		if err == nil {
			err = fmt.Errorf("CONNECT rejected, reason=%#x", reason)
		}
		return nil, present, err
	}
	return c, present, nil
}

func (r *runner) checkIngress(service *mqttClient, originalTopic string, originalPayload []byte) error {
	return r.checkIngressTimeout(service, originalTopic, originalPayload, 3*time.Second)
}

func (r *runner) checkIngressTimeout(service *mqttClient, originalTopic string, originalPayload []byte, timeout time.Duration) error {
	topic, payload, err := service.readPublish(timeout)
	if err != nil {
		return fmt.Errorf("read ingress: %w", err)
	}
	expectedTopic, err := bc.IngressTopic("qa-tenant", "device-a", []byte(originalTopic))
	if err != nil {
		return err
	}
	if topic != expectedTopic {
		return fmt.Errorf("ingress topic=%q, want %q", topic, expectedTopic)
	}
	var envelope struct {
		DeviceID string `json:"device_id"`
		Values   string `json:"values"`
		Metadata struct {
			ContractVersion  string `json:"contract_version"`
			TenantID         string `json:"tenant_id"`
			PrincipalKind    string `json:"principal_kind"`
			BrokerCell       string `json:"broker_cell"`
			BrokerInstance   string `json:"broker_instance"`
			OriginalTopicB64 string `json:"original_topic_b64"`
			OriginalQoS      byte   `json:"original_qos"`
			OriginalRetain   bool   `json:"original_retain"`
		} `json:"_tp"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("invalid ingress envelope: %w", err)
	}
	if envelope.DeviceID != "device-a" || envelope.Metadata.TenantID != "qa-tenant" ||
		envelope.Metadata.ContractVersion != bc.ContractVersion || envelope.Metadata.PrincipalKind != string(bc.PrincipalDevice) ||
		envelope.Metadata.BrokerCell != qaCell || envelope.Metadata.BrokerInstance != qaInstance ||
		envelope.Metadata.OriginalQoS != 1 || envelope.Metadata.OriginalRetain {
		return fmt.Errorf("unexpected trusted envelope metadata: %+v", envelope)
	}
	if envelope.Values != base64.StdEncoding.EncodeToString(originalPayload) {
		return fmt.Errorf("values=%q, want one standard base64 encoding %q", envelope.Values, base64.StdEncoding.EncodeToString(originalPayload))
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(envelope.Values)
	if err != nil || !bytes.Equal(decoded, originalPayload) {
		return errors.New("values do not round-trip exactly once as base64")
	}
	if got, err := base64.StdEncoding.Strict().DecodeString(envelope.Metadata.OriginalTopicB64); err != nil || !bytes.Equal(got, []byte(originalTopic)) {
		return fmt.Errorf("original_topic_b64 is not a valid single standard base64 encoding: %q (%v)", envelope.Metadata.OriginalTopicB64, err)
	}
	return nil
}

func (r *runner) expectPublishDenied(version byte, topic string, retain bool) error {
	c, err := r.connect(versionLabel(version)+"-deny-"+strings.ReplaceAll(topic, "/", "-"), "qa-device-device-a", version)
	if err != nil {
		return err
	}
	defer c.close()
	reason, err := c.publish(topic, []byte("must-not-pass"), 1, retain)
	// MQTT 3.1.1 PUBACK carries no reason code. Mosquitto can deny a publish,
	// omit it from forwarding, and still return the ordinary two-byte PUBACK;
	// the caller validates non-delivery to the ingress subscriber separately.
	if version == 4 && err == nil {
		return nil
	}
	if err == nil && reason == 0 {
		return fmt.Errorf("unauthorized publish to %q was acknowledged as successful", topic)
	}
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return fmt.Errorf("unauthorized publish to %q had no bounded broker response: %w", topic, err)
		}
	}
	return nil
}

func (r *runner) testAuthReject() error {
	c, err := dialMQTT(r.addr, 4)
	if err != nil {
		return err
	}
	reason, err := c.connect("qa-rejected", "qa-deny")
	c.close()
	if err == nil && reason == 0 {
		return errors.New("denied fixture grant received successful CONNACK")
	}
	return r.waitCalls("qa-deny", 1)
}

func (r *runner) testPreAuthDisconnect() error {
	c, err := dialMQTT(r.addr, 4)
	if err != nil {
		return err
	}
	if err := c.connectPacket("qa-preauth-cancel", "qa-slow-cancel"); err != nil {
		c.close()
		return err
	}
	if err := r.waitCalls("qa-slow-cancel", 1); err != nil {
		c.close()
		return err
	}
	c.close()
	time.Sleep(1300 * time.Millisecond) // the 1.2s fixture response is before the driver's 2s hard timeout.
	stats, err := r.stats()
	if err != nil {
		return err
	}
	if stats.Cancelled["qa-slow-cancel"] > 0 {
		return nil
	}
	return errors.New("request was not immediately canceled; 1.2s owner response completed before the 2s hard timeout")
}

func (r *runner) testAuthTimeoutCancellationAndLateResponse() error {
	started := time.Now()
	c, err := dialMQTT(r.addr, 4)
	if err != nil {
		return err
	}
	if err := c.connectPacket("qa-preauth-overdue", "qa-slow-overdue"); err != nil {
		c.close()
		return err
	}
	if err := r.waitCalls("qa-slow-overdue", 1); err != nil {
		c.close()
		return err
	}
	c.close()
	if err := r.waitCancelled("qa-slow-overdue", 1, 4*time.Second); err != nil {
		return err
	}
	if elapsed := time.Since(started); elapsed < 1500*time.Millisecond || elapsed > 4*time.Second {
		return fmt.Errorf("owner timeout cancellation elapsed outside expected bound: %s", elapsed)
	}
	probe, err := r.connect("qa-pending-cap-released", "qa-device-device-a", 4)
	if err != nil {
		return fmt.Errorf("pending capacity not released after timeout: %w", err)
	}
	probe.disconnect()

	old, err := dialMQTT(r.addr, 4)
	if err != nil {
		return err
	}
	if err := old.connectPacket("qa-late-response-reuse", "qa-late-response"); err != nil {
		old.close()
		return err
	}
	if err := r.waitCalls("qa-late-response", 1); err != nil {
		old.close()
		return err
	}
	old.close()
	time.Sleep(2200 * time.Millisecond)
	live, err := r.connect("qa-late-response-reuse", "qa-device-device-a", 4)
	if err != nil {
		return fmt.Errorf("new socket with reused ClientID failed after timeout: %w", err)
	}
	defer live.disconnect()
	if err := expectNoPacket(live, 1800*time.Millisecond); err != nil {
		return fmt.Errorf("late owner response affected reused ClientID socket: %w", err)
	}
	return nil
}

func (r *runner) testAuthTimeout() error {
	c, err := dialMQTT(r.addr, 4)
	if err != nil {
		return err
	}
	started := time.Now()
	reason, err := c.connect("qa-auth-timeout", "qa-timeout")
	c.close()
	if err == nil && reason == 0 {
		return errors.New("timeout fixture received successful CONNACK")
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		return fmt.Errorf("authentication timeout was not bounded: %s", elapsed)
	}
	return r.waitCalls("qa-timeout", 1)
}

func (r *runner) testPendingLimit() error {
	clients := make([]*mqttClient, 4)
	for i := range clients {
		c, err := dialMQTT(r.addr, 4)
		if err != nil {
			return err
		}
		clients[i] = c
		if err := c.connectPacket(fmt.Sprintf("qa-pending-%d", i), fmt.Sprintf("qa-barrier-%d", i)); err != nil {
			for _, x := range clients {
				if x != nil {
					x.close()
				}
			}
			return err
		}
	}
	defer func() {
		for _, c := range clients {
			c.close()
		}
	}()
	deadline := time.Now().Add(1500 * time.Millisecond)
	var overlap testStats
	for time.Now().Before(deadline) {
		stats, err := r.stats()
		if err == nil && stats.BarrierWaiting >= 2 {
			overlap = stats
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if overlap.BarrierWaiting < 2 {
		_ = r.releaseAuthBarrier()
		return fmt.Errorf("fixture did not hold two concurrent owner requests: %+v", overlap)
	}
	if err := r.releaseAuthBarrier(); err != nil {
		return err
	}
	accepted := 0
	for _, c := range clients {
		_ = c.conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		p, err := readPacket(c.conn)
		if err == nil && p.header>>4 == 2 && len(p.body) >= 2 && p.body[1] == 0 {
			accepted++
		}
	}
	if accepted != 2 {
		stats, _ := r.stats()
		return fmt.Errorf("barrier held %d concurrent owner requests (peak=%d); received %d successful CONNACKs, want max_pending=2 (fixture=%+v)", overlap.BarrierWaiting, overlap.BarrierMax, accepted, stats)
	}
	stats, err := r.stats()
	if err != nil {
		return err
	}
	if stats.BarrierMax != 2 || stats.BarrierCalls != 2 {
		return fmt.Errorf("barrier observed max owner overlap=%d owner calls=%d; want both exactly 2", stats.BarrierMax, stats.BarrierCalls)
	}
	fmt.Printf("PASS: MaxPending barrier observed owner IO overlap=%d/max=%d; only %d CONNECTs succeeded (ready/Poll internals remain covered by driver unit tests)\n", overlap.BarrierWaiting, stats.BarrierMax, accepted)
	return nil
}

func (r *runner) releaseAuthBarrier() error {
	resp, err := http.Post(r.fixtureURL+"/__test/barrier/release", "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("barrier release status=%s", resp.Status)
	}
	return nil
}

func (r *runner) testRefreshRevocation() error {
	device, err := r.connect("qa-refresh-existing-device", "qa-refresh-deny", 4)
	if err != nil {
		return err
	}
	defer device.close()
	codes, err := device.subscribe("commands/device-a/#")
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		return fmt.Errorf("initial command subscription codes=%v err=%v", codes, err)
	}
	service, err := r.connect("qa-refresh-service", "qa-service", 4)
	if err != nil {
		return err
	}
	defer service.disconnect()
	if reason, err := service.publish("commands/device-a/initial", []byte("before-refresh"), 1, false); err != nil || reason != 0 {
		return fmt.Errorf("initial service command reason=%#x err=%v", reason, err)
	}
	if _, payload, err := device.readPublish(2 * time.Second); err != nil || string(payload) != "before-refresh" {
		return fmt.Errorf("initial command not delivered before refresh, payload=%q err=%v", payload, err)
	}
	// The driver schedules its first refresh five seconds after authentication.
	if err := r.waitCounter("qa-refresh-deny", 2, false, 10*time.Second); err != nil {
		return err
	}
	first, err := r.authRecord("qa-refresh-deny", "qa-refresh-existing-device", 1)
	if err != nil {
		return err
	}
	second, err := r.authRecord("qa-refresh-deny", "qa-refresh-existing-device", 2)
	if err != nil {
		return err
	}
	if first.ConnectionNonce == "" || first.ConnectionNonce != second.ConnectionNonce || first.ConnectionID != second.ConnectionID {
		return fmt.Errorf("refresh changed connection identity: first=%+v second=%+v", first, second)
	}
	if reason, err := service.publish("commands/device-a/after-refresh", []byte("must-not-deliver"), 1, false); err != nil || reason != 0 {
		return fmt.Errorf("service publish after refresh reason=%#x err=%v", reason, err)
	}
	p, err := device.nextPacket(500 * time.Millisecond)
	if err == nil && p.header>>4 == 3 {
		return errors.New("existing subscription received delivery after refresh denial")
	}
	if err != nil {
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			return fmt.Errorf("unexpected socket result after refresh denial: %w", err)
		}
	}
	return nil
}

type testStats struct {
	Calls          map[string]int `json:"calls"`
	Cancelled      map[string]int `json:"cancelled"`
	Auths          []fixtureAuth  `json:"auths"`
	BarrierCalls   int            `json:"barrier_calls"`
	BarrierWaiting int            `json:"barrier_waiting"`
	BarrierMax     int            `json:"barrier_max"`
}

func (r *runner) stats() (testStats, error) {
	var out testStats
	resp, err := http.Get(r.fixtureURL + "/__test/stats")
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("fixture stats status %d", resp.StatusCode)
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

func (r *runner) authRecord(username, wireID string, call int) (fixtureAuth, error) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stats, err := r.stats()
		if err == nil {
			var matches []fixtureAuth
			for _, auth := range stats.Auths {
				if auth.Username == username && auth.WireClientID == wireID {
					matches = append(matches, auth)
				}
			}
			if len(matches) >= call {
				return matches[call-1], nil
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fixtureAuth{}, fmt.Errorf("fixture auth record %q/%q call %d missing", username, wireID, call)
}

func (r *runner) waitCalls(username string, n int) error {
	return r.waitCounter(username, n, false, 3*time.Second)
}

func (r *runner) waitCancelled(username string, n int, timeout time.Duration) error {
	return r.waitCounter(username, n, true, timeout)
}

func (r *runner) waitCounter(username string, n int, cancelled bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		stats, err := r.stats()
		if err == nil {
			values := stats.Calls
			if cancelled {
				values = stats.Cancelled
			}
			if values[username] >= n {
				return nil
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("fixture %s count for %q did not reach %d", map[bool]string{true: "cancel", false: "request"}[cancelled], username, n)
}

func expectNoPacket(c *mqttClient, wait time.Duration) error {
	p, err := c.nextPacket(wait)
	if err == nil {
		return fmt.Errorf("unexpected MQTT packet header=%#x body=%x", p.header, p.body)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return nil
	}
	return err
}

func versionLabel(version byte) string {
	if version == 5 {
		return "mqtt5"
	}
	return "mqtt311"
}
