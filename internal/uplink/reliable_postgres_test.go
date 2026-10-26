package uplink

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"project/internal/brokercommands"
	"project/internal/brokerledger"
	"project/internal/model"
	"project/internal/storage"
	"strconv"
	"strings"
	"testing"
	"time"
)

func reliableFixture(t *testing.T) (*gorm.DB, *brokerledger.Store) {
	t.Helper()
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not set")
	}
	root, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	schema := "broker_business_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if e = root.Exec("CREATE SCHEMA " + schema).Error; e != nil {
		t.Fatal(e)
	}
	db, e := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		s, _ := db.DB()
		s.Close()
		root.Exec("DROP SCHEMA " + schema + " CASCADE")
		r, _ := root.DB()
		r.Close()
	})
	if e = db.AutoMigrate(&model.Device{}, &storage.EventDataModel{}, &storage.AttributeData{}, &model.CommandSetLog{}, &model.AttributeSetLog{}); e != nil {
		t.Fatal(e)
	}
	if e = db.Exec("CREATE UNIQUE INDEX ON attribute_datas(device_id,key)").Error; e != nil {
		t.Fatal(e)
	}
	if e = db.Exec(`CREATE TABLE broker_transport_bindings(tenant_id text,device_id text,broker_cell text,route_epoch bigint,phase text,PRIMARY KEY(tenant_id,device_id))`).Error; e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"002_ledger.sql", "005_ingress_effects.sql", "006_commands.sql"} {
		b, e := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Exec(strings.ReplaceAll(string(b), "public.", "")).Error; e != nil {
			t.Fatal(e)
		}
	}
	return db, brokerledger.New(db)
}

func reliableAttributeMessage(device, tenant, sourceID string, ts int64, value int) brokerledger.Message {
	msg := reliableMessage(device, tenant, "attribute", []byte(`{"temperature":`+strconv.Itoa(value)+`}`))
	msg.SourceEventID = sourceID
	msg.Metadata["timestamp"] = ts
	return msg
}

func processReliable(t *testing.T, s *brokerledger.Store) error {
	t.Helper()
	_, err := s.ProcessOne(context.Background(), NewReliableProcessor(logrus.New()).Process)
	return err
}

func TestReliableDelayedOldAttributeReceiptCannotReplaceNewerTimestamp(t *testing.T) {
	db, s := reliableFixture(t)
	seedReliableDevice(t, db, "device-a", "tenant-a")
	oldTS := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC).UnixMilli()
	newTS := oldTS + 60_000
	oldID, _, err := s.Accept(context.Background(), reliableAttributeMessage("device-a", "tenant-a", "attribute-old", oldTS, 10))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Accept(context.Background(), reliableAttributeMessage("device-a", "tenant-a", "attribute-new", newTS, 20)); err != nil {
		t.Fatal(err)
	}
	// Model a delayed older receipt becoming available after the newer receipt.
	if err = db.Table("broker_ingress_receipts").Where("id=?", oldID).Update("created_at", time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err = processReliable(t, s); err != nil {
		t.Fatalf("newer receipt: %v", err)
	}
	if err = processReliable(t, s); err != nil {
		t.Fatalf("delayed old receipt: %v", err)
	}
	var got storage.AttributeData
	if err = db.Where("device_id=? AND key=?", "device-a", "temperature").Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.NumberV == nil || *got.NumberV != 20 || got.TS.UnixMilli() != newTS {
		t.Fatalf("delayed old receipt replaced current attribute: value=%v ts=%d want value=20 ts=%d", got.NumberV, got.TS.UnixMilli(), newTS)
	}
}

