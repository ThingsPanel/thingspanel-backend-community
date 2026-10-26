package service

// The durable planner reuses community scene definitions and comparison helpers.
// It performs SQL-only planning; all MQTT goes through broker_command_requests.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/internal/brokercommands"
	bc "project/internal/brokercontract"
	"project/internal/downlink"
	"project/internal/model"
)

type automationEffect struct {
	ID, ReceiptID, TenantID, DeviceID, State string
	Payload                                  []byte
	CreatedAt                                time.Time
	Attempts                                 int
}

func (automationEffect) TableName() string { return "broker_ingress_effects" }

type automationPolicy struct {
	Scene      model.SceneAutomation          `json:"scene"`
	Conditions []model.DeviceTriggerCondition `json:"conditions"`
	Actions    []model.SceneActionInfo        `json:"actions"`
}

func readAutomationPolicy(ctx context.Context, tx *gorm.DB, tenant, scene string) (automationPolicy, string, error) {
	p := automationPolicy{}
	lock := func() *gorm.DB { return tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}) }
	if e := lock().Where("id=? AND tenant_id=?", scene, tenant).Take(&p.Scene).Error; e != nil {
		return p, "", e
	}
	if p.Scene.Enabled != "Y" {
		return p, "", brokercommands.ErrFenced
	}
	if e := lock().Where("scene_automation_id=? AND tenant_id=?", scene, tenant).Order("id").Limit(1025).Find(&p.Conditions).Error; e != nil {
		return p, "", e
	}
	if e := lock().Where("scene_id=? AND tenant_id=?", scene, tenant).Order("id").Limit(1025).Find(&p.Actions).Error; e != nil {
		return p, "", e
	}
	if len(p.Conditions) > 1024 || len(p.Actions) > 1024 {
		return p, "", errors.New("automation policy exceeds bounded planner capacity")
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return p, "", e
	}
	if len(raw) > 2<<20 {
		return p, "", errors.New("automation policy snapshot exceeds size limit")
	}
	h := sha256.Sum256(raw)
	return p, fmt.Sprintf("%x", h[:]), nil
}

type automationTrigger struct {
	Type   string                 `json:"trigger_param_type"`
	Values map[string]interface{} `json:"trigger_values"`
	Cell   string                 `json:"broker_cell"`
	Epoch  uint64                 `json:"route_epoch"`
}

