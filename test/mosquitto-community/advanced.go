package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	bc "project/internal/brokercontract"
)

func (r *runner) testFreshConnectionNonce() error {
	const wireID = "qa-reused-wire-client-id"
	var records []fixtureAuth
	for i := 0; i < 2; i++ {
		c, err := r.connect(wireID, "qa-device-device-a", 4)
		if err != nil {
			return err
		}
		c.disconnect()
		auth, err := r.authRecord("qa-device-device-a", wireID, i+1)
		if err != nil {
			return err
		}
		records = append(records, auth)
	}
	if records[0].ConnectionNonce == "" || records[1].ConnectionNonce == "" ||
		records[0].ConnectionNonce == records[1].ConnectionNonce || records[0].ConnectionID == records[1].ConnectionID ||
		records[0].ConnectionID != fixtureConnectionID(records[0].ConnectionNonce) || records[1].ConnectionID != fixtureConnectionID(records[1].ConnectionNonce) {
		return fmt.Errorf("new authentication nonce did not create a distinct connection: %+v", records)
	}
	return nil
}

func (r *runner) testCommandFrame(version byte) error {
	label := versionLabel(version)
	device, err := r.connect("qa-command-device-a-"+label, "qa-command-device-a", version)
	if err != nil {
		return err
	}
	defer device.disconnect()
	filter := "devices/command/device-a/+"
	codes, err := device.subscribe(filter)
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		return fmt.Errorf("device command filter SUBACK=%v err=%v", codes, err)
	}
	other, err := r.connect("qa-command-device-b-"+label, "qa-command-device-b", version)
	if err != nil {
		return err
	}
	defer other.disconnect()
	denied, err := other.subscribe(filter)
	wantDenied := byte(0x80)
	if version == 5 {
		wantDenied = 0x87
	}
	if err != nil || len(denied) != 1 || denied[0] != wantDenied {
		return fmt.Errorf("cross-device command SUBACK=%v want %#x err=%v", denied, wantDenied, err)
	}
	service, err := r.connect("qa-command-service-"+label, "qa-command-service", version)
	if err != nil {
		return err
	}
	defer service.disconnect()
	topic := "devices/command/device-a/qa-" + label
	wirePayload := []byte{0x00, 0xff, '{', '}', 0x80, 0x00}
	frame, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "qa-" + label, TenantID: "qa-tenant", TargetDeviceID: "device-a", RouteEpoch: 1, MappingVersion: "fixture-map-v1", WireTopic: topic, ExpiresAt: time.Now().Add(20 * time.Second).UnixMilli(), Payload: wirePayload}, time.Now())
	if err != nil {
		return err
	}
	if reason, err := service.publish(topic, frame, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("valid command frame publish reason=%#x err=%v", reason, err)
	}
	gotTopic, got, err := device.readPublish(3 * time.Second)
	if err != nil || gotTopic != topic || !bytes.Equal(got, wirePayload) || bc.IsCommandFrame(got) {
		return fmt.Errorf("device command delivery topic=%q payload=%x err=%v", gotTopic, got, err)
	}
	r.recordCommandCase("mqtt-"+label+"-command-immediate", "device received exact opaque wire bytes; private frame was stripped")
	// A frame with the wrong route epoch can be stored by WRITE but must be
	// rejected at READ; broker PUBACK is recorded separately from delivery.
	badTopic := "devices/command/device-a/qa-bad-" + label
	bad, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "qa-bad-" + label, TenantID: "qa-tenant", TargetDeviceID: "device-a", RouteEpoch: 2, MappingVersion: "fixture-map-v1", WireTopic: badTopic, ExpiresAt: time.Now().Add(20 * time.Second).UnixMilli(), Payload: []byte("stale-route")}, time.Now())
	if err != nil {
		return err
	}
	if reason, err := service.publish(badTopic, bad, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("stale-route frame broker publish reason=%#x err=%v", reason, err)
	}
	if err := expectNoPacket(device, 250*time.Millisecond); err != nil {
		return fmt.Errorf("stale-route command reached device after broker ACK: %w", err)
	}
	r.recordCommandCase("mqtt-"+label+"-command-online-stale-route", "PUBACK observed; wrong-epoch frame was not delivered")
	// Bare canonical command publications are denied by the service ACL path.
	bareTopic := "devices/command/device-a/qa-bare-" + label
	if err := r.expectPublishDenied(version, bareTopic, false); err != nil {
		return err
	}
	if err := expectNoPacket(device, 250*time.Millisecond); err != nil {
		return fmt.Errorf("bare command reached device: %w", err)
	}
	r.recordCommandCase("mqtt-"+label+"-command-bare-denied", "unauthorized bare command was not delivered")
	device.disconnect()
	if err := r.testOfflineCommandFrames(version, service, label); err != nil {
		r.issues = append(r.issues, fmt.Sprintf("MQTT %d offline command queue/expiry failed: %v", version, err))
		fmt.Printf("FAIL: MQTT %d offline command queue/expiry: %v\n", version, err)
	} else {
		fmt.Printf("PASS: MQTT %d offline command delivery and expired-frame suppression\n", version)
	}
	fmt.Printf("PASS: MQTT %d immediate command exact-byte delivery, cross-device filter denial, stale-route suppression, bare command denial\n", version)
	return nil
}