func TestReliableSameTimestampAttributeUpdateRemainsPermitted(t *testing.T) {
	db, s := reliableFixture(t)
	seedReliableDevice(t, db, "device-a", "tenant-a")
	ts := time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC).UnixMilli()
	for i, value := range []int{10, 11} {
		if _, _, err := s.Accept(context.Background(), reliableAttributeMessage("device-a", "tenant-a", fmt.Sprintf("same-ts-%d", i), ts, value)); err != nil {
			t.Fatal(err)
		}
		if err := processReliable(t, s); err != nil {
			t.Fatal(err)
		}
	}
	var got storage.AttributeData
	if err := db.Where("device_id=? AND key=?", "device-a", "temperature").Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.NumberV == nil || *got.NumberV != 11 || got.TS.UnixMilli() != ts {
		t.Fatalf("same-timestamp update semantics changed: value=%v ts=%d", got.NumberV, got.TS.UnixMilli())
	}
}
func seedReliableDevice(t *testing.T, db *gorm.DB, id, tenant string) {
	t.Helper()
	if e := db.Create(&model.Device{ID: id, TenantID: tenant, DeviceNumber: id, IsEnabled: "enabled", ActivateFlag: "active", Voucher: `{"username":"test"}`}).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("INSERT INTO broker_transport_bindings VALUES(?,?,?,1,'active')", tenant, id, "community").Error; e != nil {
		t.Fatal(e)
	}
}
func reliableMessage(device, tenant, stream string, payload []byte) brokerledger.Message {
	return brokerledger.Message{TenantID: tenant, DeviceID: device, Stream: stream, BrokerCell: "community", RouteEpoch: 1, Payload: payload, Metadata: map[string]any{"timestamp": time.Now().UnixMilli()}}
}
func TestReliableEventBusinessAndReceiptCommitTogether(t *testing.T) {
	db, s := reliableFixture(t)
	seedReliableDevice(t, db, "device-a", "tenant-a")
	msg := reliableMessage("device-a", "tenant-a", "event", []byte(`{"method":"alarm","params":{"value":5}}`))
	msg.SourceEventID = "source-1"
	id, dup, e := s.Accept(context.Background(), msg)
	if e != nil || dup {
		t.Fatalf("accept %v", e)
	}
	p := NewReliableProcessor(logrus.New())
	worked, e := s.ProcessOne(context.Background(), p.Process)
	if e != nil || !worked {
		t.Fatalf("process %v", e)
	}
	var rows, effects int64
	db.Model(&storage.EventDataModel{}).Count(&rows)
	db.Table("broker_ingress_effects").Count(&effects)
	if rows != 1 || effects != 1 {
		t.Fatalf("domain=%d effects=%d", rows, effects)
	}
	receipt, e := s.GetReceipt(context.Background(), "tenant-a", "device-a", id)
	if e != nil || receipt.State != "processed" {
		t.Fatalf("receipt %+v %v", receipt, e)
	}
	if worked, e = s.ProcessOne(context.Background(), p.Process); e != nil || worked {
		t.Fatal("processed twice")
	}
	if _, dup, e = s.Accept(context.Background(), msg); e != nil || !dup {
		t.Fatal("source deduplication")
	}
}
func TestReliableEffectFailureRollsBackDomain(t *testing.T) {
	db, s := reliableFixture(t)
	seedReliableDevice(t, db, "device-a", "tenant-a")
	if e := db.Exec("ALTER TABLE broker_ingress_effects ADD CONSTRAINT inject_failure CHECK(effect_kind<>'automation')").Error; e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Accept(context.Background(), reliableMessage("device-a", "tenant-a", "event", []byte(`{"method":"a","params":{}}`))); e != nil {
		t.Fatal(e)
	}
	p := NewReliableProcessor(logrus.New())
	_, e := s.ProcessOne(context.Background(), p.Process)
	if e == nil {
		t.Fatal("injected fault ignored")
	}
	var rows int64
	db.Model(&storage.EventDataModel{}).Count(&rows)
	if rows != 0 {
		t.Fatal("domain commit escaped failed receipt transaction")
	}
}
func TestReliableCommandAckRemainsScoped(t *testing.T) {
	db, s := reliableFixture(t)
	seedReliableDevice(t, db, "device-a", "tenant-a")
	seedReliableDevice(t, db, "device-b", "tenant-b")
	messageID := "command-1"
	status := "1"
	if e := db.Create(&model.CommandSetLog{ID: uuid.NewString(), DeviceID: "device-b", MessageID: &messageID, Status: &status, CreatedAt: time.Now()}).Error; e != nil {
		t.Fatal(e)
	}
	msg := reliableMessage("device-a", "tenant-a", "command_response", []byte(`{"result":0}`))
	msg.Metadata["message_id"] = messageID
	if _, _, e := s.Accept(context.Background(), msg); e != nil {
		t.Fatal(e)
	}
	p := NewReliableProcessor(logrus.New())
	if _, e := s.ProcessOne(context.Background(), p.Process); e == nil {
		t.Fatal("cross device ACK processed")
	}
	var got model.CommandSetLog
	db.First(&got, "device_id=?", "device-b")
	if got.Status == nil || *got.Status != "1" {
		t.Fatal("foreign command changed")
	}
}

