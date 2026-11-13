package uplink

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"project/internal/brokerledger"
	"project/internal/brokersemantic"
	"project/internal/model"
	"project/internal/processor"
	"project/internal/uplinkbinding"
	"project/internal/uplinkcontract"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type preparedContractReader struct {
	contract *brokersemantic.UplinkContract
}

func TestPostgresPreparedRejectionKeepsBindingLockThroughDeadCommit(t *testing.T) {
	db, store, p := preparedFixture(t)
	seedReliableDevice(t, db, "d", "tenant-a")
	attachPrepared(t, db, "d")
	body := []byte(`{"temperature":"invalid"}`)
	p.decoder = &decodeProcessorStub{out: &processor.DecodeOutput{Success: true, Data: body}}
	for _, sql := range []string{
		`CREATE TABLE reject_audit(tx bigint, pin bigint)`,
		`CREATE FUNCTION audit_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='dead' THEN INSERT INTO reject_audit SELECT txid_current(),native_binding_revision FROM device_configs WHERE id='cfg'; END IF; RETURN NEW; END $$`,
		`CREATE TRIGGER audit_reject BEFORE UPDATE ON broker_ingress_receipts FOR EACH ROW EXECUTE FUNCTION audit_reject()`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	id, _, err := store.Accept(ctx, reliableMessage("d", "tenant-a", "attribute", body))
	if err != nil {
		t.Fatal(err)
	}
	var lockedTX int64
	updated := make(chan error, 1)
	_, err = store.ProcessOnePrepared(ctx, "community", func(ctx context.Context, read *gorm.DB, msg brokerledger.Message) (*brokerledger.PreparedOperation, error) {
		op, rejected := p.Prepare(ctx, read, msg)
		original := op.BeforeReject
		op.BeforeReject = func(ctx context.Context, tx *gorm.DB, msg brokerledger.Message) error {
			if e := original(ctx, tx, msg); e != nil {
				return e
			}
			if e := tx.Raw("SELECT txid_current()").Scan(&lockedTX).Error; e != nil {
				return e
			}
			go func() { updated <- db.Exec("UPDATE device_configs SET native_binding_revision=2 WHERE id='cfg'").Error }()
			select {
			case e := <-updated:
				t.Errorf("binding changed before terminal commit: %v", e)
			case <-time.After(80 * time.Millisecond):
			}
			return nil
		}
		return op, rejected
	})
	if !errors.Is(err, brokersemantic.ErrUplinkInvalid) {
		t.Fatal(err)
	}
	select {
	case e := <-updated:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("binding lock leaked")
	}
	var audit struct {
		TX  int64
		Pin int64
	}
	if e := db.Table("reject_audit").Take(&audit).Error; e != nil || audit.TX != lockedTX || audit.Pin != 1 {
		t.Fatalf("dead transition escaped validation transaction: %+v locked=%d err=%v", audit, lockedTX, e)
	}
	r, err := store.GetReceipt(ctx, "tenant-a", "d", id)
	if err != nil || r.State != "dead" || r.Attempts != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestPostgresPreparedReliableSchemaRejectionRechecksBindingBeforeDead(t *testing.T) {
	db, store, p := preparedFixture(t)
	ctx := context.Background()
	seedReliableDevice(t, db, "d", "tenant-a")
	attachPrepared(t, db, "d")
	body := []byte(`{"temperature":20}`)
	p.decoder = &decodeProcessorStub{out: &processor.DecodeOutput{Success: true, Data: body}}
	entered, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var pins uplinkcontract.Pins
		if json.NewDecoder(r.Body).Decode(&pins) != nil {
			w.WriteHeader(400)
			return
		}
		requests.Add(1)
		raw := preparedContract
		if pins.SnapshotID == "s" {
			close(entered)
			<-release
			raw = strings.Replace(raw, `"kind":"NUMBER"`, `"kind":"BOOL"`, 1)
		}
		h := sha256.Sum256([]byte(raw))
		json.NewEncoder(w).Encode(map[string]any{"schema_version": 1, "tenant_id": "tenant-a", "model_id": "m", "snapshot_id": pins.SnapshotID, "revision": pins.Revision, "contract_sha256": hex.EncodeToString(h[:]), "model": json.RawMessage(raw)})
	}))
	defer server.Close()
	client, err := uplinkcontract.NewClient(uplinkcontract.Config{BaseURL: server.URL, Token: strings.Repeat("x", 32), Deployment: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	p.contracts = client
	id, _, err := store.Accept(ctx, reliableMessage("d", "tenant-a", "attribute", body))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, e := store.ProcessOnePrepared(ctx, "community", p.Prepare); done <- e }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("no cold HTTP barrier")
	}
	updateErr := db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SET LOCAL lock_timeout='200ms'").Error; e != nil {
			return e
		}
		return tx.Exec(`UPDATE device_configs SET native_thing_model_snapshot_id='new',native_binding_revision=2 WHERE id='cfg'`).Error
	})
	close(release)
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	if err = <-done; !errors.Is(err, uplinkbinding.ErrFenced) {
		t.Fatalf("old schema rejection not fenced: %v", err)
	}
	r, err := store.GetReceipt(ctx, "tenant-a", "d", id)
	if err != nil || r.State != "pending" || r.Attempts != 1 {
		t.Fatalf("stale-schema result became terminal: %+v %v", r, err)
	}
	_, a, x := preparedCounts(t, db)
	if a+x != 0 {
		t.Fatal("old schema attempt wrote business")
	}
	if err = db.Exec("UPDATE broker_ingress_receipts SET retry_after=now() WHERE id=?", id).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = store.ProcessOnePrepared(ctx, "community", p.Prepare); err != nil {
		t.Fatal(err)
	}
	r, err = store.GetReceipt(ctx, "tenant-a", "d", id)
	if err != nil || r.State != "processed" || requests.Load() != 2 {
		t.Fatalf("new publication not revalidated: %+v calls%d err%v", r, requests.Load(), err)
	}
	_, a, x = preparedCounts(t, db)
	if a != 1 || x != 1 {
		t.Fatalf("reprepared outcome attrs%d effects%d", a, x)
	}
}