func (r *runner) testOfflineCommandFrames(version byte, service *mqttClient, label string) error {
	clientID, username, filter := "qa-command-offline-"+label, "qa-command-device-a", "devices/command/device-a/+"
	device, present, err := r.connectSession(clientID, username, version, false, 60)
	if err != nil {
		return err
	}
	if present != 0 {
		device.disconnect()
		return errors.New("first persistent session unexpectedly resumed")
	}
	codes, err := device.subscribe(filter)
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		device.disconnect()
		return fmt.Errorf("offline command SUBACK=%v err=%v", codes, err)
	}
	device.disconnect()
	expiredTopic := "devices/command/device-a/qa-offline-expired-" + label
	frame, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "qa-offline-expired-" + label, TenantID: "qa-tenant", TargetDeviceID: "device-a", RouteEpoch: 1, MappingVersion: "fixture-map-v1", WireTopic: expiredTopic, ExpiresAt: time.Now().Add(900 * time.Millisecond).UnixMilli(), Payload: []byte("expired-offline")}, time.Now())
	if err != nil {
		return err
	}
	if reason, err := service.publish(expiredTopic, frame, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("expired offline command broker ACK reason=%#x err=%v", reason, err)
	}
	time.Sleep(1100 * time.Millisecond)
	device, present, err = r.connectSession(clientID, username, version, false, 60)
	if err != nil {
		return err
	}
	if present != 1 {
		device.disconnect()
		return errors.New("expired-frame reconnect did not resume persistent session")
	}
	if err := expectNoPacket(device, 300*time.Millisecond); err != nil {
		device.disconnect()
		return fmt.Errorf("expired offline frame was delivered: %w", err)
	}
	r.recordCommandCase("mqtt-"+label+"-command-offline-expired", "persistent session resumed; expired stored frame was not delivered")
	device.disconnect()
	wrongEpochTopic := "devices/command/device-a/qa-offline-wrong-epoch-" + label
	wrongEpoch, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "qa-offline-wrong-epoch-" + label, TenantID: "qa-tenant", TargetDeviceID: "device-a", RouteEpoch: 2, MappingVersion: "fixture-map-v1", WireTopic: wrongEpochTopic, ExpiresAt: time.Now().Add(20 * time.Second).UnixMilli(), Payload: []byte("stale-epoch")}, time.Now())
	if err != nil {
		return err
	}
	if reason, err := service.publish(wrongEpochTopic, wrongEpoch, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("offline stale-epoch publish reason=%#x err=%v", reason, err)
	}
	device, present, err = r.connectSession(clientID, username, version, false, 60)
	if err != nil {
		return err
	}
	if present != 1 {
		device.disconnect()
		return errors.New("stale-epoch reconnect did not resume persistent session")
	}
	if err := expectNoPacket(device, 300*time.Millisecond); err != nil {
		device.disconnect()
		return fmt.Errorf("offline stale-epoch frame reached device: %w", err)
	}
	r.recordCommandCase("mqtt-"+label+"-command-offline-stale-route", "persistent session resumed; wrong-epoch stored frame was not delivered")
	device.disconnect()
	validTopic := "devices/command/device-a/qa-offline-valid-" + label
	wire := []byte{0x00, 0xfe, 'o', 'f', 'f', 'l', 'i', 'n', 'e'}
	frame, err = bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "qa-offline-valid-" + label, TenantID: "qa-tenant", TargetDeviceID: "device-a", RouteEpoch: 1, MappingVersion: "fixture-map-v1", WireTopic: validTopic, ExpiresAt: time.Now().Add(20 * time.Second).UnixMilli(), Payload: wire}, time.Now())
	if err != nil {
		return err
	}
	if reason, err := service.publish(validTopic, frame, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("valid offline command broker ACK reason=%#x err=%v", reason, err)
	}
	device, present, err = r.connectSession(clientID, username, version, false, 60)
	if err != nil {
		return err
	}
	defer device.disconnect()
	if present != 1 {
		return errors.New("valid-frame reconnect did not resume persistent session")
	}
	gotTopic, got, err := device.readPublish(3 * time.Second)
	if err != nil || gotTopic != validTopic || !bytes.Equal(got, wire) || bc.IsCommandFrame(got) {
		return fmt.Errorf("offline command delivery topic=%q bytes=%x err=%v", gotTopic, got, err)
	}
	r.recordCommandCase("mqtt-"+label+"-command-offline-valid", "persistent session resumed; exact wire payload delivered and frame stripped")
	if err := r.testOfflineCommandPrincipalMismatch(version, service, label); err != nil {
		return err
	}
	if err := r.testOfflineCommandRevoked(version, service, label); err != nil {
		return err
	}
	if err := r.testExpiredCommandAcrossRestart(version, service, label); err != nil {
		return err
	}
	if err := r.testQueuedCommandIdentityAfterRestart(version, label); err != nil {
		return err
	}
	return nil
}