func seedReliableDurableCommand(t *testing.T, db *gorm.DB, tenant, logicalDevice, targetDevice, commandID string, epoch uint64, state string) {
	t.Helper()
	hash := sha256.Sum256([]byte(commandID))
	row := brokercommands.Row{
		ID: commandID, TenantID: tenant, CallerID: "test-caller", Intent: "control", IdempotencyKey: "key-" + commandID,
		DeviceID: logicalDevice, TargetDeviceID: targetDevice, RouteCell: "community", RouteEpoch: int64(epoch),
		CanonicalRequest: []byte(`{"device_id":"` + logicalDevice + `"}`), CanonicalSHA256: hash[:], Payload: []byte(`{"method":"switch"}`),
		ExpiresAt: time.Now().Add(time.Hour), State: state, LastError: "", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed durable command: %v", err)
	}
	status := "1"
	if err := db.Exec("INSERT INTO command_set_logs(id,device_id,message_id,status,created_at) VALUES(?,?,?,?,?)", uuid.NewString(), logicalDevice, commandID, status, time.Now()).Error; err != nil {
		t.Fatalf("seed logical command log: %v", err)
	}
}

func acceptReliableAck(t *testing.T, s *brokerledger.Store, tenant, transportDevice, commandID string, epoch uint64, result int, source string) error {
	t.Helper()
	msg := reliableMessage(transportDevice, tenant, "command_response", []byte(fmt.Sprintf(`{"result":%d}`, result)))
	msg.RouteEpoch = epoch
	msg.SourceEventID = source
	msg.Metadata["message_id"] = commandID
	if _, _, err := s.Accept(context.Background(), msg); err != nil {
		return err
	}
	return processReliable(t, s)
}

func TestReliableDurableCommandAckAssociatesLogicalChildAndRootTransport(t *testing.T) {
	db, s := reliableFixture(t)
	seedReliableDevice(t, db, "gateway-a", "tenant-a")
	parent := "gateway-a"
	child := model.Device{ID: "child-a", TenantID: "tenant-a", DeviceNumber: "child-a", ParentID: &parent, IsEnabled: "enabled", ActivateFlag: "active", Voucher: `{}`}
	if err := db.Create(&child).Error; err != nil {
		t.Fatal(err)
	}
	commandID := uuid.NewString()
	seedReliableDurableCommand(t, db, "tenant-a", child.ID, "gateway-a", commandID, 1, brokercommands.StateDispatching)
	if err := acceptReliableAck(t, s, "tenant-a", "gateway-a", commandID, 1, 0, "child-root-ack"); err != nil {
		t.Fatalf("child command ACK via root transport failed: %v", err)
	}
	var log model.CommandSetLog
	if err := db.Where("device_id=? AND message_id=?", child.ID, commandID).Take(&log).Error; err != nil || log.Status == nil || *log.Status != "3" {
		t.Fatalf("ACK was not applied to logical child command log: status=%v err=%v", log.Status, err)
	}
	row, err := brokercommands.New(db).GetScoped(context.Background(), "tenant-a", commandID)
	if err != nil || row.State != brokercommands.StateDeviceReportedSuccess {
		t.Fatalf("command receipt not terminal after ACK: state=%q err=%v", row.State, err)
	}
	if err = brokercommands.New(db).FinishPublish(context.Background(), commandID, true, "late publish completion"); !errors.Is(err, brokercommands.ErrState) {
		t.Fatalf("late publish completion after early device ACK=%v, want ErrState", err)
	}
	row, err = brokercommands.New(db).GetScoped(context.Background(), "tenant-a", commandID)
	if err != nil || row.State != brokercommands.StateDeviceReportedSuccess {
		t.Fatalf("early terminal ACK was demoted by FinishPublish: state=%q err=%v", row.State, err)
	}
}

