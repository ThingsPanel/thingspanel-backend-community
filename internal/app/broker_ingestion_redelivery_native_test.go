package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	mqttadapter "project/internal/adapter/mqttadapter"
	"project/internal/brokercontract"
	"project/internal/brokercontrol"
	"project/internal/brokerledger"
	"project/internal/query"
	"project/internal/uplink"
	"project/pkg/global"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const nativeRedeliveryClientID = "mqtt-e2e-redelivery"

type acceptThenHold struct {
	acceptor  *brokerIngressAcceptor
	committed chan struct{}
}

type trackedLedgerAccept struct {
	store  *brokerledger.Store
	result chan trackedAcceptResult
}

type trackedAcceptResult struct {
	id        string
	duplicate bool
	err       error
}

func (s *trackedLedgerAccept) Accept(ctx context.Context, message brokerledger.Message) (string, bool, error) {
	id, duplicate, err := s.store.Accept(ctx, message)
	if s.result != nil {
		select {
		case s.result <- trackedAcceptResult{id: id, duplicate: duplicate, err: err}:
		default:
		}
	}
	return id, duplicate, err
}

func (a *acceptThenHold) Accept(ctx context.Context, message *uplink.DeviceMessage) error {
	if err := a.acceptor.Accept(ctx, message); err != nil {
		return err
	}
	select {
	case a.committed <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

// This is a real broker disconnect/redelivery test, not a process-kill or OS
// crash test. The first handler commits its receipt and disconnects without
// acknowledging; a new Adapter resumes the same persistent service session.
func TestBrokerIngressNativeReceiptSurvivesSubscriberDisconnectBeforeAck(t *testing.T) {
	if os.Getenv("TEST_MQTT_NATIVE_E2E") != "1" {
		t.Skip("set TEST_MQTT_NATIVE_E2E=1 to run native broker redelivery test")
	}
	t.Setenv("TP_BROKER_ROUTE_FENCING", "1")
	root := repoRootForNativeE2E(t)
	mosquittoPath := nativeE2EPath("MOSQUITTO_BIN", "/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto")
	includePath := nativeE2EPath("MOSQUITTO_INCLUDE_DIR", "/tmp/tp-mqtt-20261019/mosquitto/include")
	cjsonPath := nativeE2EPath("CJSON_INCLUDE_DIR", "/opt/homebrew/opt/cjson/include")
	redisPath := nativeE2EPath("REDIS_SERVER", "/opt/homebrew/bin/redis-server")
	for _, path := range []string{mosquittoPath, filepath.Join(includePath, "mosquitto_broker.h"), cjsonPath, redisPath} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("native MQTT dependency unavailable: %v", err)
		}
	}
	db, _, closeDB := nativeE2EPostgres(t)
	_ = closeDB // query package retains the pool for the test process lifetime.
	if err := migrateNativeE2ESchema(t, db, root); err != nil {
		t.Fatal(err)
	}
	previousDB, previousRedis, previousStatusRedis := global.DB, global.REDIS, global.STATUS_REDIS
	previousDeviceQuery, previousCommandQuery := query.Device, query.CommandSetLog
	query.SetDefault(db)
	global.DB = db
	redisClient, stopRedis := startNativeE2ERedis(t, redisPath)
	t.Cleanup(stopRedis)
	global.REDIS, global.STATUS_REDIS = redisClient, redisClient
	t.Cleanup(func() {
		global.DB, global.REDIS, global.STATUS_REDIS = previousDB, previousRedis, previousStatusRedis
		query.Device, query.CommandSetLog = previousDeviceQuery, previousCommandQuery
	})
	seedNativeE2EData(t, db)

	tokenBytes := make([]byte, 48)
	if _, err := rand.Read(tokenBytes); err != nil {
		t.Fatal(err)
	}
	ownerToken := hex.EncodeToString(tokenBytes)
	owner, err := brokercontrol.NewOwner(brokercontrol.NewGormRepository(db), brokercontrol.Config{
		Tokens: []brokercontrol.TokenBinding{{Token: ownerToken, BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode}},
		Services: []brokercontrol.ServiceCredential{{
			Username: "mqtt-e2e-service", Password: "e2e-service-password", BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode,
			AllowedClientIDs: []string{nativeRedeliveryClientID}, AllowedDeviceIDs: []string{nativeE2EDevice},
			Principal:     bcPrincipalService(),
			Policy:        brokercontract.Policy{Publish: []string{"devices/event/response/+/+", "devices/attributes/response/+/+"}, Subscribe: []string{brokercontract.IngressPrefix + "/#", brokercontract.StatePrefix + "/#"}, MaxPayload: brokercontract.MaxPayloadBytes, MaxQoS: 1, RetainProfile: brokercontract.RetainedLiveIngress},
			PolicyVersion: "native-redelivery-v1", MappingVersion: "standard-v1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerServer := httptest.NewServer(owner.RuntimeHandler(brokercontrol.NewMigrationStore(db)))
	t.Cleanup(ownerServer.Close)
	tokenPath := filepath.Join(t.TempDir(), "broker-owner.token")
	if err := os.WriteFile(tokenPath, []byte(ownerToken), 0o600); err != nil {
		t.Fatal(err)
	}
	plugin := buildNativeE2EPlugin(t, root, includePath, cjsonPath)
	brokerAddr, brokerLog, stopBroker := startNativeE2EBroker(t, mosquittoPath, plugin, ownerServer.URL, tokenPath, root)
	t.Cleanup(stopBroker)
	setNativeE2EViper(t, brokerAddr)
	previousFencing, previousRuntimeInstance := viper.GetBool("mqtt.route_fencing"), viper.GetString("mqtt.runtime_instance")
	viper.Set("mqtt.route_fencing", true)
	viper.Set("mqtt.runtime_instance", "mqtt-e2e-redelivery-backend")
	t.Cleanup(func() {
		viper.Set("mqtt.route_fencing", previousFencing)
		viper.Set("mqtt.runtime_instance", previousRuntimeInstance)
	})

	log := logrus.New()
	log.SetOutput(io.Discard)
	device := connectNativeE2EDevice(t, context.Background(), brokerAddr)
	bus := uplink.NewBus(uplink.BusConfig{BufferSize: 8}, log)
	t.Cleanup(bus.Close)
	base := mqttadapter.MQTTConfig{Broker: brokerAddr, Username: "mqtt-e2e-service", Password: "e2e-service-password", ClientID: nativeRedeliveryClientID, ManualAck: true}
	firstCtx, firstCancel := context.WithCancel(context.Background())
	committed := make(chan struct{}, 1)
	firstResultCh := make(chan trackedAcceptResult, 1)
	var firstAdapter *mqttadapter.Adapter
	base.BeforeConnect = func(client mqtt.Client) error {
		firstAdapter = mqttadapter.NewAdapter(bus, client, log)
		firstAdapter.SetContext(firstCtx)
		if err := firstAdapter.ConfigureIngressFromConfig(); err != nil {
			return err
		}
		firstAdapter.SetIngressAcceptor(&acceptThenHold{acceptor: &brokerIngressAcceptor{store: &trackedLedgerAccept{store: brokerledger.New(db), result: firstResultCh}}, committed: committed})
		firstAdapter.RegisterIngressRoutes(client)
		return nil
	}
	firstClient, err := mqttadapter.CreateMQTTClientContext(firstCtx, base, log)
	if err != nil {
		t.Fatalf("connect first persistent service session: %v", err)
	}
	if firstAdapter == nil {
		t.Fatal("BeforeConnect did not prepare first Adapter")
	}
	if err := firstAdapter.SubscribeAllContext(firstCtx, firstClient); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"temperature":22.4}`)
	publishNativeE2E(t, context.Background(), device, "devices/attributes/redelivery-sample", payload)
	select {
	case <-committed:
	case <-time.After(10 * time.Second):
		t.Fatal("first acceptor did not commit the ingress receipt")
	}
	firstResult := <-firstResultCh
	if firstResult.err != nil || firstResult.duplicate || firstResult.id == "" {
		t.Fatalf("first acceptance result=%+v, want new committed receipt", firstResult)
	}
	firstReceipt := firstResult.id
	firstCancel()
	firstClient.Disconnect(250)
	waitNativeE2E(t, 3*time.Second, "first service client disconnect", func() bool { return !firstClient.IsConnected() })

	secondCtx, secondCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer secondCancel()
	secondResultCh := make(chan trackedAcceptResult, 8)
	var secondAdapter *mqttadapter.Adapter
	base.BeforeConnect = func(client mqtt.Client) error {
		secondAdapter = mqttadapter.NewAdapter(bus, client, log)
		secondAdapter.SetContext(secondCtx)
		if err := secondAdapter.ConfigureIngressFromConfig(); err != nil {
			return err
		}
		secondAdapter.SetIngressAcceptor(&brokerIngressAcceptor{store: &trackedLedgerAccept{store: brokerledger.New(db), result: secondResultCh}})
		secondAdapter.RegisterIngressRoutes(client)
		return nil
	}
	secondClient, err := mqttadapter.CreateMQTTClientContext(secondCtx, base, log)
	if err != nil {
		t.Fatalf("resume persistent service session: %v", err)
	}
	if secondAdapter == nil {
		t.Fatal("BeforeConnect did not prepare second Adapter")
	}
	if err := secondAdapter.SubscribeAllContext(secondCtx, secondClient); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { secondCancel(); secondClient.Disconnect(250) })
	var redelivery trackedAcceptResult
	select {
	case redelivery = <-secondResultCh:
	case <-secondCtx.Done():
		t.Fatalf("persistent-session redelivery was not accepted: %v; broker log: %s", secondCtx.Err(), nativeE2ETail(brokerLog))
	}
	if redelivery.err != nil || !redelivery.duplicate || redelivery.id != firstReceipt {
		t.Fatalf("redelivery result=%+v, want duplicate of receipt %s", redelivery, firstReceipt)
	}
	waitNativeE2E(t, 10*time.Second, "same committed receipt acknowledged after redelivery", func() bool {
		var count int64
		return db.Table("broker_ingress_receipts").Where("tenant_id=? AND device_id=? AND stream='attribute' AND id=?", nativeE2ETenant, nativeE2EDevice, firstReceipt).Count(&count).Error == nil && count == 1 && secondClient.IsConnected()
	})

	store := brokerledger.New(db)
	processor := uplink.NewReliableProcessor(log)
	worked, err := store.ProcessOne(context.Background(), processor.Process)
	if err != nil || !worked {
		t.Fatalf("process redelivered receipt once: worked=%v err=%v", worked, err)
	}
	worked, err = store.ProcessOne(context.Background(), processor.Process)
	if err != nil || worked {
		t.Fatalf("second process must not repeat business/effect: worked=%v err=%v", worked, err)
	}
	var receipts, effects, attributes int64
	if err := db.Table("broker_ingress_receipts").Where("tenant_id=? AND device_id=? AND stream='attribute' AND id=? AND state='processed'", nativeE2ETenant, nativeE2EDevice, firstReceipt).Count(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("broker_ingress_effects").Where("receipt_id=? AND effect_kind='automation'", firstReceipt).Count(&effects).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("attribute_datas").Where("device_id=? AND key='temperature'", nativeE2EDevice).Count(&attributes).Error; err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || effects != 1 || attributes != 1 {
		t.Fatalf("dedup result receipts=%d effects=%d attributes=%d, want 1 each", receipts, effects, attributes)
	}
}

func bcPrincipalService() brokercontract.Principal {
	return brokercontract.Principal{TenantID: nativeE2ETenant, DeviceID: "service-mqtt-e2e", Kind: brokercontract.PrincipalService}
}