func (r *runner) testOfflineCommandPrincipalMismatch(version byte, service *mqttClient, label string) error {
	clientID, filter := "qa-command-principal-mismatch-"+label, "devices/command/device-a/+"
	device, _, err := r.connectSession(clientID, "qa-command-device-a", version, false, 60)
	if err != nil {
		return err
	}
	codes, err := device.subscribe(filter)
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		device.disconnect()
		return fmt.Errorf("mismatch setup SUBACK=%v err=%v", codes, err)
	}
	device.disconnect()
	topic := "devices/command/device-a/qa-principal-mismatch-" + label
	frame, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "qa-principal-mismatch-" + label, TenantID: "qa-tenant", TargetDeviceID: "device-a", RouteEpoch: 1, MappingVersion: "fixture-map-v1", WireTopic: topic, ExpiresAt: time.Now().Add(20 * time.Second).UnixMilli(), Payload: []byte("must-not-cross-principal")}, time.Now())
	if err != nil {
		return err
	}
	if reason, err := service.publish(topic, frame, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("principal mismatch frame publish reason=%#x err=%v", reason, err)
	}
	device, present, err := r.connectSession(clientID, "qa-command-device-b", version, false, 60)
	if err != nil {
		return err
	}
	defer device.disconnect()
	if present != 1 {
		return errors.New("principal-mismatch reconnect did not resume persistent session")
	}
	if err := expectNoPacket(device, 350*time.Millisecond); err != nil {
		return fmt.Errorf("command frame crossed to different principal: %w", err)
	}
	r.recordCommandCase("mqtt-"+label+"-command-offline-principal-mismatch", "restored session under different device principal received no frame")
	return nil
}

func (r *runner) testOfflineCommandRevoked(version byte, service *mqttClient, label string) error {
	clientID, filter := "qa-command-revoked-"+label, "devices/command/device-a/+"
	device, _, err := r.connectSession(clientID, "qa-command-device-a", version, false, 60)
	if err != nil {
		return err
	}
	codes, err := device.subscribe(filter)
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		device.disconnect()
		return fmt.Errorf("revocation setup SUBACK=%v err=%v", codes, err)
	}
	device.disconnect()
	topic := "devices/command/device-a/qa-revoked-" + label
	frame, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "qa-revoked-" + label, TenantID: "qa-tenant", TargetDeviceID: "device-a", RouteEpoch: 1, MappingVersion: "fixture-map-v1", WireTopic: topic, ExpiresAt: time.Now().Add(20 * time.Second).UnixMilli(), Payload: []byte("must-not-survive-revocation")}, time.Now())
	if err != nil {
		return err
	}
	if reason, err := service.publish(topic, frame, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("revoked frame publish reason=%#x err=%v", reason, err)
	}
	device, present, err := r.connectSession(clientID, "qa-command-revoked-device-a", version, false, 60)
	if err != nil {
		return err
	}
	defer device.disconnect()
	if present != 1 {
		return errors.New("revoked reconnect did not resume persistent session")
	}
	if err := expectNoPacket(device, 350*time.Millisecond); err != nil {
		return fmt.Errorf("revoked command permission still delivered queued frame: %w", err)
	}
	r.recordCommandCase("mqtt-"+label+"-command-offline-revoked", "restored session without command-subscribe permission received no frame")
	return nil
}

func (r *runner) testExpiredCommandAcrossRestart(version byte, service *mqttClient, label string) error {
	clientID, filter := "qa-command-restart-expiry-"+label, "devices/command/device-a/+"
	device, _, err := r.connectSession(clientID, "qa-command-device-a", version, false, 60)
	if err != nil {
		return err
	}
	codes, err := device.subscribe(filter)
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		device.disconnect()
		return fmt.Errorf("restart expiry setup SUBACK=%v err=%v", codes, err)
	}
	device.disconnect()
	topic := "devices/command/device-a/qa-restart-expired-" + label
	expires := time.Now().Add(4 * time.Second)
	frame, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "qa-restart-expired-" + label, TenantID: "qa-tenant", TargetDeviceID: "device-a", RouteEpoch: 1, MappingVersion: "fixture-map-v1", WireTopic: topic, ExpiresAt: expires.UnixMilli(), Payload: []byte("expired-after-restart")}, time.Now())
	if err != nil {
		return err
	}
	if reason, err := service.publish(topic, frame, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("restart-expiry frame publish reason=%#x err=%v", reason, err)
	}
	// Allow autosave_interval=1 to persist the queued MQTT message.
	time.Sleep(1200 * time.Millisecond)
	if err := r.requestBrokerRestart(); err != nil {
		return err
	}
	if wait := time.Until(expires) + 150*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	device, present, err := r.connectSession(clientID, "qa-command-device-a", version, false, 60)
	if err != nil {
		return err
	}
	defer device.disconnect()
	if present != 1 {
		return errors.New("restart did not restore the persistent command subscription session")
	}
	if err := expectNoPacket(device, 500*time.Millisecond); err != nil {
		return fmt.Errorf("persisted frame expired after broker restart was delivered: %w", err)
	}
	r.recordCommandCase("mqtt-"+label+"-command-restart-expired", "broker restart restored session; frame expired during restart and delivered no payload")
	return nil
}