func TestReliableCommandAckIsIdempotentForSameOutcomeAndRejectsConflict(t *testing.T) {
	db, s := reliableFixture(t)
	seedReliableDevice(t, db, "device-a", "tenant-a")
	commandID := uuid.NewString()
	seedReliableDurableCommand(t, db, "tenant-a", "device-a", "device-a", commandID, 1, brokercommands.StateBrokerAccepted)
	for i := 0; i < 2; i++ {
		if err := acceptReliableAck(t, s, "tenant-a", "device-a", commandID, 1, 0, fmt.Sprintf("ack-success-%d", i)); err != nil {
			t.Fatalf("same terminal ACK %d was not idempotent: %v", i, err)
		}
	}
	if err := acceptReliableAck(t, s, "tenant-a", "device-a", commandID, 1, 1, "ack-conflict"); err == nil {
		t.Fatal("conflicting terminal ACK was accepted")
	}
	var log model.CommandSetLog
	if err := db.Where("device_id=? AND message_id=?", "device-a", commandID).Take(&log).Error; err != nil || log.Status == nil || *log.Status != "3" {
		t.Fatalf("conflicting ACK overwrote terminal log: status=%v err=%v", log.Status, err)
	}
	row, err := brokercommands.New(db).GetScoped(context.Background(), "tenant-a", commandID)
	if err != nil || row.State != brokercommands.StateDeviceReportedSuccess {
		t.Fatalf("conflicting ACK overwrote terminal receipt: state=%q err=%v", row.State, err)
	}
}

func TestReliableChildCommandAckRejectsWrongDeviceTenantAndEpoch(t *testing.T) {
	tests := []struct {
		name        string
		ackTenant   string
		transport   string
		ackEpoch    uint64
		bumpBinding bool
	}{
		{name: "wrong transport device", ackTenant: "tenant-a", transport: "gateway-b", ackEpoch: 1},
		{name: "wrong tenant", ackTenant: "tenant-b", transport: "gateway-a", ackEpoch: 1},
		{name: "wrong route epoch", ackTenant: "tenant-a", transport: "gateway-a", ackEpoch: 2, bumpBinding: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, s := reliableFixture(t)
			seedReliableDevice(t, db, "gateway-a", "tenant-a")
			seedReliableDevice(t, db, "gateway-b", "tenant-a")
			seedReliableDevice(t, db, "gateway-other", "tenant-b")
			parent := "gateway-a"
			child := model.Device{ID: "child-a", TenantID: "tenant-a", DeviceNumber: "child-a", ParentID: &parent, IsEnabled: "enabled", ActivateFlag: "active", Voucher: `{}`}
			if err := db.Create(&child).Error; err != nil {
				t.Fatal(err)
			}
			commandID := uuid.NewString()
			seedReliableDurableCommand(t, db, "tenant-a", child.ID, "gateway-a", commandID, 1, brokercommands.StateBrokerAccepted)
			transport := tt.transport
			if tt.name == "wrong tenant" {
				transport = "gateway-other"
			}
			if tt.bumpBinding {
				if err := db.Exec("UPDATE broker_transport_bindings SET route_epoch=2 WHERE tenant_id=? AND device_id=?", "tenant-a", "gateway-a").Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := acceptReliableAck(t, s, tt.ackTenant, transport, commandID, tt.ackEpoch, 0, "bad-"+strings.ReplaceAll(tt.name, " ", "-")); err == nil {
				t.Fatal("out-of-scope ACK accepted")
			}
			row, err := brokercommands.New(db).GetScoped(context.Background(), "tenant-a", commandID)
			if err != nil || row.State != brokercommands.StateBrokerAccepted {
				t.Fatalf("rejected ACK changed command receipt: state=%q err=%v", row.State, err)
			}
			var log model.CommandSetLog
			if err := db.Where("device_id=? AND message_id=?", child.ID, commandID).Take(&log).Error; err != nil || log.Status == nil || *log.Status != "1" {
				t.Fatalf("rejected ACK changed logical command log: status=%v err=%v", log.Status, err)
			}
		})
	}
}
