package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/brokercommands"
	"project/internal/model"
)

func automationPG(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		b, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
		if err != nil {
			t.Skip("isolated PostgreSQL DSN unavailable")
		}
		dsn = strings.TrimSpace(string(b))
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL: %v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	schema := "broker_auto_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := `"` + schema + `"`
	if err := admin.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		t.Fatal(err)
	}
	scopedDSN := automationDSNWithSchema(dsn, schema)
	db, err := gorm.Open(postgres.Open(scopedDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	dbSQL, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	dbSQL.SetMaxOpenConns(32)
	t.Cleanup(func() {
		_ = dbSQL.Close()
		_ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE").Error
		_ = adminSQL.Close()
	})
	models := []interface{}{
		&model.Device{}, &model.SceneAutomation{}, &model.DeviceTriggerCondition{},
		&model.SceneActionInfo{}, &model.CommandSetLog{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("create community tables: %v", err)
	}
	if err := db.Exec(`CREATE TABLE broker_transport_bindings (
		tenant_id varchar(256) NOT NULL, device_id varchar(256) NOT NULL,
		broker_cell varchar(128) NOT NULL, route_epoch bigint NOT NULL,
		phase varchar(24) NOT NULL, PRIMARY KEY(tenant_id,device_id))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE broker_ingress_receipts (
		id uuid PRIMARY KEY, tenant_id varchar(36) NOT NULL, device_id varchar(36) NOT NULL,
		stream varchar(64) NOT NULL, source_event_id varchar(256), broker_cell varchar(128) NOT NULL,
		route_epoch bigint NOT NULL, payload bytea NOT NULL DEFAULT '', metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
		canonical_sha256 bytea NOT NULL DEFAULT decode(repeat('00',32),'hex'), state varchar(16) NOT NULL DEFAULT 'processed',
		attempts integer NOT NULL DEFAULT 0, retry_after timestamptz NOT NULL DEFAULT now(),
		last_error varchar(512) NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), processed_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"005_ingress_effects.sql", "006_commands.sql", "009_automation_actions.sql"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", file))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(strings.ReplaceAll(string(b), "public.", "")).Error; err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}
	return db
}

func automationDSNWithSchema(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err == nil {
			q := u.Query()
			q.Set("search_path", schema)
			u.RawQuery = q.Encode()
			return u.String()
		}
	}
	return dsn + " search_path=" + schema
}

type automationFixture struct {
	tenant, device, scene, receipt, effect string
	cell                                   string
	epoch                                  uint64
}

func seedAutomationFixture(t *testing.T, db *gorm.DB) automationFixture {
	t.Helper()
	f := automationFixture{tenant: uuid.NewString(), device: uuid.NewString(), scene: uuid.NewString(), receipt: uuid.NewString(), effect: uuid.NewString(), cell: "cell-a", epoch: 7}
	device := model.Device{ID: f.device, TenantID: f.tenant, Voucher: "{}", IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "device-number", IsOnline: 1}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES(?,?,?,?,?)", f.tenant, f.device, f.cell, f.epoch, "active").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload) VALUES(?,?,?,'event',?,?,?)", f.receipt, f.tenant, f.device, f.cell, f.epoch, []byte(`{}`)).Error; err != nil {
		t.Fatal(err)
	}
	trigger, err := json.Marshal(automationTrigger{Type: model.TRIGGER_PARAM_TYPE_TEL, Values: map[string]interface{}{"temperature": float64(24)}, Cell: f.cell, Epoch: f.epoch})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO broker_ingress_effects(id,receipt_id,tenant_id,device_id,effect_kind,payload,state) VALUES(?,?,?,?,'automation',?::jsonb,'pending')", f.effect, f.receipt, f.tenant, f.device, string(trigger)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SceneAutomation{ID: f.scene, Name: "test", Enabled: "Y", TenantID: f.tenant, Creator: "test", Updator: "test", CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	paramType, param, operator := "TEL", "temperature", ">"
	condition := model.DeviceTriggerCondition{ID: uuid.NewString(), SceneAutomationID: f.scene, Enabled: "Y", GroupID: "g1", TriggerConditionType: model.DEVICE_TRIGGER_CONDITION_TYPE_ONE, TriggerSource: &f.device, TriggerParamType: &paramType, TriggerParam: &param, TriggerOperator: &operator, TriggerValue: "20", TenantID: f.tenant}
	if err := db.Create(&condition).Error; err != nil {
		t.Fatal(err)
	}
	actionParamType, actionParam, actionValue := "CMD", "switch", `{"method":"switch","params":{"on":true}}`
	action := model.SceneActionInfo{ID: uuid.NewString(), SceneID: f.scene, ActionTarget: f.device, ActionType: model.AUTOMATE_ACTION_TYPE_ONE, ActionParamType: &actionParamType, ActionParam: &actionParam, ActionValue: &actionValue, CreatedAt: time.Now().UTC(), TenantID: f.tenant}
	if err := db.Create(&action).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPostgresAutomationEffectAtomicallyCreatesCommandAndActionReceipt(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	didWork, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell)
	if err != nil || !didWork {
		t.Fatalf("planner didWork=%v err=%v", didWork, err)
	}
	var effect automationEffect
	if err := db.Where("id=?", f.effect).Take(&effect).Error; err != nil || effect.State != "completed" {
		t.Fatalf("effect state=%q err=%v", effect.State, err)
	}
	var command brokercommands.Row
	if err := db.Where("tenant_id=? AND device_id=?", f.tenant, f.device).Take(&command).Error; err != nil {
		t.Fatalf("durable command not created: %v", err)
	}
	var logs int64
	if err := db.Model(&model.CommandSetLog{}).Where("id=? AND device_id=?", command.ID, f.device).Count(&logs).Error; err != nil || logs != 1 {
		t.Fatalf("command log count=%d err=%v", logs, err)
	}
	var actions []struct {
		State     string
		CommandID *string
	}
	if err := db.Table("broker_automation_actions").Where("effect_id=?", f.effect).Find(&actions).Error; err != nil || len(actions) != 1 || actions[0].State != "accepted" || actions[0].CommandID == nil || *actions[0].CommandID != command.ID {
		t.Fatalf("action receipts=%+v err=%v", actions, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return ValidateAutomationCommand(context.Background(), tx, command) }); err != nil {
		t.Fatalf("committed policy proof rejected: %v", err)
	}
	workedAgain, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell)
	if err != nil || workedAgain {
		t.Fatalf("completed effect repeated: didWork=%v err=%v", workedAgain, err)
	}
	var count int64
	_ = db.Model(&brokercommands.Row{}).Count(&count).Error
	if count != 1 {
		t.Fatalf("duplicate planner created %d commands", count)
	}
}

func TestPostgresConcurrentAutomationWorkersPlanEffectOnce(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = ProcessBrokerAutomationEffect(context.Background(), db, f.cell)
		}(i)
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var commands, logs, actions int64
	if err := db.Model(&brokercommands.Row{}).Count(&commands).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CommandSetLog{}).Count(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("broker_automation_actions").Count(&actions).Error; err != nil {
		t.Fatal(err)
	}
	if commands != 1 || logs != 1 || actions != 1 {
		t.Fatalf("concurrent plan duplicates: commands=%d logs=%d actions=%d", commands, logs, actions)
	}
}

func TestPostgresAutomationRejectsOldRouteAndPolicyDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *gorm.DB, automationFixture)
		want   string
	}{
		{name: "epoch-changed", want: "fenced", mutate: func(t *testing.T, db *gorm.DB, f automationFixture) {
			if err := db.Exec("UPDATE broker_transport_bindings SET route_epoch=route_epoch+1 WHERE tenant_id=? AND device_id=?", f.tenant, f.device).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "binding-freeze", want: "fenced", mutate: func(t *testing.T, db *gorm.DB, f automationFixture) {
			if err := db.Exec("UPDATE broker_transport_bindings SET phase='draining' WHERE tenant_id=? AND device_id=?", f.tenant, f.device).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "trigger-epoch-mismatch", want: "fenced", mutate: func(t *testing.T, db *gorm.DB, f automationFixture) {
			if err := db.Exec("UPDATE broker_ingress_effects SET payload=jsonb_set(payload,'{route_epoch}','99'::jsonb) WHERE id=?", f.effect).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "old-trigger", want: "fenced", mutate: func(t *testing.T, db *gorm.DB, f automationFixture) {
			if err := db.Exec("UPDATE broker_ingress_effects SET created_at=now()-interval '3 minutes' WHERE id=?", f.effect).Error; err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := automationPG(t)
			f := seedAutomationFixture(t, db)
			tc.mutate(t, db, f)
			if _, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell); err != nil {
				t.Fatal(err)
			}
			var state string
			if err := db.Raw("SELECT state FROM broker_ingress_effects WHERE id=?", f.effect).Scan(&state).Error; err != nil || state != tc.want {
				t.Fatalf("effect state=%q want=%q err=%v", state, tc.want, err)
			}
			var commands int64
			if err := db.Model(&brokercommands.Row{}).Count(&commands).Error; err != nil || commands != 0 {
				t.Fatalf("commands=%d err=%v", commands, err)
			}
		})
	}
}

func TestPostgresAutomationTransactionRollbackAndBoundedRetry(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	if err := db.Exec(`CREATE FUNCTION reject_automation_action() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced action receipt failure'; END $$; CREATE TRIGGER reject_automation_action BEFORE INSERT ON broker_automation_actions FOR EACH ROW EXECUTE FUNCTION reject_automation_action()`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell); err == nil {
		t.Fatal("expected planner transaction failure")
	}
	var commands, logs, actions int64
	_ = db.Model(&brokercommands.Row{}).Count(&commands).Error
	_ = db.Model(&model.CommandSetLog{}).Count(&logs).Error
	_ = db.Table("broker_automation_actions").Count(&actions).Error
	var state string
	var attempts int
	if err := db.Raw("SELECT state,attempts FROM broker_ingress_effects WHERE id=?", f.effect).Row().Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if commands != 0 || logs != 0 || actions != 0 || state != "pending" || attempts != 1 {
		t.Fatalf("rollback leaked writes commands=%d logs=%d actions=%d state=%s attempts=%d", commands, logs, actions, state, attempts)
	}
	if err := db.Exec(`DROP TRIGGER reject_automation_action ON broker_automation_actions; DROP FUNCTION reject_automation_action()`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE broker_ingress_effects SET retry_after=now()-interval '1 second' WHERE id=?", f.effect).Error; err != nil {
		t.Fatal(err)
	}
	if worked, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell); err != nil || !worked {
		t.Fatalf("retry didWork=%v err=%v", worked, err)
	}
	_ = db.Model(&brokercommands.Row{}).Count(&commands).Error
	_ = db.Model(&model.CommandSetLog{}).Count(&logs).Error
	_ = db.Table("broker_automation_actions").Count(&actions).Error
	if commands != 1 || logs != 1 || actions != 1 {
		t.Fatalf("retry did not commit exactly once: commands=%d logs=%d actions=%d", commands, logs, actions)
	}
}

func TestPostgresAutomationFailureRetryStopsAtFiveAttempts(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	if err := db.Exec(`CREATE FUNCTION reject_automation_action() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced action receipt failure'; END $$; CREATE TRIGGER reject_automation_action BEFORE INSERT ON broker_automation_actions FOR EACH ROW EXECUTE FUNCTION reject_automation_action()`).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if i > 0 {
			if err := db.Exec("UPDATE broker_ingress_effects SET retry_after=now()-interval '1 second' WHERE id=?", f.effect).Error; err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell); err == nil {
			t.Fatalf("attempt %d unexpectedly succeeded", i+1)
		}
	}
	var state string
	var attempts int
	if err := db.Raw("SELECT state,attempts FROM broker_ingress_effects WHERE id=?", f.effect).Row().Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || attempts != 5 {
		t.Fatalf("retry bound state=%s attempts=%d, want failed/5", state, attempts)
	}
	var commands, logs, actions int64
	_ = db.Model(&brokercommands.Row{}).Count(&commands).Error
	_ = db.Model(&model.CommandSetLog{}).Count(&logs).Error
	_ = db.Table("broker_automation_actions").Count(&actions).Error
	if commands != 0 || logs != 0 || actions != 0 {
		t.Fatalf("failed plan leaked partial writes commands=%d logs=%d actions=%d", commands, logs, actions)
	}
}

