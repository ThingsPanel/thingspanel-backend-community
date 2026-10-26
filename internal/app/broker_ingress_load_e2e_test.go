package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqttadapter "project/internal/adapter/mqttadapter"
	bc "project/internal/brokercontract"
	"project/internal/model"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	nativeE2ELoadConnections = 100
	nativeE2ELoadRate        = 100
)

type nativeLoadEvent struct {
	ID                   string    `json:"event_id"`
	DeviceID             string    `json:"planned_device_id"`
	ObservedDeviceIDs    []string  `json:"observed_device_ids,omitempty"`
	ObservationCount     int       `json:"observer_delivery_count"`
	ObservedIngestionIDs []string  `json:"observed_ingestion_ids,omitempty"`
	ObservedReceiptIDs   []string  `json:"observed_receipt_ids,omitempty"`
	PayloadBytes         int       `json:"payload_bytes"`
	PlannedAt            time.Time `json:"planned_at"`
	SentAt               time.Time `json:"sent_at,omitempty"`
	BrokerAckAt          time.Time `json:"broker_ack_at,omitempty"`
	BrokerAckError       string    `json:"broker_ack_error,omitempty"`
	GeneratorLagMS       float64   `json:"generator_lag_ms"`
	IngressSeenAt        time.Time `json:"ingress_seen_at,omitempty"`
	IngestionID          string    `json:"ingestion_id,omitempty"`
	ReceiptID            string    `json:"receipt_id,omitempty"`
	Accepted             bool      `json:"accepted"`
	SQLObserved          bool      `json:"sql_observed"`
	SQLRowCount          int       `json:"sql_row_count"`
	EffectIntentSQL      bool      `json:"effect_intent_sql"`
}

type nativeLoadObservation struct {
	At          time.Time
	IngestionID string
	ReceiptID   string
	DeviceID    string
}

type nativeLoadSummary struct {
	Kind                 string        `json:"kind"`
	RunID                string        `json:"run_id"`
	Duration             time.Duration `json:"duration_ns"`
	ConnectionsTarget    int           `json:"connections_target"`
	ConnectionsConnected int           `json:"connections_connected"`
	RateTarget           int           `json:"rate_target_per_second"`
	Planned              int           `json:"planned"`
	Sent                 int           `json:"sent"`
	BrokerACK            int           `json:"broker_ack"`
	IngressSeen          int           `json:"ingress_seen"`
	Accepted             int           `json:"accepted"`
	TerminalSQLRows      int           `json:"terminal_sql_rows"`
	EffectIntentSQL      int           `json:"effect_intent_sql_rows"`
	SideEffectsExecuted  string        `json:"side_effects_executed"`
	MaxGeneratorLagMS    float64       `json:"max_generator_lag_ms"`
	Elapsed              time.Duration `json:"elapsed_ns"`
	StartedAt            time.Time     `json:"started_at"`
	FinishedAt           time.Time     `json:"finished_at"`
	Result               string        `json:"result"`
	WrongDeviceIdentity  int           `json:"wrong_device_identity_events"`
	DuplicateObserver    int           `json:"duplicate_observer_deliveries"`
	ExtraReceiptRows     int           `json:"extra_receipt_rows"`
	ExtraObserverRows    int           `json:"extra_observer_deliveries"`
	ExtraSQLRows         int           `json:"extra_sql_rows"`
	ExtraEffectRows      int           `json:"extra_effect_rows"`
	LatencyMeasurement   string        `json:"latency_measurement"`
	StageTimesUseLatency bool          `json:"stage_timestamps_are_latency_samples"`
}

func nativeE2ELoadDuration(t *testing.T) time.Duration {
	t.Helper()
	value := strings.TrimSpace(os.Getenv("TP_NATIVE_E2E_LOAD_DURATION"))
	if value == "" {
		return 0
	}
	duration, err := time.ParseDuration(value)
	if err != nil || (duration != 5*time.Second && duration != 60*time.Second) {
		t.Fatalf("TP_NATIVE_E2E_LOAD_DURATION must be 5s or 60s")
	}
	return duration
}

