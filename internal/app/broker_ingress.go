package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"project/internal/adapter/mqttadapter"
	"project/internal/brokerledger"
	"project/internal/service"
	"project/internal/uplink"
	"project/pkg/global"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

var errBrokerIngressTrust = errors.New("invalid trusted broker ingress metadata")

type ingressReceiptStore interface {
	Accept(context.Context, brokerledger.Message) (string, bool, error)
	ProcessOne(context.Context, func(context.Context, *gorm.DB, brokerledger.Message) error) (bool, error)
}

type ingressProcessor interface {
	Process(context.Context, *gorm.DB, brokerledger.Message) error
}

type brokerIngressAcceptor struct {
	store interface {
		Accept(context.Context, brokerledger.Message) (string, bool, error)
	}
	logger *logrus.Logger
}

func (a *brokerIngressAcceptor) Reject(ctx context.Context, input brokerledger.Rejection) error {
	store, ok := a.store.(interface {
		RecordRejection(context.Context, brokerledger.Rejection) (string, bool, error)
	})
	if !ok {
		return errors.New("durable ingress quarantine store unavailable")
	}
	id, duplicate, err := store.RecordRejection(ctx, input)
	if err == nil && a.logger != nil {
		a.logger.WithFields(logrus.Fields{"rejection_id": id, "duplicate": duplicate, "reason": input.Reason, "tenant_id": input.TenantID, "device_id": input.DeviceID}).Warn("MQTT ingress quarantined; business acceptance was not granted")
	}
	return err
}

func (a *brokerIngressAcceptor) Accept(ctx context.Context, input *uplink.DeviceMessage) error {
	if input == nil {
		return errors.New("nil ingress message")
	}
	cell, ok := input.Metadata["broker_cell"].(string)
	if !ok || cell == "" {
		return errBrokerIngressTrust
	}
	epoch, ok := strictUint64(input.Metadata["route_epoch"])
	if !ok || epoch == 0 {
		return errBrokerIngressTrust
	}
	metadata := make(map[string]any, len(input.Metadata)+1)
	for key, value := range input.Metadata {
		metadata[key] = value
	}
	metadata["timestamp"] = input.Timestamp
	var sourceID string
	if input.Type == "event" || input.Type == "gateway_event" || input.Type == "gateway_attribute" {
		var raw struct {
			SourceEventID string `json:"source_event_id"`
		}
		if json.Unmarshal(input.Payload, &raw) == nil {
			sourceID = raw.SourceEventID
		}
	}
	message := brokerledger.Message{
		TenantID: input.TenantID, DeviceID: input.DeviceID, Stream: string(input.Type),
		SourceEventID: sourceID, BrokerCell: cell, RouteEpoch: epoch,
		Payload: append([]byte(nil), input.Payload...), Metadata: metadata,
	}
	if value, exists := metadata["ingestion_id"]; exists {
		ingestionID, ok := value.(string)
		if !ok || ingestionID == "" || len(ingestionID) > 128 || strings.TrimSpace(ingestionID) != ingestionID {
			return errBrokerIngressTrust
		}
		message.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte("broker-ingress-v1\x00"+input.TenantID+"\x00"+input.DeviceID+"\x00"+string(input.Type)+"\x00"+cell+"\x00"+ingestionID)).String()
	}
	id, duplicate, err := a.store.Accept(ctx, message)
	if err != nil {
		return err
	}
	if a.logger != nil {
		a.logger.WithFields(logrus.Fields{"receipt_id": id, "tenant_id": input.TenantID, "device_id": input.DeviceID, "duplicate": duplicate}).Info("MQTT ingress durably accepted; business processing remains pending")
	}
	return nil
}