func (r *runner) requestBrokerRestart() error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Post(r.fixtureURL+"/__test/restart-broker", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("restart request status=%s", resp.Status)
	}
	deadline := time.Now().Add(8 * time.Second)
	downSeen := false
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", r.addr, 100*time.Millisecond)
		if err != nil {
			downSeen = true
		} else {
			_ = conn.Close()
			if downSeen {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("broker restart did not produce a down/up transition (down observed=%t)", downSeen)
}

func (r *runner) testQueuedCommandIdentityAfterRestart(version byte, label string) error {
	service, err := r.connect("qa-command-restart-identity-service-"+label, "qa-command-service", version)
	if err != nil {
		return err
	}
	defer service.disconnect()
	filter := "devices/command/device-a/+"
	clientIDs := []string{"qa-command-restart-mismatch-" + label, "qa-command-restart-revoked-" + label}
	for _, clientID := range clientIDs {
		c, _, err := r.connectSession(clientID, "qa-command-device-a", version, false, 60)
		if err != nil {
			return err
		}
		codes, err := c.subscribe(filter)
		if err != nil || len(codes) != 1 || codes[0] != 1 {
			c.disconnect()
			return fmt.Errorf("restart identity setup SUBACK=%v err=%v", codes, err)
		}
		c.disconnect()
	}
	topic := "devices/command/device-a/qa-restart-identity-" + label
	frame, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "qa-restart-identity-" + label, TenantID: "qa-tenant", TargetDeviceID: "device-a", RouteEpoch: 1, MappingVersion: "fixture-map-v1", WireTopic: topic, ExpiresAt: time.Now().Add(25 * time.Second).UnixMilli(), Payload: []byte("must-not-cross-after-restart")}, time.Now())
	if err != nil {
		return err
	}
	if reason, err := service.publish(topic, frame, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("restart identity frame publish reason=%#x err=%v", reason, err)
	}
	time.Sleep(1200 * time.Millisecond)
	if err := r.requestBrokerRestart(); err != nil {
		return err
	}
	for i, identity := range []string{"qa-command-device-b", "qa-command-revoked-device-a"} {
		c, present, err := r.connectSession(clientIDs[i], identity, version, false, 60)
		if err != nil {
			return err
		}
		if present != 1 {
			c.disconnect()
			return fmt.Errorf("restart identity session %q did not resume", identity)
		}
		if err := expectNoPacket(c, 400*time.Millisecond); err != nil {
			c.disconnect()
			return fmt.Errorf("persisted command reached %s after broker restart: %w", identity, err)
		}
		r.recordCommandCase("mqtt-"+label+"-command-restart-identity-"+identity, "broker restart restored session; mismatched or revoked principal received no payload")
		c.disconnect()
	}
	return nil
}

func (r *runner) recordCommandCase(name, evidence string) {
	r.commandCases = append(r.commandCases, commandLedgerCase{Case: name, Evidence: evidence})
}

func (r *runner) testNarrowServiceFilter(version byte) error {
	service, err := r.connect("qa-narrow-service-"+versionLabel(version), "qa-service", version)
	if err != nil {
		return err
	}
	defer service.disconnect()
	narrow := bc.IngressPrefix + "/cWEtdGVuYW50/ZGV2aWNlLWE/#"
	codes, err := service.subscribe(narrow)
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		return fmt.Errorf("authorized narrow filter SUBACK=%v err=%v", codes, err)
	}
	denied, err := service.subscribe("telemetry/device-a/#")
	wantDenied := byte(0x80)
	if version == 5 {
		wantDenied = 0x87
	}
	if err != nil || len(denied) != 1 || denied[0] != wantDenied {
		return fmt.Errorf("outside service policy SUBACK=%v want %#x err=%v", denied, wantDenied, err)
	}
	legacyDenied, err := service.subscribe("$tp/ingress/v1/#")
	if err != nil || len(legacyDenied) != 1 || legacyDenied[0] != wantDenied {
		return fmt.Errorf("legacy internal prefix SUBACK=%v want %#x err=%v", legacyDenied, wantDenied, err)
	}
	device, err := r.connect("qa-narrow-device-"+versionLabel(version), "qa-device-device-a", version)
	if err != nil {
		return err
	}
	defer device.disconnect()
	topic, payload := "telemetry/device-a/narrow-filter", []byte("narrow-subscription-delivery")
	if reason, err := device.publish(topic, payload, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("device publish reason=%#x err=%v", reason, err)
	}
	if err := r.checkIngress(service, topic, payload); err != nil {
		return err
	}
	r.ingress = append(r.ingress, "mqtt-"+versionLabel(version)+"-service-narrow-filter")
	return nil
}