// The baseline e2e broker helper remains capped at 64 sessions. Only this
// opt-in load path raises capacity for 100 device connections plus services.
func startNativeE2ELoadBroker(t *testing.T, binary, plugin, ownerURL, tokenPath, root string, maxPending, maxSessions int) (string, string, func()) {
	t.Helper()
	port := freeNativeE2EPort(t)
	addr := fmt.Sprintf("tcp://127.0.0.1:%d", port)
	dir := t.TempDir()
	configPath, logPath := filepath.Join(dir, "mosquitto.conf"), filepath.Join(dir, "mosquitto.log")
	config := fmt.Sprintf("listener %d 127.0.0.1\nallow_anonymous false\nsys_interval 1\npersistence false\nplugin %s\nlog_dest file %s\nlog_type all\n", port, plugin, logPath)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-c", configPath)
	command.Dir = root
	command.Env = append(os.Environ(),
		"TP_BROKER_OWNER_URL="+ownerURL,
		"TP_BROKER_TOKEN_FILE="+tokenPath,
		"TP_BROKER_CELL="+nativeE2ECell,
		"TP_BROKER_INSTANCE="+nativeE2ENode,
		fmt.Sprintf("TP_BROKER_MAX_PENDING=%d", maxPending),
		fmt.Sprintf("TP_BROKER_MAX_SESSIONS=%d", maxSessions),
		"TP_BROKER_ROUTE_FENCING=1",
		"DYLD_LIBRARY_PATH=/tmp/tp-mqtt-20261019/build/install/lib",
	)
	if err := command.Start(); err != nil {
		t.Fatalf("start private load Mosquitto: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	var stopped atomic.Bool
	stop := func() {
		if stopped.Swap(true) || command.Process == nil {
			return
		}
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-waited:
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
			<-waited
		}
	}
	t.Cleanup(stop)
	waitNativeE2E(t, 8*time.Second, "private load Mosquitto listener", func() bool {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 50*time.Millisecond)
		if err != nil {
			select {
			case exitErr := <-waited:
				t.Fatalf("private load Mosquitto exited: %v; log: %s", exitErr, nativeE2ETail(logPath))
			default:
			}
			return false
		}
		_ = connection.Close()
		return true
	})
	return addr, logPath, stop
}