func strictUint64(value any) (uint64, bool) {
	switch n := value.(type) {
	case uint64:
		return n, true
	case uint:
		return uint64(n), true
	case uint32:
		return uint64(n), true
	case int:
		if n >= 0 {
			return uint64(n), true
		}
	case int64:
		if n >= 0 {
			return uint64(n), true
		}
	case json.Number:
		u, err := strconv.ParseUint(string(n), 10, 64)
		return u, err == nil
	case float64:
		if n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0) && n == math.Trunc(n) && n < math.Exp2(64) {
			return uint64(n), true
		}
	}
	return 0, false
}

type brokerIngressWorker struct {
	cell           string
	store          ingressReceiptStore
	processor      ingressProcessor
	processEffects func(context.Context) (bool, error)
	runtimeSync    func(context.Context) error
	logger         *logrus.Logger
	mu             sync.Mutex
	cancel         context.CancelFunc
	done           chan struct{}
}

func (w *brokerIngressWorker) Start(ctx context.Context) error {
	if w == nil || w.store == nil || w.processor == nil || ctx == nil {
		return errors.New("broker ingress worker is not configured")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done != nil {
		return nil
	}
	if w.runtimeSync != nil {
		if e := w.runtimeSync(ctx); e != nil {
			return e
		}
	}
	workerCtx, cancel := context.WithCancel(ctx)
	w.cancel, w.done = cancel, make(chan struct{})
	go w.run(workerCtx, w.done)
	return nil
}

func (w *brokerIngressWorker) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	cancel, done := w.cancel, w.done
	w.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	w.mu.Lock()
	if w.done == done {
		w.cancel, w.done = nil, nil
	}
	w.mu.Unlock()
}

func (w *brokerIngressWorker) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	delay := 100 * time.Millisecond
	const maxDelay = 2 * time.Second
	var lastSync time.Time
	for ctx.Err() == nil {
		if w.runtimeSync != nil && time.Since(lastSync) >= 5*time.Second {
			if e := w.runtimeSync(ctx); e != nil {
				if ctx.Err() != nil {
					return
				}
				if w.logger != nil {
					w.logger.Warn("reliable ingress paused: route runtime synchronization unavailable")
				}
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				continue
			}
			lastSync = time.Now()
		}
		processed, err := w.processOne(ctx)
		if ctx.Err() == nil && w.processEffects != nil {
			effectProcessed, effectErr := w.processEffects(ctx)
			processed = processed || effectProcessed
			if err == nil {
				err = effectErr
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if w.logger != nil {
				w.logger.WithError(err).Error("reliable MQTT ingress processing failed")
			}
		} else if processed {
			delay = 100 * time.Millisecond
			continue
		} else {
			delay = 100 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		if err != nil && delay < maxDelay {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
}

func (w *brokerIngressWorker) processOne(ctx context.Context) (bool, error) {
	if w.cell == "" {
		return w.store.ProcessOne(ctx, w.processor.Process)
	}
	store, ok := w.store.(interface {
		ProcessOneInCell(context.Context, string, func(context.Context, *gorm.DB, brokerledger.Message) error) (bool, error)
	})
	if !ok {
		return false, errors.New("reliable ingress store does not support cell scoping")
	}
	return store.ProcessOneInCell(ctx, w.cell, w.processor.Process)
}

var ingressRequiredColumns = map[string][]string{
	"broker_automation_execution_windows": {"tenant_id", "scene_id", "device_id", "effect_id", "last_evaluated_at", "tokens"},
	"broker_ingress_quarantine":           {"id", "tenant_id", "device_id", "broker_cell", "route_epoch", "topic", "body", "reason", "occurrences", "last_seen", "expires_at"},
	"broker_command_requests":             {"id", "tenant_id", "caller_id", "intent", "idempotency_key", "device_id", "target_device_id", "route_cell", "route_epoch", "canonical_request", "canonical_sha256", "payload", "expires_at", "state", "last_error", "created_at", "updated_at", "dispatched_at", "completed_at"},
	"broker_state_projection_queue":       {"tenant_id", "device_id", "updated_at"},
	"broker_transport_bindings":           {"tenant_id", "device_id", "broker_cell", "route_epoch", "phase"},
	"broker_ingress_receipts":             {"id", "tenant_id", "device_id", "stream", "source_event_id", "broker_cell", "route_epoch", "payload", "metadata", "canonical_sha256", "state", "attempts", "retry_after", "last_error", "created_at", "processed_at"},
	"broker_connection_observations":      {"tenant_id", "device_id", "broker_cell", "broker_instance", "node_boot_id", "connection_id", "route_epoch", "event_seq", "online", "observed_at", "received_at"},
	"broker_ingress_effects":              {"id", "receipt_id", "tenant_id", "device_id", "effect_kind", "payload", "state", "last_error", "created_at", "updated_at", "attempts", "retry_after"},
	"broker_telemetry_activity":           {"tenant_id", "device_id", "transport_device_id", "broker_cell", "route_epoch", "receipt_id", "observed_at", "next_check_at"},
	"broker_automation_policy_snapshots":  {"tenant_id", "scene_id", "policy_sha256", "snapshot"},
	"broker_automation_actions":           {"id", "effect_id", "tenant_id", "scene_id", "action_id", "target_device_id", "policy_sha256", "state", "command_id", "detail"},
}

func validateBrokerIngressSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("ingress_v1 requires an application database")
	}
	for table, required := range ingressRequiredColumns {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("ingress_v1 schema missing table %s", table)
		}
		columns, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			return fmt.Errorf("inspect ingress_v1 table %s: %w", table, err)
		}
		found := make(map[string]bool, len(columns))
		for _, column := range columns {
			found[column.Name()] = true
		}
		for _, name := range required {
			if !found[name] {
				return fmt.Errorf("ingress_v1 schema table %s missing column %s", table, name)
			}
		}
	}
	var safeVoucher bool
	if err := db.Raw("SELECT to_regprocedure('broker_safe_voucher_v1(text)') IS NOT NULL").Scan(&safeVoucher).Error; err != nil {
		return errors.New("ingress_v1 could not validate credential schema")
	}
	if !safeVoucher {
		return errors.New("ingress_v1 requires safe voucher migration 011")
	}

	return nil
}