func (r *runner) testSharedServiceFilter(version byte) error {
	service, err := r.connect("qa-shared-service-"+versionLabel(version), "qa-shared-service", version)
	if err != nil {
		return err
	}
	defer service.disconnect()
	deniedFilter := "$share/rogue/" + bc.IngressPrefix + "/#"
	denied, err := service.subscribe(deniedFilter)
	wantDenied := byte(0x80)
	if version == 5 {
		wantDenied = 0x87
	}
	if err != nil || len(denied) != 1 || denied[0] != wantDenied {
		return fmt.Errorf("unlisted shared group SUBACK=%v want %#x err=%v", denied, wantDenied, err)
	}
	allowedFilter := "$share/qa-workers/" + bc.IngressPrefix + "/#"
	codes, err := service.subscribe(allowedFilter)
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		return fmt.Errorf("explicit shared group SUBACK=%v err=%v", codes, err)
	}
	device, err := r.connect("qa-shared-device-"+versionLabel(version), "qa-device-device-a", version)
	if err != nil {
		return err
	}
	defer device.disconnect()
	topic, payload := "telemetry/device-a/shared", []byte("shared-subscription-delivery")
	if reason, err := device.publish(topic, payload, 1, false); err != nil || reason != 0 {
		return fmt.Errorf("device publish reason=%#x err=%v", reason, err)
	}
	if err := r.checkIngress(service, topic, payload); err != nil {
		return err
	}
	r.ingress = append(r.ingress, "mqtt-"+versionLabel(version)+"-explicit-shared-group")
	return nil
}

func (r *runner) testImmediateWill(version byte) error {
	label := versionLabel(version)
	service, err := r.connect("qa-will-service-"+label, "qa-will-service", version)
	if err != nil {
		return err
	}
	defer service.disconnect()
	filters := []string{bc.IngressPrefix + "/#", bc.StatePrefix + "/#", "telemetry/device-a/#"}
	codes, err := service.subscribe(filters...)
	if err != nil || len(codes) != len(filters) {
		return fmt.Errorf("Will service SUBACK=%v err=%v", codes, err)
	}
	for i, code := range codes {
		if code != 1 {
			return fmt.Errorf("Will service filter %q denied with %#x", filters[i], code)
		}
	}

	clientID := "qa-immediate-will-" + label
	wireTopic := "telemetry/device-a/will/abrupt-" + label
	wirePayload := []byte{0x00, 0xff, 'w', 'i', 'l', 'l', 0xe6, 0xb8, 0xa9}
	device, err := dialMQTT(r.addr, version)
	if err != nil {
		return err
	}
	reason, err := device.connectWithWill(clientID, "qa-will-device", &mqttWill{topic: wireTopic, payload: wirePayload, qos: 1})
	if err != nil || reason != 0 {
		device.close()
		return fmt.Errorf("CONNECT with Will reason=%#x err=%v", reason, err)
	}
	auth, err := r.authRecord("qa-will-device", clientID, 1)
	if err != nil {
		device.close()
		return err
	}
	stateTopic, _ := bc.StateTopic(bc.Principal{TenantID: "qa-tenant", DeviceID: "device-a", Kind: bc.PrincipalDevice})
	if err := r.readStateFor(service, stateTopic, "device-a", auth.ConnectionID, 1, true); err != nil {
		device.close()
		return fmt.Errorf("Will client online state: %w", err)
	}
	device.close() // abrupt TCP close must trigger an immediate Will

	gotIngress, gotWire, gotOffline := false, false, false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (!gotIngress || !gotWire || !gotOffline) {
		topic, payload, retained, err := service.readPublishDetail(time.Until(deadline))
		if err != nil {
			return fmt.Errorf("read Will or offline state: %w", err)
		}
		switch topic {
		case wireTopic:
			if !bytes.Equal(payload, wirePayload) || retained {
				return fmt.Errorf("wire Will changed: payload=%x retained=%t", payload, retained)
			}
			gotWire = true
		case func() string { t, _ := bc.IngressTopic("qa-tenant", "device-a", []byte(wireTopic)); return t }():
			if err := validateWillIngress(topic, payload, wireTopic, wirePayload, auth.ConnectionID); err != nil {
				return err
			}
			gotIngress = true
		case stateTopic:
			var state bc.StateObservation
			if err := json.Unmarshal(payload, &state); err != nil {
				return err
			}
			if state.ConnectionID == auth.ConnectionID && state.EventSeq == 2 && !state.Online && state.Principal.DeviceID == "device-a" {
				gotOffline = true
			}
		}
	}
	if !gotWire || !gotIngress || !gotOffline {
		return fmt.Errorf("missing Will effects: trusted_ingress=%t original_wire=%t offline_state=%t", gotIngress, gotWire, gotOffline)
	}
	r.states = append(r.states, "mqtt-"+label+"-will-online-seq1", "mqtt-"+label+"-will-offline-seq2")
	return nil
}