// ProcessBrokerAutomationEffect plans at most one durable trigger in this cell.
// Errors roll back every command/action. Retries are bounded and never publish.
func ProcessBrokerAutomationEffect(ctx context.Context, db *gorm.DB, cell string) (bool, error) {
	if ctx == nil || db == nil || cell == "" {
		return false, errors.New("automation planner dependencies unavailable")
	}
	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var candidate automationEffect
	e := db.WithContext(ctx).Table("broker_ingress_effects AS e").Select("e.*").Joins("JOIN broker_ingress_receipts r ON r.id=e.receipt_id").Where("e.state='pending' AND e.effect_kind='automation' AND e.retry_after<=now() AND r.broker_cell=?", cell).Order("e.updated_at,e.id").Take(&candidate).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	e = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Manual Submit takes this same lock before its binding. Take it first
		// here too, before the source route, to avoid a planner/manual lock cycle.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "brokercommands:"+candidate.TenantID).Error; err != nil {
			return err
		}
		// Lock the authenticated transport's route before the effect, matching ingress.
		var source struct {
			DeviceID   string
			RouteEpoch uint64
		}
		if e := tx.Table("broker_ingress_receipts").Select("device_id,route_epoch").Where("id=? AND tenant_id=? AND broker_cell=?", candidate.ReceiptID, candidate.TenantID, cell).Take(&source).Error; e != nil {
			return e
		}
		var binding struct {
			Phase      string
			RouteEpoch uint64
			BrokerCell string
		}
		if e := tx.Table("broker_transport_bindings").Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id=? AND tenant_id=?", source.DeviceID, candidate.TenantID).Take(&binding).Error; e != nil {
			return e
		}
		var effect automationEffect
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("id=? AND state='pending' AND retry_after<=now()", candidate.ID).Take(&effect)
		if errors.Is(q.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if q.Error != nil {
			return q.Error
		}
		terminal := func(state, detail string) error {
			return tx.Model(&automationEffect{}).Where("id=?", effect.ID).Updates(map[string]interface{}{"state": state, "last_error": detail, "updated_at": time.Now()}).Error
		}
		if binding.Phase != "active" || binding.BrokerCell != cell || binding.RouteEpoch != source.RouteEpoch {
			return terminal("fenced", "source transport route changed")
		}
		// Old triggers must never create fresh commands after a prolonged outage.
		if time.Since(effect.CreatedAt) > 2*time.Minute || effect.CreatedAt.After(time.Now().Add(5*time.Second)) {
			return terminal("fenced", "automation trigger is outside its execution window")
		}
		var trigger automationTrigger
		if e := json.Unmarshal(effect.Payload, &trigger); e != nil {
			return terminal("failed", "invalid persisted automation trigger")
		}
		if trigger.Cell != cell || trigger.Epoch != source.RouteEpoch {
			return terminal("fenced", "persisted trigger route does not match its receipt")
		}
		var device model.Device
		if e := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=? AND is_enabled='enabled'", effect.DeviceID, effect.TenantID).Take(&device).Error; e != nil {
			return e
		}
		// A child may have moved since decoding; its old gateway cannot trigger
		// automation for the child's new ownership path.
		current := device
		for depth := 0; current.ID != source.DeviceID; depth++ {
			if depth >= 7 || current.ParentID == nil || *current.ParentID == "" {
				return terminal("fenced", "logical source no longer belongs to the authenticated transport")
			}
			var parent model.Device
			if e := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=? AND is_enabled='enabled'", *current.ParentID, effect.TenantID).Take(&parent).Error; e != nil {
				return e
			}
			current = parent
		}
		var ids []string
		config := ""
		if device.DeviceConfigID != nil {
			config = *device.DeviceConfigID
		}
		q = tx.Model(&model.DeviceTriggerCondition{}).Distinct("scene_automation_id").Where("tenant_id=? AND enabled='Y' AND ((trigger_condition_type=? AND trigger_source=?) OR (trigger_condition_type=? AND trigger_source=?))", effect.TenantID, model.DEVICE_TRIGGER_CONDITION_TYPE_ONE, device.ID, model.DEVICE_TRIGGER_CONDITION_TYPE_MULTIPLE, config).Order("scene_automation_id").Limit(257).Pluck("scene_automation_id", &ids)
		if q.Error != nil {
			return q.Error
		}
		if len(ids) > 256 {
			return errors.New("automation candidate scene capacity exceeded")
		}
		plannedActions := 0
		for _, id := range ids {
			policy, hash, e := readAutomationPolicy(ctx, tx, effect.TenantID, id)
			if errors.Is(e, brokercommands.ErrFenced) || errors.Is(e, gorm.ErrRecordNotFound) {
				continue
			}
			if e != nil {
				return e
			}
			raw, err := json.Marshal(policy)
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO broker_automation_policy_snapshots(tenant_id,scene_id,policy_sha256,snapshot) VALUES(?,?,?,?::jsonb) ON CONFLICT DO NOTHING`, effect.TenantID, id, hash, string(raw)).Error; err != nil {
				return err
			}
			window := tx.Exec(`INSERT INTO broker_automation_execution_windows(tenant_id,scene_id,device_id,effect_id,last_evaluated_at,tokens) VALUES(?,?,?,?,now(),9) ON CONFLICT(tenant_id,scene_id,device_id) DO UPDATE SET effect_id=excluded.effect_id,last_evaluated_at=now(),tokens=least(10,broker_automation_execution_windows.tokens+greatest(0,extract(epoch FROM (now()-broker_automation_execution_windows.last_evaluated_at)))/3)-1 WHERE least(10,broker_automation_execution_windows.tokens+greatest(0,extract(epoch FROM (now()-broker_automation_execution_windows.last_evaluated_at)))/3)>=1`, effect.TenantID, id, effect.DeviceID, effect.ID)
			if window.Error != nil {
				return window.Error
			}
			if window.RowsAffected == 0 {
				if e := recordAutomationAction(tx, effect, id, "@policy", "", hash, "skipped", "scene/device rate limit (one token per 3s; burst 10)", ""); e != nil {
					return e
				}
				continue
			}
			matches, supported := durablePolicyMatches(policy, device, trigger)
			if !supported {
				if e := recordAutomationAction(tx, effect, id, "@policy", "", hash, "unsupported", "condition requires an unsupported or missing evidence source", ""); e != nil {
					return e
				}
				continue
			}
			if !matches {
				if e := recordAutomationAction(tx, effect, id, "@policy", "", hash, "skipped", "conditions did not match", ""); e != nil {
					return e
				}
				continue
			}
			for _, action := range policy.Actions {
				targets, e := automationTargets(tx, effect.TenantID, action)
				if e != nil {
					return e
				}
				if len(targets) == 0 {
					targets = []string{""}
				}
				for _, target := range targets {
					plannedActions++
					if plannedActions > 1000 {
						return errors.New("automation per-trigger action capacity exceeded")
					}
					if !automationCommandAction(action) || target == "" {
						if e := recordAutomationAction(tx, effect, id, action.ID, target, hash, "unsupported", "action requires a separately certified durable handler", ""); e != nil {
							return e
						}
						continue
					}
					proof := brokercommands.AutomationProof{EffectID: effect.ID, SceneID: id, ActionID: action.ID, PolicySHA256: hash}
					command, e := submitAutomationCommand(ctx, tx, effect, proof, target, action)
					if e != nil {
						return e
					}
					if e := recordAutomationAction(tx, effect, id, action.ID, target, hash, "accepted", "command accepted; device result is separate", command); e != nil {
						return e
					}
				}
			}
		}
		return terminal("completed", "")
	})
	if e != nil && parentCtx.Err() == nil {
		recordCtx, recordCancel := context.WithTimeout(parentCtx, 2*time.Second)
		defer recordCancel()
		retry := db.WithContext(recordCtx).Model(&automationEffect{}).Where("id=? AND state='pending'", candidate.ID).Updates(map[string]interface{}{"attempts": gorm.Expr("attempts+1"), "state": gorm.Expr("CASE WHEN attempts+1>=5 THEN 'failed' ELSE 'pending' END"), "last_error": "automation plan transaction failed; inspect correlated server diagnostics", "retry_after": time.Now().Add(time.Second), "updated_at": time.Now()})
		if retry.Error != nil {
			return true, fmt.Errorf("automation planning and retry recording failed: %w", retry.Error)
		}
	}
	return true, e
}

func recordAutomationAction(tx *gorm.DB, e automationEffect, scene, action, target, hash, state, detail, command string) error {
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(e.ID+"/"+scene+"/"+action+"/"+target)).String()
	var cmd interface{}
	if command != "" {
		cmd = command
	}
	return tx.Exec(`INSERT INTO broker_automation_actions(id,effect_id,tenant_id,scene_id,action_id,target_device_id,policy_sha256,state,detail,command_id) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, e.ID, e.TenantID, scene, action, target, hash, state, detail, cmd).Error
}
func automationCommandAction(a model.SceneActionInfo) bool {
	if a.ActionParamType == nil || a.ActionValue == nil {
		return false
	}
	switch *a.ActionParamType {
	case AUTOMATE_ACTION_PARAM_TYPE_CMD, AUTOMATE_ACTION_PARAM_TYPE_COMMAND, AUTOMATE_ACTION_PARAM_TYPE_C_COMMAND:
		return a.ActionType == model.AUTOMATE_ACTION_TYPE_ONE || a.ActionType == model.AUTOMATE_ACTION_TYPE_MULTIPLE
	}
	return false
}
func automationTargets(tx *gorm.DB, tenant string, a model.SceneActionInfo) ([]string, error) {
	switch a.ActionType {
	case model.AUTOMATE_ACTION_TYPE_ONE:
		return []string{a.ActionTarget}, nil
	case model.AUTOMATE_ACTION_TYPE_MULTIPLE:
		var ids []string
		e := tx.Model(&model.Device{}).Where("tenant_id=? AND device_config_id=? AND is_enabled='enabled'", tenant, a.ActionTarget).Order("id").Limit(1001).Pluck("id", &ids).Error
		if len(ids) > 1000 {
			return nil, errors.New("automation action target capacity exceeded")
		}
		return ids, e
	default:
		return nil, nil
	}
}