func (r preparedContractReader) Contract(context.Context, uplinkcontract.Pins) (*brokersemantic.UplinkContract, error) {
	return r.contract, nil
}

const preparedContract = `{"name":"fixture","fields":[{"kind":"ATTRIBUTE","identifier":"temperature","nameI18n":{"default":"T"},"access":{"read":true},"valueType":{"kind":"NUMBER"}},{"kind":"EVENT","identifier":"alarm","nameI18n":{"default":"E"},"access":{"observe":true},"parameters":{"schemaVersion":1,"items":[{"identifier":"value","nameI18n":{"default":"V"},"valueType":{"kind":"BOOL"}}]}}]}`

func preparedFixture(t *testing.T) (*gorm.DB, *brokerledger.Store, *ReliableProcessor) {
	t.Helper()
	db, store := reliableFixture(t)
	if err := db.AutoMigrate(&model.DeviceConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO device_configs(id,name,tenant_id,device_type,native_thing_model_id,native_thing_model_snapshot_id,native_thing_model_revision,native_binding_revision,created_at,updated_at,auto_register) VALUES('cfg','QA','tenant-a','1','m','s',1,1,now(),now(),0)`).Error; err != nil {
		t.Fatal(err)
	}
	c, err := brokersemantic.CompileUplinkContract(json.RawMessage(preparedContract))
	if err != nil {
		t.Fatal(err)
	}
	p := NewReliableProcessor(logrus.New()).WithContracts(preparedContractReader{c})
	return db, store, p
}
func attachPrepared(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	if err := db.Exec(`UPDATE devices SET device_config_id='cfg' WHERE id=?`, id).Error; err != nil {
		t.Fatal(err)
	}
}
func preparedCounts(t *testing.T, db *gorm.DB) (events, attributes, effects int64) {
	t.Helper()
	for table, out := range map[string]*int64{"event_datas": &events, "attribute_datas": &attributes, "broker_ingress_effects": &effects} {
		if err := db.Table(table).Count(out).Error; err != nil {
			t.Fatal(err)
		}
	}
	return
}
func TestPostgresPreparedReliableRejectsInvalidWithoutBusinessOrEffects(t *testing.T) {
	db, store, p := preparedFixture(t)
	ctx := context.Background()
	seedReliableDevice(t, db, "d", "tenant-a")
	attachPrepared(t, db, "d")
	for _, tc := range []struct {
		family, body string
		valid        bool
	}{
		{"event", `{"method":"alarm","params":{"value":false},"ts":7}`, true},
		{"event", `{"method":"alarm","params":{"value":"false"}}`, false},
		{"attribute", `{"temperature":20}`, true},
		{"attribute", `{"temperature":"99"}`, false},
		{"attribute", `{"temperature":40,"temperature":41}`, false},
	} {
		beforeE, beforeA, beforeX := preparedCounts(t, db)
		body := []byte(tc.body)
		p.decoder = &decodeProcessorStub{out: &processor.DecodeOutput{Success: true, Data: body}}
		id, _, err := store.Accept(ctx, reliableMessage("d", "tenant-a", tc.family, body))
		if err != nil {
			t.Fatal(err)
		}
		worked, err := store.ProcessOnePrepared(ctx, "community", p.Prepare)
		if !worked || (err == nil) != tc.valid {
			t.Fatalf("%s valid=%v worked=%v err=%v", tc.body, tc.valid, worked, err)
		}
		afterE, afterA, afterX := preparedCounts(t, db)
		r, e := store.GetReceipt(ctx, "tenant-a", "d", id)
		if e != nil {
			t.Fatal(e)
		}
		if !tc.valid {
			if !errors.Is(err, brokersemantic.ErrUplinkInvalid) || r.State != "dead" || r.Attempts != 1 || beforeE != afterE || beforeA != afterA || beforeX != afterX {
				t.Fatalf("invalid altered business or retried: %+v counts %d/%d %d/%d %d/%d err=%v", r, beforeE, afterE, beforeA, afterA, beforeX, afterX, err)
			}
		} else if r.State != "processed" || afterX != beforeX+1 {
			t.Fatalf("valid business did not commit: %+v", r)
		}
	}
	var value float64
	if err := db.Table("attribute_datas").Select("number_v").Where("device_id='d' AND key='temperature'").Scan(&value).Error; err != nil || value != 20 {
		t.Fatalf("invalid overwrote latest: %v %v", value, err)
	}
}

func TestPostgresPreparedReliableGatewayAtomicAndBindingFence(t *testing.T) {
	db, store, p := preparedFixture(t)
	ctx := context.Background()
	for _, id := range []string{"g", "a", "b"} {
		seedReliableDevice(t, db, id, "tenant-a")
	}
	for _, id := range []string{"a", "b"} {
		attachPrepared(t, db, id)
		if err := db.Exec(`UPDATE devices SET parent_id='g',sub_device_addr=? WHERE id=?`, id, id).Error; err != nil {
			t.Fatal(err)
		}
	}
	bad := []byte(`{"sub_device_data":{"a":{"temperature":10},"b":{"temperature":"20"}}}`)
	id, _, err := store.Accept(ctx, reliableMessage("g", "tenant-a", "gateway_attribute", bad))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ProcessOnePrepared(ctx, "community", p.Prepare); !errors.Is(err, brokersemantic.ErrUplinkInvalid) {
		t.Fatal(err)
	}
	e, a, x := preparedCounts(t, db)
	if e+a+x != 0 {
		t.Fatalf("partial gateway write: %d %d %d", e, a, x)
	}
	r, err := store.GetReceipt(ctx, "tenant-a", "g", id)
	if err != nil || r.State != "dead" {
		t.Fatalf("bad child receipt: %+v %v", r, err)
	}
	good := []byte(`{"sub_device_data":{"a":{"temperature":10},"b":{"temperature":20}}}`)
	m := reliableMessage("g", "tenant-a", "gateway_attribute", good)
	m.ID = "prepared-fixture"
	handler, err := p.Prepare(ctx, db, m)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`UPDATE device_configs SET native_binding_revision=2 WHERE id='cfg'`).Error; err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error { return handler.Write(ctx, tx, m) })
	if !errors.Is(err, uplinkbinding.ErrFenced) {
		t.Fatalf("rebind not fenced: %v", err)
	}
	e, a, x = preparedCounts(t, db)
	if e+a+x != 0 {
		t.Fatal("fenced gateway produced business effect")
	}
	if _, _, err = store.Accept(ctx, reliableMessage("g", "tenant-a", "gateway_attribute", good)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ProcessOnePrepared(ctx, "community", p.Prepare); err != nil {
		t.Fatal(err)
	}
	e, a, x = preparedCounts(t, db)
	if e != 0 || a != 2 || x != 2 {
		t.Fatalf("valid gateway incomplete: %d %d %d", e, a, x)
	}
	duplicate := []byte(`{"sub_device_data":{"a":{"temperature":11},"a":{"temperature":12}}}`)
	if _, _, err = store.Accept(ctx, reliableMessage("g", "tenant-a", "gateway_attribute", duplicate)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ProcessOnePrepared(ctx, "community", p.Prepare); !errors.Is(err, brokersemantic.ErrUplinkInvalid) {
		t.Fatalf("duplicate envelope accepted: %v", err)
	}
	_, a, x = preparedCounts(t, db)
	if a != 2 || x != 2 {
		t.Fatal("duplicate envelope emitted effect")
	}
}
