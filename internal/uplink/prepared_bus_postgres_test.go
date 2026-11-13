package uplink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"project/internal/brokersemantic"
	"project/internal/diagnostics"
	"project/internal/processor"
	"project/internal/storage"
)

func TestPostgresPreparedNumericCapacityIsNotSchemaFailure(t *testing.T) {
	db, store, p := preparedFixture(t)
	seedReliableDevice(t, db, "d", "tenant-a")
	attachPrepared(t, db, "d")
	contract, err := brokersemantic.CompileUplinkContract(json.RawMessage(strings.Replace(preparedContract, `"kind":"NUMBER"`, `"kind":"INT"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	p.contracts = preparedContractReader{contract}
	body := []byte(`{"temperature":1` + strings.Repeat("0", 400) + `}`)
	if err = contract.Validate("attribute", body); err != nil {
		t.Fatalf("published INT contract must allow: %v", err)
	}
	p.decoder = &decodeProcessorStub{out: &processor.DecodeOutput{Success: true, Data: body}}
	id, _, err := store.Accept(context.Background(), reliableMessage("d", "tenant-a", "attribute", body))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ProcessOnePrepared(context.Background(), "community", p.Prepare)
	var capacity storageRepresentationRejection
	if !errors.As(err, &capacity) || errors.Is(err, brokersemantic.ErrUplinkInvalid) {
		t.Fatalf("capacity misclassified: %v", err)
	}
	r, e := store.GetReceipt(context.Background(), "tenant-a", "d", id)
	if e != nil || r.State != "dead" || r.Attempts != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	ev, attrs, effects := preparedCounts(t, db)
	if ev+attrs+effects != 0 {
		t.Fatal("unrepresentable native INT became _raw/business success")
	}
	// Document the existing float8 boundary, without changing the storage schema.
	body = []byte(`{"temperature":9007199254740993}`)
	p.decoder = &decodeProcessorStub{out: &processor.DecodeOutput{Success: true, Data: body}}
	if _, _, e = store.Accept(context.Background(), reliableMessage("d", "tenant-a", "attribute", body)); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ProcessOnePrepared(context.Background(), "community", p.Prepare); e != nil {
		t.Fatal(e)
	}
	var value float64
	if e = db.Table("attribute_datas").Select("number_v").Scan(&value).Error; e != nil || value != 9007199254740992 {
		t.Fatalf("existing float8 representation %v %v", value, e)
	}
	t.Log("existing float8 limitation: source INT 9007199254740993 stored as 9007199254740992; no precision-storage expansion in this patch")
}

func TestPostgresRedisPreparedBusCommitRejectSiblingAndCapacity(t *testing.T) {
	addr := os.Getenv("TEST_DIAGNOSTICS_REDIS_ADDR")
	if addr == "" {
		t.Skip("isolated Redis required")
	}
	rds := redis.NewClient(&redis.Options{Addr: addr})
	cfg := diagnostics.DefaultConfig()
	cfg.BatchFlushInterval = 20 * time.Millisecond
	if e := diagnostics.GetInstance().Init(cfg, rds); e != nil {
		t.Fatal(e)
	}
	db, _, p := preparedFixture(t)
	root, good, bad := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{root, good, bad} {
		seedReliableDevice(t, db, id, "tenant-a")
	}
	for id, address := range map[string]string{good: "good", bad: "bad"} {
		if e := db.Exec("UPDATE devices SET parent_id=?,sub_device_addr=? WHERE id=?", root, address, id).Error; e != nil {
			t.Fatal(e)
		}
	}
	attachPrepared(t, db, bad)
	t.Cleanup(func() {
		for _, id := range []string{root, good, bad} {
			prefix := fmt.Sprintf("device:{%s}:diagnostics:v2:", id)
			rds.Del(context.Background(), prefix+"stats", prefix+"observations", prefix+"order", prefix+"failures")
		}
	})
	out := make(chan *storage.Message, 10)
	flow := NewAttributeUplink(AttributeUplinkConfig{DB: db, Contracts: p.contracts, StorageInput: out, Logger: logrus.New()})
	defer flow.Stop()
	body := []byte(`{"sub_device_data":{"good":{"legacy_value":21},"missing":{"x":1},"bad":{"temperature":"invalid"}}}`)
	flow.processMessage(&DeviceMessage{DeviceID: root, Type: "gateway_attribute", Payload: body, Timestamp: time.Now().UnixMilli(), Metadata: map[string]interface{}{"device_id": root}})
	var msg *storage.Message
	select {
	case msg = <-out:
		if msg.DeviceID != good {
			t.Fatal("invalid sibling reached storage")
		}
	default:
		t.Fatal("valid unbound sibling dropped")
	}
	select {
	case extra := <-out:
		t.Fatalf("unexpected storage message %s", extra.DeviceID)
	default:
	}
	// Drive the real storage service; substitute only the external automation
	// callback with a counter so this isolated fixture cannot call a real service.
	var callbacks atomic.Int32
	msg.AfterCommit = func() { callbacks.Add(1) }
	input := make(chan *storage.Message, 10)
	writer := storage.New(db, logrus.New(), storage.DefaultConfig())
	if e := writer.Start(context.Background(), input); e != nil {
		t.Fatal(e)
	}
	defer writer.Stop(time.Second)
	input <- msg
	deadline := time.Now().Add(3 * time.Second)
	for callbacks.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if callbacks.Load() != 1 {
		t.Fatal("valid sibling never committed")
	}
	var count int64
	if e := db.Table("attribute_datas").Where("device_id=? AND key='legacy_value'", good).Count(&count).Error; e != nil || count != 1 {
		t.Fatalf("actual storage count%d err%v", count, e)
	}
	for time.Now().Before(deadline) {
		d, e := diagnostics.GetInstance().GetDiagnostics(good)
		if e == nil && d.Stats.Uplink.Success == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	g, e := diagnostics.GetInstance().GetDiagnostics(good)
	if e != nil || g.Stats.Uplink.Total != 1 || g.Stats.Uplink.Success != 1 || g.Stats.Storage.Success != 1 {
		t.Fatalf("valid commit diagnostics %+v %v", g, e)
	}
	b, e := diagnostics.GetInstance().GetDiagnostics(bad)
	if e != nil || b.Stats.Uplink.Total != 1 || b.Stats.Uplink.Success != 0 || b.Stats.Uplink.Failed != 1 || b.Stats.Storage.Total != 0 {
		t.Fatalf("invalid child diagnostics %+v %v", b, e)
	}
	// An empty gateway envelope is zero logical messages, not eternal pending.
	empty, err := prepareBusMessage(context.Background(), db, p.contracts, nil, &DeviceMessage{DeviceID: root, Type: "gateway_attribute", Payload: []byte(`{"sub_device_data":{}}`), Metadata: map[string]interface{}{"device_id": root}}, logrus.New())
	if err != nil || len(batchObservations(empty, "tenant-a", root, "empty", "attribute")) != 0 {
		t.Fatal("empty envelope produced logical pending")
	}
	// Native INT overflow gets a distinct capacity failure and never queues _raw.
	contract, err := brokersemantic.CompileUplinkContract(json.RawMessage(strings.Replace(preparedContract, `"kind":"NUMBER"`, `"kind":"INT"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	flow.contracts = preparedContractReader{contract}
	flow.processMessage(&DeviceMessage{DeviceID: root, Type: "gateway_attribute", Payload: []byte(`{"sub_device_data":{"bad":{"temperature":1` + strings.Repeat("0", 400) + `}}}`), Metadata: map[string]interface{}{"device_id": root}})
	select {
	case <-out:
		t.Fatal("unrepresentable native numeric queued as raw")
	default:
	}
	b, e = diagnostics.GetInstance().GetDiagnostics(bad)
	if e != nil || b.Stats.Uplink.Total != 2 || b.Stats.Uplink.Failed != 2 || b.Stats.Storage.Total != 0 {
		t.Fatalf("capacity stats %+v %v", b, e)
	}
	found := false
	for _, failure := range b.RecentFailures {
		if strings.Contains(failure.Error, "存储范围") {
			found = true
		}
	}
	if !found {
		t.Fatal("capacity diagnosis absent")
	}
	// Native telemetry and event flows drive their real converters and writers.
	// One invalid field rejects the whole logical sample before current/history.
	nativeID := uuid.NewString()
	seedReliableDevice(t, db, nativeID, "tenant-a")
	attachPrepared(t, db, nativeID)
	t.Cleanup(func() {
		prefix := fmt.Sprintf("device:{%s}:diagnostics:v2:", nativeID)
		rds.Del(context.Background(), prefix+"stats", prefix+"observations", prefix+"order", prefix+"failures")
	})
	if e = db.AutoMigrate(&storage.TelemetryData{}, &storage.TelemetryCurrentData{}); e != nil {
		t.Fatal(e)
	}
	if e = db.Exec("CREATE UNIQUE INDEX ON telemetry_datas(device_id,key,ts)").Error; e != nil {
		t.Fatal(e)
	}
	if e = db.Exec("CREATE UNIQUE INDEX ON telemetry_current_datas(device_id,key)").Error; e != nil {
		t.Fatal(e)
	}
	nativeRaw := `{"name":"native bus","fields":[{"kind":"TELEMETRY","identifier":"n","nameI18n":{"default":"n"},"valueType":{"kind":"NUMBER"}},{"kind":"TELEMETRY","identifier":"b","nameI18n":{"default":"b"},"valueType":{"kind":"BOOL"}},{"kind":"TELEMETRY","identifier":"e","nameI18n":{"default":"e"},"valueType":{"kind":"ENUM","baseKind":"NUMBER","enumValues":[{"code":1,"label":{"default":"one"}}]}},{"kind":"EVENT","identifier":"changed","nameI18n":{"default":"changed"},"parameters":{"schemaVersion":1,"items":[{"identifier":"n","nameI18n":{"default":"n"},"valueType":{"kind":"NUMBER"}},{"identifier":"b","nameI18n":{"default":"b"},"valueType":{"kind":"BOOL"}}]}}]}`
	nativeContract, e := brokersemantic.CompileUplinkContract(json.RawMessage(nativeRaw))
	if e != nil {
		t.Fatal(e)
	}
	decoder := &decodeProcessorStub{}
	tf := NewTelemetryUplink(TelemetryUplinkConfig{DB: db, Contracts: preparedContractReader{nativeContract}, Processor: decoder, StorageInput: out, Logger: logrus.New()})
	defer tf.Stop()
	ef := NewEventUplink(EventUplinkConfig{DB: db, Contracts: preparedContractReader{nativeContract}, Processor: decoder, StorageInput: out, Logger: logrus.New()})
	defer ef.Stop()
	for _, tc := range []struct {
		family, body string
		valid        bool
	}{
		{"telemetry", `{"n":23.5,"b":true,"e":1}`, true},
		{"telemetry", `{"n":"99","b":true,"e":1}`, false},
		{"telemetry", `{"n":99,"b":"true","e":1}`, false},
		{"telemetry", `{"n":99,"b":true,"e":2}`, false},
		{"event", `{"method":"changed","params":{"n":23.5,"b":false},"ts":123}`, true},
		{"event", `{"method":"changed","params":{"n":"99","b":false}}`, false},
		{"event", `{"method":"changed","params":{"n":99,"b":"false"}}`, false},
	} {
		decoder.out = &processor.DecodeOutput{Success: true, Data: []byte(tc.body)}
		m := &DeviceMessage{DeviceID: nativeID, Type: tc.family, Payload: []byte(tc.body), Timestamp: time.Now().UnixMilli(), Metadata: map[string]interface{}{"device_id": nativeID}}
		if tc.family == "telemetry" {
			tf.processMessage(m)
		} else {
			ef.processMessage(m)
		}
		select {
		case stored := <-out:
			if !tc.valid {
				t.Fatalf("invalid %s reached storage", tc.body)
			}
			committed := make(chan struct{})
			stored.AfterCommit = func() { close(committed) }
			input <- stored
			select {
			case <-committed:
			case <-time.After(3 * time.Second):
				t.Fatalf("valid %s never committed", tc.family)
			}
		default:
			if tc.valid {
				t.Fatalf("valid %s rejected", tc.family)
			}
		}
	}
	for table, want := range map[string]int64{"telemetry_datas": 3, "telemetry_current_datas": 3, "event_datas": 1} {
		var n int64
		if e = db.Table(table).Where("device_id=?", nativeID).Count(&n).Error; e != nil || n != want {
			t.Fatalf("%s rows%d want%d err%v", table, n, want, e)
		}
	}
	var latest float64
	if e = db.Table("telemetry_current_datas").Select("number_v").Where("device_id=? AND key='n'", nativeID).Scan(&latest).Error; e != nil || latest != 23.5 {
		t.Fatalf("invalid overwrote current: %v %v", latest, e)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d, e := diagnostics.GetInstance().GetDiagnostics(nativeID)
		if e == nil && d.Stats.Uplink.Success == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	nativeDiag, e := diagnostics.GetInstance().GetDiagnostics(nativeID)
	if e != nil || nativeDiag.Stats.Uplink.Total != 7 || nativeDiag.Stats.Uplink.Success != 2 || nativeDiag.Stats.Uplink.Failed != 5 || nativeDiag.Stats.Uplink.Pending != 0 || nativeDiag.Stats.Storage.Success != 2 {
		t.Fatalf("native bus diagnostic mismatch %+v %v", nativeDiag, e)
	}
}