func TestPostgresAutomationRejectsCrossTenantForgedTriggerSource(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	other := uuid.NewString()
	if err := db.Create(&model.SceneAutomation{ID: other, Name: "foreign", Enabled: "Y", TenantID: uuid.NewString(), Creator: "x", Updator: "x", CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	paramType, param, operator := "TEL", "temperature", ">"
	foreign := model.DeviceTriggerCondition{ID: uuid.NewString(), SceneAutomationID: other, Enabled: "Y", GroupID: "foreign", TriggerConditionType: model.DEVICE_TRIGGER_CONDITION_TYPE_ONE, TriggerSource: &f.device, TriggerParamType: &paramType, TriggerParam: &param, TriggerOperator: &operator, TriggerValue: "0", TenantID: uuid.NewString()}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell); err != nil {
		t.Fatal(err)
	}
	var commands int64
	if err := db.Model(&brokercommands.Row{}).Count(&commands).Error; err != nil || commands != 1 {
		t.Fatalf("tenant-local command count=%d err=%v", commands, err)
	}
}

func TestPostgresAutomationEventAndAttributeConditionsCreateCommands(t *testing.T) {
	for _, tc := range []struct {
		name, kind, param, value, condition string
	}{
		{name: "attribute", kind: model.TRIGGER_PARAM_TYPE_ATTR, param: "switch", value: `true`, condition: `true`},
		{name: "event", kind: model.TRIGGER_PARAM_TYPE_EVT, param: "alarm", value: `{"state":"on"}`, condition: `{"match_mode":"field","conditions":[{"field":"state","operator":"=","value":"on"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := automationPG(t)
			f := seedAutomationFixture(t, db)
			var actual interface{}
			if err := json.Unmarshal([]byte(tc.value), &actual); err != nil {
				t.Fatal(err)
			}
			trigger, err := json.Marshal(automationTrigger{Type: tc.kind, Values: map[string]interface{}{tc.param: actual}, Cell: f.cell, Epoch: f.epoch})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("UPDATE broker_ingress_effects SET payload=?::jsonb WHERE id=?", string(trigger), f.effect).Error; err != nil {
				t.Fatal(err)
			}
			operator := "="
			if tc.kind == model.TRIGGER_PARAM_TYPE_TEL {
				operator = ">"
			}
			if err := db.Exec("UPDATE device_trigger_condition SET trigger_param_type=?,trigger_param=?,trigger_operator=?,trigger_value=? WHERE scene_automation_id=?", tc.kind, tc.param, operator, tc.condition, f.scene).Error; err != nil {
				t.Fatal(err)
			}
			if worked, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell); err != nil || !worked {
				t.Fatalf("planner didWork=%v err=%v", worked, err)
			}
			var commands int64
			if err := db.Model(&brokercommands.Row{}).Count(&commands).Error; err != nil || commands != 1 {
				t.Fatalf("matching %s condition commands=%d err=%v", tc.name, commands, err)
			}
		})
	}
}

func TestPostgresAutomationUnsupportedActionAuditedWithoutCommand(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	if err := db.Model(&model.SceneActionInfo{}).Where("scene_id=?", f.scene).Update("action_type", model.AUTOMATE_ACTION_TYPE_SCENE).Error; err != nil {
		t.Fatal(err)
	}
	if worked, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell); err != nil || !worked {
		t.Fatalf("planner didWork=%v err=%v", worked, err)
	}
	var state string
	if err := db.Raw("SELECT state FROM broker_automation_actions WHERE effect_id=?", f.effect).Scan(&state).Error; err != nil || state != "unsupported" {
		t.Fatalf("unsupported action audit=%q err=%v", state, err)
	}
	var commands int64
	if err := db.Model(&brokercommands.Row{}).Count(&commands).Error; err != nil || commands != 0 {
		t.Fatalf("unsupported action created commands=%d err=%v", commands, err)
	}
}

func TestPostgresAutomationPolicyProofRejectsTenantAndPolicyDrift(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	if _, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell); err != nil {
		t.Fatal(err)
	}
	var row brokercommands.Row
	if err := db.Where("tenant_id=? AND device_id=?", f.tenant, f.device).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		foreign := row
		foreign.TenantID = uuid.NewString()
		if err := ValidateAutomationCommand(context.Background(), tx, foreign); err != brokercommands.ErrRejected {
			return fmt.Errorf("foreign tenant proof error=%v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name string }{
		{name: "policy-modified"},
		{name: "policy-disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Isolate each mutation and its policy proof validation.
			db2 := automationPG(t)
			f2 := seedAutomationFixture(t, db2)
			if _, err := ProcessBrokerAutomationEffect(context.Background(), db2, f2.cell); err != nil {
				t.Fatal(err)
			}
			var command brokercommands.Row
			if err := db2.Where("tenant_id=? AND device_id=?", f2.tenant, f2.device).Take(&command).Error; err != nil {
				t.Fatal(err)
			}
			var err error
			if tc.name == "policy-modified" {
				err = db2.Model(&model.DeviceTriggerCondition{}).Where("scene_automation_id=?", f2.scene).Update("trigger_value", "25").Error
			} else {
				err = db2.Model(&model.SceneAutomation{}).Where("id=? AND tenant_id=?", f2.scene, f2.tenant).Update("enabled", "N").Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := db2.Transaction(func(tx *gorm.DB) error { return ValidateAutomationCommand(context.Background(), tx, command) }); err != brokercommands.ErrFenced {
				t.Fatalf("stale policy proof error=%v", err)
			}
		})
	}
}