// Existing event-field and scalar comparison functions are reused without the
// legacy condition decorators (which may emit alarms while checking a rule).
func durablePolicyMatches(p automationPolicy, d model.Device, t automationTrigger) (bool, bool) {
	groups := map[string][]model.DeviceTriggerCondition{}
	for _, c := range p.Conditions {
		if c.Enabled == "Y" {
			groups[c.GroupID] = append(groups[c.GroupID], c)
		}
	}
	a := &Automate{}
	anyMatch, allSupported := false, true
	for _, group := range groups {
		match, triggered, supported := true, false, true
		for _, c := range group {
			if c.TriggerConditionType == model.DEVICE_TRIGGER_CONDITION_TYPE_TIME {
				match = match && a.automateConditionCheckWithTime(c)
				continue
			}
			source := c.TriggerSource != nil && ((c.TriggerConditionType == model.DEVICE_TRIGGER_CONDITION_TYPE_ONE && *c.TriggerSource == d.ID) || (c.TriggerConditionType == model.DEVICE_TRIGGER_CONDITION_TYPE_MULTIPLE && d.DeviceConfigID != nil && *c.TriggerSource == *d.DeviceConfigID))
			if !source || c.TriggerParamType == nil || c.TriggerParam == nil {
				supported = false
				continue
			}
			if normalizedTriggerType(*c.TriggerParamType) != normalizedTriggerType(t.Type) {
				supported = false
				continue
			}
			actual, found := t.Values[*c.TriggerParam]
			if t.Type == model.TRIGGER_PARAM_TYPE_STATUS {
				actual, found = t.Values["login"]
			}
			if !found {
				supported = false
				continue
			}
			triggered = true
			operator := "="
			if c.TriggerOperator != nil {
				operator = *c.TriggerOperator
			}
			ok := false
			if t.Type == model.TRIGGER_PARAM_TYPE_STATUS {
				ok = strings.EqualFold(*c.TriggerParam, "ALL") || a.automateConditionCheckByOperator("=", *c.TriggerParam, actual)
			} else if t.Type == model.TRIGGER_PARAM_TYPE_EVT {
				var handled bool
				ok, _, handled = a.automateEventParamConditionCheck(c.TriggerValue, actual)
				if !handled {
					ok = a.automateConditionCheckByOperator("=", c.TriggerValue, actual)
				}
			} else {
				ok = a.automateConditionCheckByOperator(operator, c.TriggerValue, actual)
			}
			match = match && ok
		}
		if supported && triggered && match {
			anyMatch = true
		}
		allSupported = allSupported && supported
	}
	// A proven matching OR group is sufficient even when another group is unknown.
	return anyMatch, anyMatch || allSupported
}