func validateWillIngress(topic string, payload []byte, wireTopic string, wirePayload []byte, connectionID string) error {
	expectedTopic, err := bc.IngressTopic("qa-tenant", "device-a", []byte(wireTopic))
	if err != nil {
		return err
	}
	if topic != expectedTopic {
		return fmt.Errorf("trusted Will topic=%q want %q", topic, expectedTopic)
	}
	var env struct {
		DeviceID string `json:"device_id"`
		Values   string `json:"values"`
		Metadata struct {
			TenantID         string `json:"tenant_id"`
			ConnectionID     string `json:"connection_id"`
			OriginalTopicB64 string `json:"original_topic_b64"`
			OriginalQoS      byte   `json:"original_qos"`
			OriginalRetain   bool   `json:"original_retain"`
		} `json:"_tp"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return err
	}
	if env.DeviceID != "device-a" || env.Metadata.TenantID != "qa-tenant" || env.Metadata.ConnectionID != connectionID ||
		env.Metadata.OriginalQoS != 1 || env.Metadata.OriginalRetain ||
		env.Metadata.OriginalTopicB64 != base64.StdEncoding.EncodeToString([]byte(wireTopic)) ||
		env.Values != base64.StdEncoding.EncodeToString(wirePayload) {
		return fmt.Errorf("trusted Will envelope mismatched connection/payload/topic: %+v", env)
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(env.Values)
	if err != nil || !bytes.Equal(decoded, wirePayload) {
		return errors.New("Will payload did not round-trip exactly once as base64")
	}
	return nil
}

func (r *runner) readStateFor(service *mqttClient, topic, deviceID, connectionID string, seq uint64, online bool) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		t, body, err := service.readPublish(time.Until(deadline))
		if err != nil {
			return err
		}
		if t != topic {
			continue
		}
		var state bc.StateObservation
		if err := json.Unmarshal(body, &state); err != nil {
			return err
		}
		if state.ConnectionID == connectionID {
			if state.EventSeq != seq || state.Online != online || state.Principal.TenantID != "qa-tenant" || state.Principal.DeviceID != deviceID || state.Principal.Kind != bc.PrincipalDevice || state.BrokerCell != qaCell || state.BrokerInstance != qaInstance || state.NodeBootID == "" {
				return fmt.Errorf("unexpected trusted state: %+v", state)
			}
			return nil
		}
	}
	return errors.New("timed out waiting for matching state observation")
}

func (r *runner) willObserver(version byte, suffix string, subscribeState bool) (*mqttClient, error) {
	clientID := fmt.Sprintf("qa-will-observer-%s-%s", versionLabel(version), suffix)
	service, err := r.connect(clientID, "qa-will-service", version)
	if err != nil {
		return nil, err
	}
	filters := []string{bc.IngressPrefix + "/#", "telemetry/device-a/#"}
	if subscribeState {
		filters = append(filters, bc.StatePrefix+"/#")
	}
	codes, err := service.subscribe(filters...)
	if err != nil {
		service.close()
		return nil, err
	}
	for i, code := range codes {
		if code != 1 {
			service.close()
			return nil, fmt.Errorf("observer filter %q rejected %#x", filters[i], code)
		}
	}
	return service, nil
}

func (r *runner) connectWill(clientID, username string, version byte, will *mqttWill) (*mqttClient, error) {
	c, err := dialMQTT(r.addr, version)
	if err != nil {
		return nil, err
	}
	reason, err := c.connectWithWill(clientID, username, will)
	if err != nil || reason != 0 {
		c.close()
		return nil, fmt.Errorf("CONNECT with Will rejected reason=%#x err=%v", reason, err)
	}
	return c, nil
}

func (r *runner) testGracefulWill(version byte) error {
	service, err := r.willObserver(version, "graceful", false)
	if err != nil {
		return err
	}
	defer service.disconnect()
	device, err := r.connectWill("qa-graceful-will-"+versionLabel(version), "qa-will-device", version, &mqttWill{topic: "telemetry/device-a/will/graceful", payload: []byte("must-not-publish"), qos: 1})
	if err != nil {
		return err
	}
	device.disconnect()
	if err := expectNoPacket(service, 500*time.Millisecond); err != nil {
		return fmt.Errorf("graceful disconnect emitted Will: %w", err)
	}
	return nil
}

func (r *runner) testInternalWillDenied(version byte) error {
	service, err := r.willObserver(version, "internal", false)
	if err != nil {
		return err
	}
	defer service.disconnect()
	device, err := r.connectWill("qa-forged-will-"+versionLabel(version), "qa-will-device", version, &mqttWill{topic: bc.IngressPrefix + "/forged", payload: []byte("must-not-pass"), qos: 1})
	if err != nil {
		return err
	}
	device.close()
	if err := expectNoPacket(service, 500*time.Millisecond); err != nil {
		return fmt.Errorf("forged internal Will reached subscriber: %w", err)
	}
	return nil
}

func (r *runner) testCommandWillDenied(version byte) error {
	label := versionLabel(version)
	observer, err := r.connect("qa-command-will-observer-"+label, "qa-command-device-a", version)
	if err != nil {
		return err
	}
	defer observer.disconnect()
	codes, err := observer.subscribe("devices/command/device-a/+")
	if err != nil || len(codes) != 1 || codes[0] != 1 {
		return fmt.Errorf("command Will observer SUBACK=%v err=%v", codes, err)
	}
	topic := "devices/command/device-a/qa-will-injected-" + label
	frame, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "qa-will-injected-" + label, TenantID: "qa-tenant", TargetDeviceID: "device-a", RouteEpoch: 1, MappingVersion: "fixture-map-v1", WireTopic: topic, ExpiresAt: time.Now().Add(20 * time.Second).UnixMilli(), Payload: []byte("will-must-not-become-command")}, time.Now())
	if err != nil {
		return err
	}
	attacker, err := r.connectWill("qa-command-will-attacker-"+label, "qa-command-device-a", version, &mqttWill{topic: topic, payload: frame, qos: 1})
	if err != nil {
		return err
	}
	attacker.close() // abrupt close attempts the private frame as a broker Will
	if err := expectNoPacket(observer, 500*time.Millisecond); err != nil {
		return fmt.Errorf("command-frame Will injection reached device: %w", err)
	}
	return nil
}

func (r *runner) testExpiredAndRevokedWill() error {
	for _, tc := range []struct {
		user, client string
		version      byte
		wait         time.Duration
	}{
		{"qa-will-expired", "qa-expired-will", 4, 1300 * time.Millisecond},
		{"qa-will-revoked", "qa-revoked-will", 4, 0},
	} {
		service, err := r.willObserver(tc.version, tc.client, false)
		if err != nil {
			return err
		}
		device, err := r.connectWill(tc.client, tc.user, tc.version, &mqttWill{topic: "telemetry/device-a/will/" + tc.client, payload: []byte("must-not-pass"), qos: 1})
		if err != nil {
			service.close()
			return err
		}
		if tc.user == "qa-will-revoked" {
			if err := r.waitCounter(tc.user, 2, false, 10*time.Second); err != nil {
				device.close()
				service.close()
				return err
			}
			// The fixture records the request before returning its denial.
			time.Sleep(150 * time.Millisecond)
		} else {
			time.Sleep(tc.wait)
		}
		device.close()
		if err := expectNoPacket(service, 500*time.Millisecond); err != nil {
			service.close()
			return fmt.Errorf("%s Will was delivered: %w", tc.user, err)
		}
		service.disconnect()
	}
	return nil
}

func (r *runner) testDelayedWillDenied() error {
	service, err := r.willObserver(5, "delayed", false)
	if err != nil {
		return err
	}
	defer service.disconnect()
	device, err := r.connectWill("qa-delayed-will", "qa-will-device", 5, &mqttWill{topic: "telemetry/device-a/will/delayed", payload: []byte("must-not-pass"), qos: 1, delaySecond: 7})
	if err != nil {
		return err
	}
	device.close()
	// Delay exceeds the five-second closed-principal grant window; only immediate
	// Wills are implemented. A delayed/restored Will must never inherit that lease.
	if err := expectNoPacket(service, 8500*time.Millisecond); err != nil {
		return fmt.Errorf("delayed Will escaped its grant window: %w", err)
	}
	return nil
}

type retainedSample struct {
	topic     string
	wireTopic string
	payload   []byte
	deviceID  string
}

func (r *runner) testPlatformRetained(version byte) error {
	label := versionLabel(version)
	service, err := r.connect("qa-retained-live-"+label, "qa-retained-service", version)
	if err != nil {
		return err
	}
	defer service.disconnect()
	if codes, err := service.subscribeWithOptions(0x01, bc.IngressPrefix+"/#"); err != nil || len(codes) != 1 || codes[0] != 1 {
		return fmt.Errorf("retained live subscriber SUBACK=%v err=%v", codes, err)
	}
	samples := []retainedSample{
		{wireTopic: "telemetry/device-a/snapshot", payload: []byte(`{"source":"a","kind":"snapshot"}`), deviceID: "device-a"},
		{wireTopic: "telemetry/device-a/config", payload: []byte{0x00, 0xff, 'a'}, deviceID: "device-a"},
		{wireTopic: "telemetry/device-b/snapshot", payload: []byte(`{"source":"b","kind":"snapshot"}`), deviceID: "device-b"},
	}
	devA, err := r.connect("qa-retained-publisher-a-"+label, "qa-retained-device-device-a", version)
	if err != nil {
		return err
	}
	defer devA.disconnect()
	devB, err := r.connect("qa-retained-publisher-b-"+label, "qa-retained-device-device-b", version)
	if err != nil {
		return err
	}
	defer devB.disconnect()
	publishers := map[string]*mqttClient{"device-a": devA, "device-b": devB}
	for i := range samples {
		s := &samples[i]
		s.topic, err = bc.IngressTopic("qa-tenant", s.deviceID, []byte(s.wireTopic))
		if err != nil {
			return err
		}
		if reason, err := publishers[s.deviceID].publish(s.wireTopic, s.payload, 1, true); err != nil || reason != 0 {
			return fmt.Errorf("retained publish %q reason=%#x err=%v", s.wireTopic, reason, err)
		}
		if err := checkRetainedDelivery(service, *s, false, false); err != nil {
			return fmt.Errorf("live retained publish %q: %w", s.wireTopic, err)
		}
	}
	probeA, err := r.connect("qa-retained-snapshot-a-"+label, "qa-retained-service", version)
	if err != nil {
		return err
	}
	defer probeA.disconnect()
	filtersA := []string{samples[0].topic, samples[1].topic}
	if codes, err := probeA.subscribeWithOptions(0x01, filtersA...); err != nil || len(codes) != len(filtersA) {
		return fmt.Errorf("snapshot A SUBACK=%v err=%v", codes, err)
	}
	if err := checkRetainedDelivery(probeA, samples[0], true, false); err != nil {
		return fmt.Errorf("snapshot A first: %w", err)
	}
	if err := checkRetainedDelivery(probeA, samples[1], true, false); err != nil {
		return fmt.Errorf("snapshot A second: %w", err)
	}
	if err := expectNoPacket(probeA, 150*time.Millisecond); err != nil {
		return fmt.Errorf("device B snapshot crossed into device A subscription: %w", err)
	}
	probeB, err := r.connect("qa-retained-snapshot-b-"+label, "qa-retained-service", version)
	if err != nil {
		return err
	}
	defer probeB.disconnect()
	if codes, err := probeB.subscribeWithOptions(0x01, samples[2].topic); err != nil || len(codes) != 1 || codes[0] != 1 {
		return fmt.Errorf("snapshot B SUBACK=%v err=%v", codes, err)
	}
	if err := checkRetainedDelivery(probeB, samples[2], true, false); err != nil {
		return fmt.Errorf("snapshot B: %w", err)
	}
	if err := expectNoPacket(probeB, 150*time.Millisecond); err != nil {
		return fmt.Errorf("device A snapshot crossed into device B subscription: %w", err)
	}

	for _, s := range samples {
		if reason, err := publishers[s.deviceID].publish(s.wireTopic, []byte{}, 1, true); err != nil || reason != 0 {
			return fmt.Errorf("retained clear %q reason=%#x err=%v", s.wireTopic, reason, err)
		}
		if err := checkRetainedClear(service, s.topic); err != nil {
			return fmt.Errorf("live clear %q: %w", s.wireTopic, err)
		}
		probe := probeA
		if s.deviceID == "device-b" {
			probe = probeB
		}
		if err := checkRetainedClear(probe, s.topic); err != nil {
			return fmt.Errorf("snapshot reader clear %q: %w", s.wireTopic, err)
		}
	}
	lateA, err := r.connect("qa-retained-late-a-"+label, "qa-retained-service", version)
	if err != nil {
		return err
	}
	defer lateA.disconnect()
	if _, err := lateA.subscribeWithOptions(0x01, filtersA...); err != nil {
		return err
	}
	if err := expectNoPacket(lateA, 350*time.Millisecond); err != nil {
		return fmt.Errorf("cleared A retained snapshot replayed: %w", err)
	}
	lateB, err := r.connect("qa-retained-late-b-"+label, "qa-retained-service", version)
	if err != nil {
		return err
	}
	defer lateB.disconnect()
	if _, err := lateB.subscribeWithOptions(0x01, samples[2].topic); err != nil {
		return err
	}
	if err := expectNoPacket(lateB, 350*time.Millisecond); err != nil {
		return fmt.Errorf("cleared B retained snapshot replayed: %w", err)
	}
	r.ingress = append(r.ingress, "mqtt-"+label+"-platform-retained-clear-and-isolation")
	return nil
}

func checkRetainedDelivery(c *mqttClient, sample retainedSample, retained, empty bool) error {
	topic, payload, gotRetained, err := c.readPublishDetail(3 * time.Second)
	if err != nil {
		return err
	}
	if topic != sample.topic || gotRetained != retained {
		return fmt.Errorf("topic=%q retained=%t want topic=%q retained=%t", topic, gotRetained, sample.topic, retained)
	}
	if empty {
		if len(payload) != 0 {
			return fmt.Errorf("clear payload has %d bytes", len(payload))
		}
		return nil
	}
	var env struct {
		DeviceID string `json:"device_id"`
		Values   string `json:"values"`
		Metadata struct {
			TenantID         string `json:"tenant_id"`
			PrincipalKind    string `json:"principal_kind"`
			OriginalTopicB64 string `json:"original_topic_b64"`
		} `json:"_tp"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return err
	}
	if env.DeviceID != sample.deviceID || env.Metadata.TenantID != "qa-tenant" || env.Metadata.PrincipalKind != string(bc.PrincipalDevice) ||
		env.Metadata.OriginalTopicB64 != base64.StdEncoding.EncodeToString([]byte(sample.wireTopic)) || env.Values != base64.StdEncoding.EncodeToString(sample.payload) {
		return fmt.Errorf("retained envelope mismatch for %q: %+v", sample.wireTopic, env)
	}
	return nil
}

func checkRetainedClear(c *mqttClient, topic string) error {
	gotTopic, payload, retained, err := c.readPublishDetail(3 * time.Second)
	if err != nil {
		return err
	}
	if gotTopic != topic || len(payload) != 0 || retained {
		return fmt.Errorf("clear delivery topic=%q bytes=%d retained=%t, want %q empty retain=false", gotTopic, len(payload), retained, topic)
	}
	return nil
}

func (r *runner) noWillDelivery(service *mqttClient, wait time.Duration) error {
	err := expectNoPacket(service, wait)
	if err == nil {
		return nil
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return nil
	}
	return err
}