func runNativeE2ELoad(t *testing.T, ctx context.Context, db *gorm.DB, broker string, duration time.Duration) {
	t.Helper()
	if duration != 5*time.Second && duration != 60*time.Second {
		t.Fatalf("unsupported native load duration %s", duration)
	}
	runID := nativeE2ERandomID(t)
	multiDevice := strings.TrimSpace(os.Getenv("TP_NATIVE_E2E_LOAD_PROFILE")) == "100-devices"
	deviceIDs := []string{nativeE2EDevice}
	if multiDevice {
		deviceIDs = make([]string, nativeE2ELoadConnections)
		for i := range deviceIDs {
			deviceIDs[i] = nativeE2ELoadDeviceID(i)
		}
	}
	observations := make(map[string][]nativeLoadObservation)
	var observationsMu sync.Mutex
	invalidObserverMessages := atomic.Int64{}
	observerOptions := mqtt.NewClientOptions().AddBroker(broker).SetClientID("mqtt-e2e-load-observer").SetUsername("mqtt-e2e-service").SetPassword("e2e-service-password").SetCleanSession(true).SetAutoReconnect(false)
	observer := mqtt.NewClient(observerOptions)
	if err := mqttadapter.WaitTokenContext(ctx, observer.Connect(), 5*time.Second); err != nil {
		t.Fatalf("connect ingress observer for load harness: %v", err)
	}
	defer observer.Disconnect(250)
	callback := func(_ mqtt.Client, message mqtt.Message) {
		if message.Retained() {
			return
		}
		envelope, err := bc.ParseIngress(message.Topic(), message.Payload(), bc.TrustedMetadata{BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode})
		if err != nil || !containsNativeLoadDevice(deviceIDs, envelope.DeviceID) || envelope.Metadata.IngestionID == "" {
			invalidObserverMessages.Add(1)
			return
		}
		var values map[string]json.RawMessage
		if json.Unmarshal(envelope.Values, &values) != nil {
			invalidObserverMessages.Add(1)
			return
		}
		var eventID string
		if json.Unmarshal(values["qa_id"], &eventID) != nil || eventID == "" {
			invalidObserverMessages.Add(1)
			return
		}
		receiptID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("broker-telemetry-v1\x00"+nativeE2ETenant+"\x00"+envelope.DeviceID+"\x00"+nativeE2ECell+"\x00"+envelope.Metadata.IngestionID)).String()
		observationsMu.Lock()
		observations[eventID] = append(observations[eventID], nativeLoadObservation{At: time.Now().UTC(), IngestionID: envelope.Metadata.IngestionID, ReceiptID: receiptID, DeviceID: envelope.DeviceID})
		observationsMu.Unlock()
	}
	if err := mqttadapter.WaitTokenContext(ctx, observer.Subscribe(bc.IngressPrefix+"/#", 1, callback), 5*time.Second); err != nil {
		t.Fatalf("subscribe load ingress observer: %v", err)
	}

	clients := make([]mqtt.Client, nativeE2ELoadConnections)
	var connectWG sync.WaitGroup
	connectErrs := make(chan error, nativeE2ELoadConnections)
	for first := 0; first < nativeE2ELoadConnections; first += 16 {
		end := first + 16
		if end > nativeE2ELoadConnections {
			end = nativeE2ELoadConnections
		}
		for i := first; i < end; i++ {
			connectWG.Add(1)
			go func(index int) {
				defer connectWG.Done()
				id := fmt.Sprintf("device-load-%03d", index)
				username, password := "mqtt-e2e-device", "e2e-device-password"
				if multiDevice {
					username, password = nativeE2ELoadVoucher(index)
				}
				options := mqtt.NewClientOptions().AddBroker(broker).SetClientID(id).SetUsername(username).SetPassword(password).SetCleanSession(true).SetAutoReconnect(false)
				client := mqtt.NewClient(options)
				if err := mqttadapter.WaitTokenContext(ctx, client.Connect(), 8*time.Second); err != nil {
					connectErrs <- fmt.Errorf("connect %s: %w", id, err)
					return
				}
				clients[index] = client
			}(i)
		}
		connectWG.Wait()
		if len(connectErrs) > 0 {
			for _, client := range clients {
				if client != nil && client.IsConnected() {
					client.Disconnect(100)
				}
			}
			t.Fatalf("C=100 load setup connected only %d clients; first error: %v", nativeE2ELoadConnections-len(connectErrs), <-connectErrs)
		}
	}
	defer func() {
		for _, client := range clients {
			if client != nil && client.IsConnected() {
				client.Disconnect(100)
			}
		}
	}()

	total := int(duration.Seconds()) * nativeE2ELoadRate
	events := make([]*nativeLoadEvent, total)
	started := time.Now().UTC().Add(500 * time.Millisecond)
	for seq := 0; seq < total; seq++ {
		id := fmt.Sprintf("%s-%06d", runID, seq)
		events[seq] = &nativeLoadEvent{ID: id, DeviceID: deviceIDs[seq%len(deviceIDs)], PayloadBytes: nativeLoadPayloadSize(seq), PlannedAt: started.Add(time.Duration(seq) * time.Second / nativeE2ELoadRate)}
	}
	reportPath := strings.TrimSpace(os.Getenv("TP_NATIVE_E2E_LOAD_REPORT"))
	if reportPath == "" {
		reportPath = filepath.Join("/tmp", "tp-mqtt-20261019", "build", "native-full-stack-load-"+runID+".ndjson")
	}
	partialSummary := nativeLoadSummary{Kind: "summary", RunID: runID, Duration: duration, ConnectionsTarget: nativeE2ELoadConnections, ConnectionsConnected: len(clients), RateTarget: nativeE2ELoadRate, Planned: total, StartedAt: started, LatencyMeasurement: "not collected; accepted/SQL stages are sampled after send drain", StageTimesUseLatency: false}
	ledgerWritten := false
	defer func() {
		if ledgerWritten {
			return
		}
		partialSummary.Result, partialSummary.FinishedAt = "INCOMPLETE", time.Now().UTC()
		partialSummary.Elapsed = partialSummary.FinishedAt.Sub(started)
		for _, event := range events {
			if !event.SentAt.IsZero() {
				partialSummary.Sent++
			}
			if !event.BrokerAckAt.IsZero() {
				partialSummary.BrokerACK++
			}
			if event.ObservationCount > 0 {
				partialSummary.IngressSeen++
			}
			if event.Accepted {
				partialSummary.Accepted++
			}
			if event.SQLObserved {
				partialSummary.TerminalSQLRows++
			}
		}
		if err := writeNativeLoadLedger(reportPath, events, partialSummary); err != nil {
			t.Errorf("write incomplete load ledger: %v", err)
		}
	}()
	var ackWG sync.WaitGroup
	var eventMu sync.Mutex