func submitAutomationCommand(ctx context.Context, tx *gorm.DB, e automationEffect, proof brokercommands.AutomationProof, target string, a model.SceneActionInfo) (string, error) {
	if err := bc.ValidateUniqueJSON([]byte(*a.ActionValue)); err != nil {
		return "", err
	}
	var input struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal([]byte(*a.ActionValue), &input); err != nil {
		return "", err
	}
	if input.Method == "" {
		return "", errors.New("automation command method is required")
	}
	var device model.Device
	if err := tx.Where("id=? AND tenant_id=? AND is_enabled='enabled'", target, e.TenantID).Take(&device).Error; err != nil {
		return "", err
	}
	root := device
	for depth := 0; root.ParentID != nil && *root.ParentID != ""; depth++ {
		if depth >= 7 {
			return "", errors.New("automation command ancestor limit")
		}
		var parent model.Device
		if err := tx.Where("id=? AND tenant_id=? AND is_enabled='enabled'", *root.ParentID, e.TenantID).Take(&parent).Error; err != nil {
			return "", err
		}
		root = parent
	}
	kind, config := "1", ""
	if device.DeviceConfigID != nil {
		var cfg model.DeviceConfig
		if err := tx.Where("id=? AND tenant_id=?", *device.DeviceConfigID, e.TenantID).Take(&cfg).Error; err != nil {
			return "", err
		}
		kind = cfg.DeviceType
	}
	if root.DeviceConfigID != nil {
		config = *root.DeviceConfigID
	}
	// Keep service/plugin routing out of the MQTT transport handler.
	for _, d := range []model.Device{device, root} {
		if (d.ServiceAccessID != nil && strings.TrimSpace(*d.ServiceAccessID) != "") || (d.AccessWay != nil && *d.AccessWay == "B") {
			return "", brokercommands.ErrRejected
		}
	}
	params := string(input.Params)
	req := &model.PutMessageForCommand{DeviceID: target, Identify: input.Method, Value: &params}
	data, err := buildCommandData(input.Method, &params)
	if err != nil {
		return "", err
	}
	msg := &downlink.Message{DeviceID: target, DeviceNumber: root.DeviceNumber, DeviceType: kind, DeviceConfigID: config, Type: downlink.MessageTypeCommand}
	prepared, err := prepareDurableDownlink(ctx, tx, msg, e.TenantID, target, root.ID, kind, data)
	if err != nil {
		return "", err
	}
	if len(prepared.Encoded) == 0 || len(prepared.Encoded) > brokercommands.MaxCommandPayload {
		return "", brokercommands.ErrInvalid
	}
	prepared.Automation = &proof
	payload, err := json.Marshal(prepared)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalizeCommand(req, "")
	if err != nil {
		return "", err
	}
	key := e.ID + "/" + proof.ActionID + "/" + target
	digest := sha256.Sum256([]byte(key))
	key = fmt.Sprintf("%x", digest[:])
	row, _, err := brokercommands.New(tx).Submit(ctx, brokercommands.Request{TenantID: e.TenantID, CallerID: "automation:" + proof.SceneID, Intent: automationCommandIntent, IdempotencyKey: key, DeviceID: target, TargetDeviceID: root.ID, CanonicalRequest: canonical, Payload: payload, ExpiresAt: e.CreatedAt.Add(2 * time.Minute)}, func(tx *gorm.DB, row brokercommands.Row) error {
		op, identify, datum, status := "2", input.Method, string(prepared.Message.Data), "0"
		return tx.Create(&model.CommandSetLog{ID: row.ID, DeviceID: target, OperationType: &op, MessageID: &row.ID, Identify: &identify, Datum: &datum, Status: &status, CreatedAt: row.CreatedAt}).Error
	})
	return row.ID, err
}