func (s *MQTTService) configureBrokerIngress(ctx context.Context, adapter *mqttadapter.Adapter) error {
	profile := "legacy"
	if configured := viper.GetString("mqtt.input_profile"); configured != "" {
		profile = configured
	}
	if profile == "legacy" {
		return nil
	}
	if profile != "ingress_v1" {
		return fmt.Errorf("unsupported MQTT input profile %q", profile)
	}
	if s.app == nil || s.app.DB == nil || global.DB == nil {
		return errors.New("ingress_v1 requires both application and global database connections")
	}
	if s.app.DB != global.DB {
		return errors.New("ingress_v1 requires application and global database to share the same connection")
	}
	if err := validateBrokerIngressSchema(s.app.DB.WithContext(ctx)); err != nil {
		return err
	}
	store := brokerledger.New(s.app.DB)
	processor := uplink.NewReliableProcessor(s.logger())
	worker := &brokerIngressWorker{cell: viper.GetString("mqtt.broker_cell"), store: store, processor: processor, logger: s.logger()}
	runtimeSync, err := brokerRuntimeSync(s.app.DB, "adapter", "effects")
	if err != nil {
		return err
	}
	worker.runtimeSync = runtimeSync
	worker.processEffects = func(ctx context.Context) (bool, error) {
		return service.ProcessBrokerAutomationEffect(ctx, s.app.DB, worker.cell)
	}
	if err := worker.Start(ctx); err != nil {
		return err
	}
	adapter.SetIngressAcceptor(&brokerIngressAcceptor{store: store, logger: s.logger()})
	s.mu.Lock()
	if s.brokerWorker != nil {
		s.mu.Unlock()
		worker.Stop()
		return errors.New("broker ingress worker already configured")
	}
	s.brokerWorker = worker
	s.mu.Unlock()
	return nil
}
