package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"project/internal/brokercommands"
	"project/internal/downlink"
	"project/internal/model"
	"project/pkg/global"
)

func TestNativeCommandOwnerValidationAllPreparationPaths(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	checkNativeCommandOwnerPreparation(t, db)
}

func TestPostgresNativeCommandOwnerValidationAllPreparationPaths(t *testing.T) {
	checkNativeCommandOwnerPreparation(t, brokerEvidenceTestDB(t))
}

func checkNativeCommandOwnerPreparation(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&model.Device{}, &model.DeviceConfig{}, &model.DataScript{}, &model.CommandSetLog{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE device_topic_mappings (id INTEGER,device_config_id TEXT,direction TEXT,source_topic TEXT,target_topic TEXT,priority INTEGER,data_identifier TEXT,enabled BOOLEAN)`).Error; err != nil {
		t.Fatal(err)
	}
	old := global.DB
	global.DB = db
	t.Cleanup(func() { global.DB = old })
	config := nativeCommandDeviceConfig("model", "snapshot", 7)
	config.ID = "config"
	config.TenantID = "tenant-a"
	config.DeviceType = "1"
	if err := db.Create(config).Error; err != nil {
		t.Fatal(err)
	}
	device := model.Device{ID: "device", TenantID: "tenant-a", DeviceConfigID: &config.ID, DeviceNumber: "number", IsEnabled: "enabled", ActivateFlag: "active"}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	const token = "test-owner-token-32-characters-long"
	t.Setenv("GOTP_AUTOMATION_TOKEN", token)
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment")
	calls, status := 0, 200
	timeoutEntered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/internal/thingmodel/validate-command" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-Automation-Tenant") != "tenant-a" || r.Header.Get("X-Automation-Deployment") != "deployment" {
			t.Error("wrong owner context")
		}
		var input nativeCommandValidationRequest
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.DeviceID != device.ID || input.ModelID != "model" || input.Revision != 7 || input.SnapshotID != "snapshot" || input.BindingRevision != 1 {
			t.Errorf("incorrect binding: %+v", input)
		}
		if status == 999 {
			timeoutEntered <- struct{}{}
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		w.WriteHeader(status)
		if status == 200 {
			fmt.Fprintf(w, `{"valid":true,"device_id":"device","snapshot_id":"snapshot","binding_revision":1}`)
		}
	}))
	defer server.Close()
	t.Setenv("TP_THINGMODEL_NATIVE_URL", server.URL)
	data := map[string]interface{}{"method": "set", "params": json.RawMessage(`{"value":true}`)}
	message := &downlink.Message{DeviceID: device.ID, DeviceNumber: device.DeviceNumber, DeviceConfigID: config.ID, Type: downlink.MessageTypeCommand}
	for _, tc := range []struct {
		name    string
		prepare func() error
	}{
		{"manual durable", func() error {
			_, err := prepareDurableDownlink(context.Background(), db, message, device.TenantID, device.ID, device.ID, "1", data)
			return err
		}},
		{"automation and queued preparation", func() error {
			_, _, err := prepareExpectedDownlink(context.Background(), &model.ExpectedData{TenantID: device.TenantID, DeviceID: device.ID, SendType: "command", Payload: `{"method":"set","params":{"value":true}}`})
			return err
		}},
		{"delayed admission", func() error {
			return validateStoredNativeCommand(context.Background(), db, &model.ExpectedData{TenantID: device.TenantID, DeviceID: device.ID, SendType: "command", Payload: `{"method":"set","params":{"value":true}}`})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, code := range []int{200, 400, 401, 404, 409, 503} {
				status = code
				before := calls
				err := tc.prepare()
				if calls != before+1 || (err == nil) != (code == 200) {
					t.Fatalf("status=%d calls=%d error=%v", code, calls-before, err)
				}
			}
		})
	}
	status = 200
	if db.Dialector.Name() == "postgres" {
		if err := db.Exec(`CREATE TABLE automation_scope_generation (scope_key text PRIMARY KEY,generation bigint NOT NULL,updated_at timestamptz NOT NULL)`).Error; err != nil {
			t.Fatal(err)
		}
		oldProfile := viper.Get("mqtt.input_profile")
		viper.Set("mqtt.input_profile", "ingress_v1")
		t.Cleanup(func() { viper.Set("mqtt.input_profile", oldProfile) })
		t.Setenv("GOTP_AUTOMATION_MODE", "native")
		status = 400
		before := calls
		id, err := SubmitNativeAutomationCommand(context.Background(), NativeCommand{TenantID: device.TenantID, DeploymentID: "deployment", DeviceID: device.ID, Kind: "command", Identifier: "set", Value: `{"value":"invalid-bool"}`, Key: "new-intent", Scope: "qa-scope", Generation: 1})
		if id != "" || !errors.Is(err, brokercommands.ErrInvalid) || calls != before+1 {
			t.Fatalf("automation bypassed semantic rejection: id=%s err=%v calls=%d", id, err, calls-before)
		}
		status = 200
		// The owner never calls Community back. A failed bounded RPC releases the
		// preparation's SHARE locks, permitting a competing binding update.
		status = 999
		finished := make(chan error, 1)
		started := time.Now()
		go func() {
			_, err := prepareDurableDownlink(context.Background(), db, message, device.TenantID, device.ID, device.ID, "1", data)
			finished <- err
		}()
		select {
		case <-timeoutEntered:
		case <-time.After(time.Second):
			t.Fatal("owner request did not start")
		}
		updated := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			updated <- db.WithContext(ctx).Model(config).Update("native_binding_revision", 1).Error
		}()
		select {
		case err := <-updated:
			t.Fatalf("configuration was not protected by preparation lock: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		if err := <-finished; err == nil || time.Since(started) > 4*time.Second {
			t.Fatalf("unbounded owner request: %v elapsed=%s", err, time.Since(started))
		}
		if err := <-updated; err != nil {
			t.Fatalf("owner timeout left a database lock: %v", err)
		}
		status = 200
	}
	accepted, err := prepareDurableDownlink(context.Background(), db, message, device.TenantID, device.ID, device.ID, "1", data)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{"native_thing_model_id": "other-model", "native_thing_model_snapshot_id": "other-snapshot", "native_thing_model_revision": 8, "native_binding_revision": 2} {
		original := map[string]any{"native_thing_model_id": "model", "native_thing_model_snapshot_id": "snapshot", "native_thing_model_revision": 7, "native_binding_revision": 1}
		if err := db.Model(config).Update(key, value).Error; err != nil {
			t.Fatal(err)
		}
		fingerprint, _, _, err := readCommandFingerprint(context.Background(), db, device.TenantID, device.ID, device.ID, config.ID)
		if err != nil || fingerprint == accepted.ConfigurationSHA256 {
			t.Fatalf("binding %s did not fence: %v", key, err)
		}
		if err := db.Model(config).Updates(original).Error; err != nil {
			t.Fatal(err)
		}
	}
	before := calls
	_, _, err = prepareExpectedDownlink(context.Background(), &model.ExpectedData{TenantID: "tenant-b", DeviceID: device.ID, SendType: "command", Payload: `{"method":"set","params":{}}`})
	if err == nil || calls != before {
		t.Fatal("cross-tenant device reached model owner")
	}
	if err := db.Model(config).Updates(map[string]any{"native_thing_model_id": nil, "native_thing_model_snapshot_id": nil, "native_thing_model_revision": nil, "native_binding_revision": 0}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = prepareDurableDownlink(context.Background(), db, message, device.TenantID, device.ID, device.ID, "1", data)
	if err != nil || calls != before {
		t.Fatalf("unbound device required model owner: %v", err)
	}
	var count int64
	db.Model(&model.CommandSetLog{}).Count(&count)
	if count != 0 {
		t.Fatal("preparation wrote a command log")
	}
}

func TestExpectedCommandRetainsExactNumberBeforeValidation(t *testing.T) {
	params := `{"value":9007199254740993}`
	wire, err := mergeIdentifyAndPayload("set", &params)
	if err != nil || wire != `{"method":"set","params":{"value":9007199254740993}}` {
		t.Fatalf("command number changed: %s %v", wire, err)
	}
	params = `{} {}`
	if _, err := mergeIdentifyAndPayload("set", &params); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