// ValidateAutomationCommand proves that this command was minted by a committed
// internal plan and that the complete policy remains enabled and unchanged.
func ValidateAutomationCommand(ctx context.Context, tx *gorm.DB, row brokercommands.Row) error {
	var prepared brokercommands.PreparedCommand
	if err := json.Unmarshal(row.Payload, &prepared); err != nil {
		return brokercommands.ErrRejected
	}
	p := prepared.Automation
	if p == nil || row.Intent != automationCommandIntent || row.CallerID != "automation:"+p.SceneID {
		return brokercommands.ErrRejected
	}
	var count int64
	if err := tx.Table("broker_automation_actions AS a").Joins("JOIN broker_ingress_effects e ON e.id=a.effect_id").Where("a.command_id=? AND a.effect_id=? AND a.tenant_id=? AND a.scene_id=? AND a.action_id=? AND a.target_device_id=? AND a.policy_sha256=? AND a.state='accepted' AND e.state='completed' AND e.tenant_id=a.tenant_id", row.ID, p.EffectID, row.TenantID, p.SceneID, p.ActionID, row.DeviceID, p.PolicySHA256).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return brokercommands.ErrRejected
	}
	_, hash, err := readAutomationPolicy(ctx, tx, row.TenantID, p.SceneID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return brokercommands.ErrFenced
	}
	if err != nil {
		return err
	}
	if hash != p.PolicySHA256 {
		return brokercommands.ErrFenced
	}
	return nil
}

func normalizedTriggerType(s string) string {
	switch strings.ToUpper(s) {
	case "TEL", "TELEMETRY":
		return "TEL"
	case "ATTR", "ATTRIBUTES":
		return "ATTR"
	case "EVT", "EVENT":
		return "EVT"
	default:
		return strings.ToUpper(s)
	}
}