sendLoop:
	for seq := 0; seq < total; seq++ {
		event := events[seq]
		if err := ctx.Err(); err != nil {
			event.BrokerAckError = "not sent; context canceled: " + err.Error()
			break sendLoop
		}
		due := event.PlannedAt
		if wait := time.Until(due); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				event.BrokerAckError = "not sent; context canceled: " + ctx.Err().Error()
				break sendLoop
			case <-timer.C:
			}
		}
		payload := nativeLoadPayload(event.ID, event.PayloadBytes, multiDevice)
		if len(payload) != event.PayloadBytes {
			event.BrokerAckError = fmt.Sprintf("not sent; generated payload size=%d want=%d", len(payload), event.PayloadBytes)
			continue
		}
		event.SentAt = time.Now().UTC()
		event.GeneratorLagMS = float64(time.Since(due).Microseconds()) / 1000
		token := clients[seq%len(clients)].Publish("devices/telemetry", 1, false, payload)
		ackWG.Add(1)
		go func(event *nativeLoadEvent, token mqtt.Token) {
			defer ackWG.Done()
			if !token.WaitTimeout(8 * time.Second) {
				eventMu.Lock()
				event.BrokerAckError = "PUBACK timeout after 8s"
				eventMu.Unlock()
				return
			}
			eventMu.Lock()
			defer eventMu.Unlock()
			if err := token.Error(); err != nil {
				event.BrokerAckError = err.Error()
				return
			}
			event.BrokerAckAt = time.Now().UTC()
		}(event, token)
	}
	ackWG.Wait()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		observationsMu.Lock()
		seen := 0
		for _, event := range events {
			if len(observations[event.ID]) > 0 {
				seen++
			}
		}
		observationsMu.Unlock()
		if seen == total {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	observationsMu.Lock()
	for _, event := range events {
		items := observations[event.ID]
		event.ObservationCount = len(items)
		seenIDs, seenDevices, receiptIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, observation := range items {
			if event.IngressSeenAt.IsZero() || observation.At.Before(event.IngressSeenAt) {
				event.IngressSeenAt = observation.At
				event.IngestionID, event.ReceiptID = observation.IngestionID, observation.ReceiptID
			}
			seenIDs[observation.IngestionID] = true
			seenDevices[observation.DeviceID] = true
			receiptIDs[observation.ReceiptID] = true
		}
		for id := range seenIDs {
			event.ObservedIngestionIDs = append(event.ObservedIngestionIDs, id)
		}
		for id := range seenDevices {
			event.ObservedDeviceIDs = append(event.ObservedDeviceIDs, id)
		}
		for id := range receiptIDs {
			event.ObservedReceiptIDs = append(event.ObservedReceiptIDs, id)
		}
		sort.Strings(event.ObservedIngestionIDs)
		sort.Strings(event.ObservedDeviceIDs)
		sort.Strings(event.ObservedReceiptIDs)
	}
	observationsMu.Unlock()

	byEventID := make(map[string]*nativeLoadEvent, total)
	for _, event := range events {
		byEventID[event.ID] = event
	}
	receiptByID := make(map[string]string, total)
	allReceiptIDs := make([]string, 0, total)
	extraReceiptRows, extraSQLRows := 0, 0
	effectCounts := make(map[string]int, total)
	acceptedCounts, sqlCounts := make(map[string]int, total), make(map[string]int, total)
	drainDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(drainDeadline) {
		var receipts []struct {
			ID, DeviceID, State string
			Payload             []byte
		}
		if err := db.Table("broker_ingress_receipts").Select("id,device_id,state,payload").Where("tenant_id=? AND device_id IN ? AND stream='telemetry_sample' AND created_at>=? AND convert_from(payload,'UTF8') LIKE ?", nativeE2ETenant, deviceIDs, started.Add(-time.Second), "%"+runID+"%").Scan(&receipts).Error; err != nil {
			t.Fatalf("query run telemetry receipts: %v", err)
		}
		acceptedCounts = make(map[string]int, total)
		receiptByID = make(map[string]string, total)
		allReceiptIDs = allReceiptIDs[:0]
		extraReceiptRows = 0
		for _, receipt := range receipts {
			allReceiptIDs = append(allReceiptIDs, receipt.ID)
			var values map[string]json.RawMessage
			var eventID string
			if json.Unmarshal(receipt.Payload, &values) == nil && json.Unmarshal(values["qa_id"], &eventID) == nil && strings.HasPrefix(eventID, runID+"-") {
				if event := byEventID[eventID]; event != nil {
					if receipt.DeviceID != event.DeviceID {
						extraReceiptRows++
					} else {
						receiptByID[receipt.ID] = eventID
						acceptedCounts[eventID]++
					}
				} else {
					extraReceiptRows++
				}
			} else if strings.Contains(string(receipt.Payload), runID) {
				extraReceiptRows++
			}
		}
		var sqlRows []struct {
			DeviceID, Value string
			Count           int
		}
		if multiDevice {
			if err := db.Table("telemetry_datas").Select("device_id,string_v AS value,count(*) AS count").Where("tenant_id=? AND device_id IN ? AND key='qa_id' AND string_v LIKE ?", nativeE2ETenant, deviceIDs, runID+"-%").Group("device_id,string_v").Scan(&sqlRows).Error; err != nil {
				t.Fatalf("query telemetry SQL rows: %v", err)
			}
		} else {
			if err := db.Table("telemetry_datas").Select("device_id,key AS value,count(*) AS count").Where("tenant_id=? AND device_id=? AND key LIKE ?", nativeE2ETenant, nativeE2EDevice, "load_"+runID+"-%").Group("device_id,key").Scan(&sqlRows).Error; err != nil {
				t.Fatalf("query telemetry SQL rows: %v", err)
			}
		}
		sqlCounts = make(map[string]int, total)
		extraSQLRows = 0
		for _, row := range sqlRows {
			eventID := row.Value
			if !multiDevice {
				eventID = strings.TrimPrefix(row.Value, "load_")
			}
			if event := byEventID[eventID]; event != nil && (!multiDevice || event.DeviceID == row.DeviceID) {
				sqlCounts[event.ID] += row.Count
			} else if strings.HasPrefix(eventID, runID+"-") {
				extraSQLRows += row.Count
			}
		}
		allAccepted, allTerminal := true, true
		for _, event := range events {
			event.Accepted = acceptedCounts[event.ID] == 1
			event.SQLRowCount = sqlCounts[event.ID]
			event.SQLObserved = event.SQLRowCount == 1
			allAccepted = allAccepted && acceptedCounts[event.ID] > 0
			allTerminal = allTerminal && sqlCounts[event.ID] > 0
		}
		if allAccepted && allTerminal {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, event := range events {
		if acceptedCounts[event.ID] > 1 {
			extraReceiptRows += acceptedCounts[event.ID] - 1
		}
		if sqlCounts[event.ID] > 1 {
			extraSQLRows += sqlCounts[event.ID] - 1
		}
	}
	extraEffectRows := 0
	if len(allReceiptIDs) > 0 {
		var effects []struct {
			ReceiptID string
			Count     int
		}
		if err := db.Table("broker_ingress_effects").Select("receipt_id,count(*) AS count").Where("receipt_id IN ? AND effect_kind='automation'", allReceiptIDs).Group("receipt_id").Scan(&effects).Error; err != nil {
			t.Fatalf("query telemetry effect-intent IDs: %v", err)
		}
		for _, effect := range effects {
			if eventID := receiptByID[effect.ReceiptID]; eventID != "" {
				effectCounts[eventID] += effect.Count
			} else {
				extraEffectRows += effect.Count
			}
		}
	}
	for _, count := range effectCounts {
		if count > 1 {
			extraEffectRows += count - 1
		}
	}

	finished := time.Now().UTC()
	maxLag := 0.0
	summary := nativeLoadSummary{Kind: "summary", RunID: runID, Duration: duration, ConnectionsTarget: nativeE2ELoadConnections, ConnectionsConnected: len(clients), RateTarget: nativeE2ELoadRate, Planned: total, SideEffectsExecuted: "unobserved; this health harness observes persisted automation intents only", StartedAt: started, FinishedAt: finished, LatencyMeasurement: "not collected; accepted/SQL stages are sampled after send drain", StageTimesUseLatency: false}
	for _, event := range events {
		if !event.SentAt.IsZero() {
			summary.Sent++
		}
		if event.BrokerAckError == "" && !event.BrokerAckAt.IsZero() {
			summary.BrokerACK++
		}
		if event.ObservationCount > 0 {
			summary.IngressSeen++
		}
		if acceptedCounts[event.ID] == 1 {
			event.Accepted = true
			summary.Accepted++
		}
		event.SQLRowCount = sqlCounts[event.ID]
		if event.SQLRowCount == 1 {
			event.SQLObserved = true
			summary.TerminalSQLRows++
		}
		event.EffectIntentSQL = effectCounts[event.ID] == 1
		if len(event.ObservedDeviceIDs) > 0 && (len(event.ObservedDeviceIDs) != 1 || event.ObservedDeviceIDs[0] != event.DeviceID) {
			summary.WrongDeviceIdentity++
		}
		if event.ObservationCount > 1 {
			summary.DuplicateObserver += event.ObservationCount - 1
		}
		summary.EffectIntentSQL += effectCounts[event.ID]
		if event.GeneratorLagMS > maxLag {
			maxLag = event.GeneratorLagMS
		}
	}
	observationsMu.Lock()
	for eventID, items := range observations {
		if byEventID[eventID] == nil {
			summary.ExtraObserverRows += len(items)
		}
	}
	observationsMu.Unlock()
	summary.ExtraReceiptRows, summary.ExtraSQLRows, summary.ExtraEffectRows = extraReceiptRows, extraSQLRows, extraEffectRows
	summary.MaxGeneratorLagMS = maxLag
	summary.Elapsed = finished.Sub(started)
	if summary.Planned == summary.Sent && summary.BrokerACK == summary.Planned && summary.IngressSeen == summary.Planned && summary.Accepted == summary.Planned && summary.TerminalSQLRows == summary.Planned && summary.EffectIntentSQL == summary.Planned && summary.WrongDeviceIdentity == 0 && summary.DuplicateObserver == 0 && summary.ExtraObserverRows == 0 && summary.ExtraReceiptRows == 0 && summary.ExtraSQLRows == 0 && summary.ExtraEffectRows == 0 && invalidObserverMessages.Load() == 0 {
		summary.Result = "PASS_HEALTH_ONLY"
	} else {
		summary.Result = "INCOMPLETE"
	}
	if err := writeNativeLoadLedger(reportPath, events, summary); err != nil {
		t.Fatalf("write load report: %v", err)
	}
	ledgerWritten = true
	t.Logf("native full-stack load profile=%s result=%s planned=%d sent=%d broker_ack=%d ingress_seen=%d accepted=%d terminal_sql=%d effect_intents=%d wrong_device=%d duplicate_observer=%d extra_receipts=%d extra_sql=%d extra_effects=%d invalid_observer=%d max_generator_lag_ms=%.3f report=%s", map[bool]string{true: "100-devices", false: "single-device"}[multiDevice], summary.Result, summary.Planned, summary.Sent, summary.BrokerACK, summary.IngressSeen, summary.Accepted, summary.TerminalSQLRows, summary.EffectIntentSQL, summary.WrongDeviceIdentity, summary.DuplicateObserver, summary.ExtraReceiptRows, summary.ExtraSQLRows, summary.ExtraEffectRows, invalidObserverMessages.Load(), summary.MaxGeneratorLagMS, reportPath)
	if summary.Result != "PASS_HEALTH_ONLY" {
		t.Fatalf("native full-stack load incomplete; inspect ledger %s", reportPath)
	}
}

func writeNativeLoadLedger(path string, events []*nativeLoadEvent, summary nativeLoadSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	for _, event := range events {
		if err = encoder.Encode(event); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err = encoder.Encode(summary); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func nativeE2EConnectLoadDevice(t *testing.T, ctx context.Context, broker, clientID string) (mqtt.Client, error) {
	t.Helper()
	options := mqtt.NewClientOptions().AddBroker(broker).SetClientID(clientID).SetUsername("mqtt-e2e-device").SetPassword("e2e-device-password").SetCleanSession(true).SetAutoReconnect(false)
	client := mqtt.NewClient(options)
	if err := mqttadapter.WaitTokenContext(ctx, client.Connect(), 8*time.Second); err != nil {
		return nil, err
	}
	return client, nil
}

func nativeLoadPayloadSize(seq int) int {
	position := seq % 100
	switch {
	case position < 90:
		return 512
	case position < 99:
		return 4 * 1024
	default:
		return 64 * 1024
	}
}

func nativeLoadPayload(id string, size int, fixedKey bool) []byte {
	prefix := fmt.Sprintf("{\"qa_id\":%q,\"pad\":\"", id)
	if !fixedKey {
		prefix = fmt.Sprintf("{\"qa_id\":%q,\"load_%s\":1,\"pad\":\"", id, id)
	}
	suffix := "\"}"
	pad := size - len(prefix) - len(suffix)
	if pad < 0 {
		return nil
	}
	return []byte(prefix + strings.Repeat("x", pad) + suffix)
}

func nativeE2ELoadDeviceID(index int) string { return fmt.Sprintf("device-mqtt-load-%03d", index) }
func nativeE2ELoadVoucher(index int) (string, string) {
	return fmt.Sprintf("mqtt-e2e-load-%03d", index), fmt.Sprintf("e2e-load-password-%03d", index)
}
func containsNativeLoadDevice(devices []string, deviceID string) bool {
	for _, id := range devices {
		if id == deviceID {
			return true
		}
	}
	return false
}
func seedNativeE2ELoadDevices(t *testing.T, db *gorm.DB, count int) {
	t.Helper()
	rows := make([]model.Device, 0, count)
	for i := 0; i < count; i++ {
		username, password := nativeE2ELoadVoucher(i)
		voucher, _ := json.Marshal(map[string]string{"username": username, "password": password})
		rows = append(rows, model.Device{ID: nativeE2ELoadDeviceID(i), TenantID: nativeE2ETenant, DeviceNumber: fmt.Sprintf("native-e2e-load-number-%03d", i), Voucher: string(voucher), IsEnabled: "enabled", ActivateFlag: "active", IsOnline: 0})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed 100 native load devices: %v", err)
	}
}

func nativeE2ERandomID(t *testing.T) string {
	t.Helper()
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw[:])
}
