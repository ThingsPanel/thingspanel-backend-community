package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"project/internal/brokercontract"
	"project/internal/brokercontrol"
)

// This exercises two real native brokers and two independent backend runtimes.
// It does not migrate production devices, legacy GMQTT, queued commands or disk.
func TestBrokerMigrationNativeTwoCellRoundTrip(t *testing.T) {
	if os.Getenv("TEST_MQTT_NATIVE_MIGRATION_E2E") != "1" {
		t.Skip("set TEST_MQTT_NATIVE_MIGRATION_E2E=1 for isolated native A-to-B-to-A migration")
	}
	root := repoRootForNativeE2E(t)
	binary := nativeE2EPath("MOSQUITTO_BIN", "/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto")
	include := nativeE2EPath("MOSQUITTO_INCLUDE_DIR", "/tmp/tp-mqtt-20261019/mosquitto/include")
	cjson := nativeE2EPath("CJSON_INCLUDE_DIR", "/opt/homebrew/opt/cjson/include")
	redisBinary := nativeE2EPath("REDIS_SERVER", "/opt/homebrew/bin/redis-server")
	for _, path := range []string{binary, filepath.Join(include, "mosquitto_broker.h"), cjson, redisBinary} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("explicitly enabled native migration dependency unavailable")
		}
	}
	db, schema, closeDB := nativeE2EPostgres(t)
	t.Cleanup(closeDB)
	if pool, err := db.DB(); err == nil {
		pool.SetMaxOpenConns(4)
	}
	if err := migrateNativeE2ESchema(t, db, root); err != nil {
		t.Fatal("migrate private round-trip schema")
	}
	seedNativeE2EData(t, db)
	redisClient, _ := startNativeE2ERedis(t, redisBinary)
	dsnFile := filepath.Join(t.TempDir(), "qa-dsn")
	if err := writeMigrationCellPrivateFile(dsnFile, []byte(nativeE2EQADataSource(t))); err != nil {
		t.Fatal("write private child database configuration")
	}

	cells := []struct {
		cell, node, client, username, password, token, addr, log string
	}{
		{cell: nativeE2ECell, node: nativeE2ENode, client: "migration-service-a", username: "migration-service-a", password: "isolated-a-password"},
		{cell: "community-target", node: "node-b", client: "migration-service-b", username: "migration-service-b", password: "isolated-b-password"},
	}
	config := brokercontrol.Config{}
	for i := range cells {
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			t.Fatal("create private owner token")
		}
		cell := &cells[i]
		cell.token = hex.EncodeToString(token[:])
		config.Tokens = append(config.Tokens, brokercontrol.TokenBinding{Token: cell.token, BrokerCell: cell.cell, BrokerInstance: cell.node})
		config.Services = append(config.Services, brokercontrol.ServiceCredential{
			Username: cell.username, Password: cell.password, BrokerCell: cell.cell, BrokerInstance: cell.node,
			AllowedClientIDs: []string{cell.client}, AllowedDeviceIDs: []string{nativeE2EDevice},
			Principal: brokercontract.Principal{TenantID: nativeE2ETenant, DeviceID: cell.client, Kind: brokercontract.PrincipalService},
			Policy: brokercontract.Policy{
				Publish:    []string{"devices/command/native-e2e-number/+", "devices/event/response/+/+", "devices/attributes/response/+/+"},
				Subscribe:  []string{brokercontract.IngressPrefix + "/#", brokercontract.StatePrefix + "/#"},
				MaxPayload: brokercontract.MaxPayloadBytes, MaxQoS: 1, RetainProfile: brokercontract.RetainedLiveIngress,
			},
			PolicyVersion: "migration-test-v1", MappingVersion: "standard-v1",
		})
	}
	owner, err := brokercontrol.NewOwner(brokercontrol.NewGormRepository(db), config)
	if err != nil {
		t.Fatal("create private dual-cell owner")
	}
	store := brokercontrol.NewMigrationStore(db)
	ownerServer := httptest.NewServer(owner.RuntimeHandler(store))
	t.Cleanup(ownerServer.Close)
	plugin := buildNativeE2EPlugin(t, root, include, cjson)
	for i := range cells {
		cell := &cells[i]
		tokenFile := filepath.Join(t.TempDir(), "owner-token")
		if err := writeMigrationCellPrivateFile(tokenFile, []byte(cell.token)); err != nil {
			t.Fatal("write private owner token")
		}
		cell.addr, cell.log, _ = startMigrationCellBroker(t, migrationCellBrokerConfig{
			Binary: binary, Plugin: plugin, OwnerURL: ownerServer.URL, TokenPath: tokenFile, Root: root, Cell: cell.cell, Instance: cell.node,
		})
		startMigrationCellRuntime(t, MigrationCellRuntimeConfig{
			DSNFile: dsnFile, Schema: schema, RedisAddr: redisClient.Options().Addr, BrokerAddr: cell.addr,
			Cell: cell.cell, Instance: cell.node, ClientID: cell.client, ServiceUsername: cell.username, ServicePassword: cell.password,
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	for _, cell := range cells {
		waitNativeE2E(t, 12*time.Second, "four real runtime members in "+cell.cell, func() bool {
			members, _, err := store.Members(ctx, cell.cell)
			return err == nil && len(members) == 4
		})
		members, digest, _ := store.Members(ctx, cell.cell)
		snapshot, _ := json.Marshal(members)
		t.Logf("actual runtime roster cell=%s members_sha256=%s members=%s", cell.cell, digest, snapshot)
	}
	device := connectNativeE2EDevice(t, ctx, cells[0].addr)
	publish := func(client mqtt.Client, source string) {
		payload, _ := json.Marshal(map[string]any{"source_event_id": source, "method": "migration", "params": map[string]any{"value": 1}})
		publishNativeE2E(t, ctx, client, "devices/event/"+source, payload)
	}
	publish(device, "migration-initial")
	waitNativeE2EReceipt(t, db, "migration-initial", "processed")
	assertNativeE2ECounts(t, db, "event_datas", 1)
	var initialConnection string
	waitNativeE2E(t, 5*time.Second, "initial actual device connection", func() bool {
		return db.Table("broker_connection_observations").Select("connection_id").Where("tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=1 AND online", nativeE2ETenant, nativeE2EDevice, cells[0].cell).Scan(&initialConnection).Error == nil && initialConnection != ""
	})
	previousConnection := initialConnection
	connectionsByCell := map[string]string{cells[0].cell: initialConnection}

	for leg := 0; leg < 2; leg++ {
		source, target := cells[leg%2], cells[(leg+1)%2]
		epoch := int64(leg + 1)
		_, membersSHA, err := store.Members(ctx, source.cell)
		if err != nil {
			t.Fatal("read source runtime roster")
		}
		changeID := uuid.NewString()
		plan := sha256.Sum256([]byte(fmt.Sprintf("isolated-roundtrip:%s:%s:%s:%d", changeID, source.cell, target.cell, epoch)))
		if _, err := store.Freeze(ctx, brokercontrol.FreezeRouteRequest{
			ChangeID: changeID, TenantID: nativeE2ETenant, DeviceID: nativeE2EDevice, ExpectedCell: source.cell, TargetCell: target.cell,
			ExpectedEpoch: epoch, Actor: "isolated-migration-test", MembersSHA256: membersSHA, PlanSHA256: hex.EncodeToString(plan[:]),
		}); err != nil {
			t.Fatal("freeze source route")
		}
		waitNativeE2E(t, 12*time.Second, "four real generation-bound fence acknowledgements", func() bool {
			var n int64
			return db.Table("broker_route_barrier_members").Where("change_id=?::uuid AND acknowledged_at IS NOT NULL", changeID).Count(&n).Error == nil && n == 4
		})
		var acknowledgements []struct {
			Role, InstanceID, BootID, Generation string
			AcknowledgedAt                       time.Time
		}
		if err := db.Table("broker_route_barrier_members").Select("role,instance_id,boot_id,generation,acknowledged_at").Where("change_id=?::uuid", changeID).Order("role,instance_id").Find(&acknowledgements).Error; err != nil {
			t.Fatal("read actual barrier evidence")
		}
		barrierJSON, _ := json.Marshal(acknowledgements)
		t.Logf("source fence change_id=%s cell=%s epoch=%d acknowledgements=%s", changeID, source.cell, epoch, barrierJSON)
		before, _ := os.ReadFile(source.log)
		denied := strings.Count(string(before), "Denied PUBLISH")
		deniedID := fmt.Sprintf("migration-denied-epoch-%d", epoch)
		publish(device, deniedID)
		waitNativeE2E(t, 3*time.Second, "source broker rejects new publish after fence ACK", func() bool {
			log, _ := os.ReadFile(source.log)
			return strings.Count(string(log), "Denied PUBLISH") > denied
		})
		if nativeE2EReceiptCount(t, db, nativeE2ETenant, nativeE2EDevice, deniedID) != 0 {
			t.Fatal("frozen source admitted new business ingress")
		}
		device.Disconnect(250)
		ready := sha256.Sum256([]byte("isolated-target-runtime-ready:" + target.cell + ":" + target.node))
		if err := store.PrepareTarget(ctx, nativeE2ETenant, nativeE2EDevice, changeID, hex.EncodeToString(ready[:])); err != nil {
			t.Fatal("prepare target route")
		}
		if err := store.Activate(ctx, nativeE2ETenant, nativeE2EDevice, changeID); !errors.Is(err, brokercontrol.ErrTargetUnproven) {
			t.Fatal("target activated before fresh device access evidence")
		}
		device = connectNativeE2EDevice(t, ctx, target.addr)
		sourceID := fmt.Sprintf("migration-target-epoch-%d", epoch+1)
		publish(device, sourceID)
		waitNativeE2EReceipt(t, db, sourceID, "pending")
		if nativeE2EReceiptCount(t, db, nativeE2ETenant, nativeE2EDevice, sourceID) != 1 {
			t.Fatal("target event must have exactly one scoped receipt")
		}
		var connection struct {
			ConnectionID, NodeBootID, BrokerInstance string
			ObservedAt, ReceivedAt                   time.Time
		}
		waitNativeE2E(t, 5*time.Second, "target actual new-epoch connection observation", func() bool {
			return db.Table("broker_connection_observations").Select("connection_id,node_boot_id,broker_instance,observed_at,received_at").Where("tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=? AND online AND received_at>=now()-interval '30 seconds'", nativeE2ETenant, nativeE2EDevice, target.cell, epoch+1).Take(&connection).Error == nil && connection.ConnectionID != "" && connection.NodeBootID != "" && connection.BrokerInstance == target.node && connection.ConnectionID != previousConnection && connection.ConnectionID != connectionsByCell[target.cell]
		})
		assertNativeE2ECounts(t, db, "event_datas", int64(leg+1))
		var receipt struct {
			ID, BrokerCell string
			RouteEpoch     int64
			State          string
		}
		if err := db.Table("broker_ingress_receipts").Select("id,broker_cell,route_epoch,state").Where("tenant_id=? AND device_id=? AND source_event_id=?", nativeE2ETenant, nativeE2EDevice, sourceID).Take(&receipt).Error; err != nil || receipt.ID == "" || receipt.BrokerCell != target.cell || receipt.RouteEpoch != epoch+1 || receipt.State != "pending" {
			t.Fatal("connecting receipt scope or pending-state proof failed")
		}
		if err := store.Activate(ctx, nativeE2ETenant, nativeE2EDevice, changeID); err != nil {
			t.Fatal("activate target after real connection and ingress")
		}
		waitNativeE2EReceipt(t, db, sourceID, "processed")
		assertNativeE2ECounts(t, db, "event_datas", int64(leg+2))
		var effectCount int64
		if err := db.Table("broker_ingress_effects").Where("receipt_id IN (SELECT id FROM broker_ingress_receipts WHERE tenant_id=? AND device_id=? AND source_event_id=?)", nativeE2ETenant, nativeE2EDevice, sourceID).Count(&effectCount).Error; err != nil || effectCount != 1 {
			t.Fatal("target event must commit exactly one business effect intent")
		}
		connectionJSON, _ := json.Marshal(connection)
		t.Logf("target activated change_id=%s cell=%s epoch=%d receipt_id=%s source_event_id=%s connecting_state=pending final_state=processed business_events=%d effect_intents=1 connection=%s", changeID, target.cell, epoch+1, receipt.ID, sourceID, leg+2, connectionJSON)
		previousConnection = connection.ConnectionID
		connectionsByCell[target.cell] = connection.ConnectionID
	}
	var binding brokercontrol.TransportBinding
	if err := db.Where("tenant_id=? AND device_id=?", nativeE2ETenant, nativeE2EDevice).Take(&binding).Error; err != nil || binding.BrokerCell != cells[0].cell || binding.RouteEpoch != 3 || binding.Phase != "active" {
		t.Fatal("round-trip must return to A at a new active epoch 3")
	}
	assertNativeE2ECounts(t, db, "event_datas", 3)
	assertNativeE2ECounts(t, db, "broker_command_requests", 0)
	t.Log("isolated native A/1 -> B/2 -> A/3: real four-role barriers, connecting receipts and business processing verified; no command or physical-effect certification")
}
