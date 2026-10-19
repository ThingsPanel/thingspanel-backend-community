package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"project/internal/brokercommands"
	bc "project/internal/brokercontract"
	"project/internal/downlink"
	"project/internal/model"
	"project/internal/processor"
	"project/pkg/constant"
	"project/pkg/global"

	"github.com/spf13/viper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	commandInputIngressV1   = "ingress_v1"
	automationCallerID      = "system:automation"
	manualCommandIntent     = "device.command"
	automationCommandIntent = "automation.command"
	defaultCommandTTL       = 30 * time.Second
)

var errDurableCommandSchema = errors.New("durable command schema is not installed")

type canonicalCommandRequest struct {
	DeviceID  string `json:"device_id"`
	Identify  string `json:"identify"`
	Params    any    `json:"params,omitempty"`
	HasParams bool   `json:"has_params"`
	ExpiresAt *int64 `json:"expires_at,omitempty"`
}

func isIngressV1Commands() bool {
	return viper.GetString("mqtt.input_profile") == commandInputIngressV1
}

func getDurableCommandStore(db *gorm.DB) (*brokercommands.Store, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: database is unavailable", errDurableCommandSchema)
	}
	requiredColumns := map[string][]string{
		"broker_command_requests":   {"id", "tenant_id", "caller_id", "intent", "idempotency_key", "device_id", "target_device_id", "route_cell", "route_epoch", "canonical_sha256", "payload", "expires_at", "state"},
		"broker_transport_bindings": {"tenant_id", "device_id", "broker_cell", "route_epoch", "phase"},
		"devices":                   {"id", "tenant_id", "is_enabled", "parent_id"},
		"command_set_logs":          {"id", "device_id", "operation_type", "message_id", "data", "status", "created_at"},
		"device_configs":            {"id", "tenant_id", "device_type", "device_template_id", "protocol_type"},
		"device_model_commands":     {"id", "device_template_id", "data_identifier", "tenant_id"},
		"data_scripts":              {"id", "device_config_id", "script_type", "enable_flag", "content"},
		"device_topic_mappings":     {"id", "device_config_id", "enabled"},
	}
	for table, columns := range requiredColumns {
		if !db.Migrator().HasTable(table) {
			return nil, fmt.Errorf("%w: missing table %s", errDurableCommandSchema, table)
		}
		for _, column := range columns {
			if !db.Migrator().HasColumn(table, column) {
				return nil, fmt.Errorf("%w: missing column %s.%s", errDurableCommandSchema, table, column)
			}
		}
	}
	return brokercommands.New(db), nil
}

func durableCommandIdentity(operationType, operatorID string) (callerID, intent string, err error) {
	switch operationType {
	case strconv.Itoa(constant.Manual):
		if operatorID == "" {
			return "", "", errors.New("manual command requires an authenticated caller ID")
		}
		return operatorID, manualCommandIntent, nil
	case strconv.Itoa(constant.Auto):
		return "", "", errors.New("automated commands are disabled for ingress_v1 until an auditable automation policy context is available")
	default:
		return "", "", fmt.Errorf("unsupported command operation type %q", operationType)
	}
}

// ValidateDurableCommandCaller is intended for the dispatcher Claim guard. It
// rechecks the user, tenant, and the same configured Casbin route as the HTTP
// endpoint; machine-originated commands remain fail-closed until they carry a
// separately reviewable policy identity and revision.
func ValidateDurableCommandCaller(ctx context.Context, tx *gorm.DB, row brokercommands.Row) error {
	if ctx == nil || tx == nil || row.TenantID == "" || row.CallerID == "" {
		return errors.New("durable command caller context is incomplete")
	}
	if row.Intent == automationCommandIntent {
		return ValidateAutomationCommand(ctx, tx, row)
	}
	if (row.Intent != manualCommandIntent && row.Intent != plannedCommandIntent) || row.CallerID == automationCallerID {
		return fmt.Errorf("%w: caller has no supported auditable authorization policy", brokercommands.ErrRejected)
	}
	var user model.User
	if err := tx.WithContext(ctx).Where("id = ?", row.CallerID).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: command caller no longer exists", brokercommands.ErrRejected)
		}
		return fmt.Errorf("durable command caller is unavailable: %w", err)
	}
	if user.Status == nil || *user.Status != "N" || user.TenantID == nil || *user.TenantID != row.TenantID {
		return fmt.Errorf("%w: caller is disabled or outside the command tenant", brokercommands.ErrRejected)
	}
	if global.CasbinEnforcer == nil {
		return errors.New("durable command authorization is unavailable")
	}
	const route = "api/v1/command/datas/pub"
	configured := global.CasbinEnforcer.GetFilteredNamedGroupingPolicy("g2", 0, route)
	if len(configured) == 0 {
		if row.Intent == plannedCommandIntent {
			return validatePlannedCommandFinal(ctx, tx, row)
		}
		return nil // Mirrors middleware's unconfigured-route behavior.
	}
	allowed, err := global.CasbinEnforcer.Enforce(row.CallerID, route, "allow")
	if err != nil {
		return fmt.Errorf("evaluate command route authorization: %w", err)
	}
	if !allowed {
		return fmt.Errorf("%w: caller is not authorized for the command route", brokercommands.ErrRejected)
	}
	if row.Intent == plannedCommandIntent {
		return validatePlannedCommandFinal(ctx, tx, row)
	}
	return nil
}

func durableCommandExpiry(req *model.PutMessageForCommand, now time.Time) time.Time {
	if req.ExpiresAt != nil {
		return time.UnixMilli(*req.ExpiresAt)
	}
	return now.Add(defaultCommandTTL).UTC()
}

func canonicalizeCommand(req *model.PutMessageForCommand, _ string) ([]byte, error) {
	// Route/ancestor/configuration snapshots are server-derived and must not
	// turn an unchanged client retry into another logical request.
	canonical := canonicalCommandRequest{DeviceID: req.DeviceID, Identify: req.Identify, ExpiresAt: req.ExpiresAt}
	if req.Value != nil && strings.TrimSpace(*req.Value) != "" {
		canonical.HasParams = true
		if err := bc.ValidateUniqueJSON([]byte(*req.Value)); err != nil {
			return nil, err
		}
		var params any
		decoder := json.NewDecoder(strings.NewReader(*req.Value))
		decoder.UseNumber()
		if err := decoder.Decode(&params); err != nil {
			return nil, err
		}
		if decoder.Decode(new(any)) != io.EOF {
			return nil, errors.New("trailing command parameters")
		}
		canonical.Params = params
	}
	return json.Marshal(canonical)
}

type commandDeviceSnapshot struct {
	ID              string  `json:"id"`
	TenantID        string  `json:"tenant_id"`
	IsEnabled       string  `json:"is_enabled"`
	ActivateFlag    string  `json:"activate_flag"`
	ParentID        *string `json:"parent_id,omitempty"`
	DeviceConfigID  *string `json:"device_config_id,omitempty"`
	DeviceNumber    string  `json:"device_number"`
	SubDeviceAddr   *string `json:"sub_device_addr,omitempty"`
	Protocol        *string `json:"protocol,omitempty"`
	AccessWay       *string `json:"access_way,omitempty"`
	ServiceAccessID *string `json:"service_access_id,omitempty"`
	DeviceType      string  `json:"device_type"`
}

type commandConfigSnapshot struct {
	ID               string  `json:"id"`
	TenantID         string  `json:"tenant_id"`
	DeviceType       string  `json:"device_type"`
	DeviceTemplateID *string `json:"device_template_id,omitempty"`
	ProtocolType     *string `json:"protocol_type,omitempty"`
	ProtocolConfig   *string `json:"protocol_config,omitempty"`
	AdditionalInfo   *string `json:"additional_info,omitempty"`
	OtherConfig      *string `json:"other_config,omitempty"`
}

type commandDefinitionSnapshot struct {
	ID               string  `json:"id"`
	DeviceTemplateID string  `json:"device_template_id"`
	DataIdentifier   string  `json:"data_identifier"`
	DataName         *string `json:"data_name,omitempty"`
	Params           *string `json:"params,omitempty"`
	AdditionalInfo   *string `json:"additional_info,omitempty"`
	TenantID         string  `json:"tenant_id"`
}

type commandScriptSnapshot struct {
	ID             string  `json:"id"`
	DeviceConfigID string  `json:"device_config_id"`
	ScriptType     string  `json:"script_type"`
	EnableFlag     string  `json:"enable_flag"`
	Content        *string `json:"content,omitempty"`
}

type commandFingerprintSnapshot struct {
	Devices  []commandDeviceSnapshot     `json:"devices"`
	Configs  []commandConfigSnapshot     `json:"configs"`
	Commands []commandDefinitionSnapshot `json:"commands"`
	Scripts  []commandScriptSnapshot     `json:"scripts"`
}

func readCommandFingerprint(ctx context.Context, tx *gorm.DB, tenant, logicalDeviceID, targetRootID, expectedConfigID string) (string, *model.DataScript, commandFingerprintSnapshot, error) {
	if tx == nil || ctx == nil || tenant == "" || logicalDeviceID == "" || targetRootID == "" {
		return "", nil, commandFingerprintSnapshot{}, errors.New("command fingerprint input is incomplete")
	}
	snapshot := commandFingerprintSnapshot{}
	configsByID := make(map[string]model.DeviceConfig)
	currentID := logicalDeviceID
	visited := make(map[string]struct{}, 8)
	for depth := 0; depth < 8; depth++ {
		if _, exists := visited[currentID]; exists {
			return "", nil, commandFingerprintSnapshot{}, fmt.Errorf("%w: device parent cycle", brokercommands.ErrRejected)
		}
		visited[currentID] = struct{}{}
		var device model.Device
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND tenant_id = ?", currentID, tenant).Take(&device).Error; err != nil {
			return "", nil, commandFingerprintSnapshot{}, err
		}
		if device.IsEnabled != "enabled" || device.ActivateFlag != "active" {
			return "", nil, commandFingerprintSnapshot{}, fmt.Errorf("%w: device in command path is disabled", brokercommands.ErrRejected)
		}
		deviceType := "1"
		snapshot.Devices = append(snapshot.Devices, commandDeviceSnapshot{
			ID: device.ID, TenantID: device.TenantID, IsEnabled: device.IsEnabled, ActivateFlag: device.ActivateFlag, ParentID: device.ParentID,
			DeviceConfigID: device.DeviceConfigID, DeviceNumber: device.DeviceNumber, SubDeviceAddr: device.SubDeviceAddr,
			Protocol: device.Protocol, AccessWay: device.AccessWay, ServiceAccessID: device.ServiceAccessID,
		})
		if device.DeviceConfigID != nil && *device.DeviceConfigID != "" {
			configID := *device.DeviceConfigID
			if _, ok := configsByID[configID]; !ok {
				var config model.DeviceConfig
				if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND tenant_id = ?", configID, tenant).Take(&config).Error; err != nil {
					return "", nil, commandFingerprintSnapshot{}, err
				}
				configsByID[configID] = config
			}
			deviceType = configsByID[configID].DeviceType
		}
		snapshot.Devices[len(snapshot.Devices)-1].DeviceType = deviceType
		if currentID == targetRootID {
			if device.ParentID != nil && *device.ParentID != "" {
				return "", nil, commandFingerprintSnapshot{}, fmt.Errorf("%w: command target is not a root device", brokercommands.ErrRejected)
			}
			if (device.DeviceConfigID != nil && *device.DeviceConfigID != expectedConfigID) || ((device.DeviceConfigID == nil || *device.DeviceConfigID == "") && expectedConfigID != "") {
				return "", nil, commandFingerprintSnapshot{}, fmt.Errorf("%w: target configuration changed while preparing command", brokercommands.ErrFenced)
			}
			break
		}
		if device.ParentID == nil || *device.ParentID == "" {
			return "", nil, commandFingerprintSnapshot{}, fmt.Errorf("%w: command target is not an ancestor", brokercommands.ErrRejected)
		}
		currentID = *device.ParentID
	}
	if len(snapshot.Devices) == 0 || snapshot.Devices[len(snapshot.Devices)-1].ID != targetRootID {
		return "", nil, commandFingerprintSnapshot{}, fmt.Errorf("%w: command target ancestry exceeds eight levels", brokercommands.ErrRejected)
	}
	configIDs := make([]string, 0, len(configsByID))
	for id := range configsByID {
		configIDs = append(configIDs, id)
	}
	sort.Strings(configIDs)
	templateIDs := make([]string, 0, len(configIDs))
	for _, id := range configIDs {
		config := configsByID[id]
		var mapping model.DeviceTopicMapping
		err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("device_config_id=? AND enabled=true", config.ID).Take(&mapping).Error
		if err == nil {
			return "", nil, commandFingerprintSnapshot{}, fmt.Errorf("%w: custom command mapping has no frozen durable proof", brokercommands.ErrRejected)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil, commandFingerprintSnapshot{}, err
		}
		snapshot.Configs = append(snapshot.Configs, commandConfigSnapshot{
			ID: config.ID, TenantID: config.TenantID, DeviceType: config.DeviceType, DeviceTemplateID: config.DeviceTemplateID,
			ProtocolType: config.ProtocolType, ProtocolConfig: config.ProtocolConfig, AdditionalInfo: config.AdditionalInfo, OtherConfig: config.OtherConfig,
		})
		if config.DeviceTemplateID != nil && *config.DeviceTemplateID != "" {
			templateIDs = append(templateIDs, *config.DeviceTemplateID)
		}
	}
	sort.Strings(templateIDs)
	for _, templateID := range templateIDs {
		var definitions []model.DeviceModelCommand
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("device_template_id = ? AND tenant_id = ?", templateID, tenant).Order("id ASC").Find(&definitions).Error; err != nil {
			return "", nil, commandFingerprintSnapshot{}, err
		}
		for _, definition := range definitions {
			snapshot.Commands = append(snapshot.Commands, commandDefinitionSnapshot{
				ID: definition.ID, DeviceTemplateID: definition.DeviceTemplateID, DataIdentifier: definition.DataIdentifier,
				DataName: definition.DataName, Params: definition.Param, AdditionalInfo: definition.AdditionalInfo, TenantID: definition.TenantID,
			})
		}
	}
	rootDevice := snapshot.Devices[len(snapshot.Devices)-1]
	var scripts []model.DataScript
	if rootDevice.DeviceConfigID != nil && *rootDevice.DeviceConfigID != "" {
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("device_config_id = ? AND script_type = ?", *rootDevice.DeviceConfigID, processor.ScriptTypeCommand).Order("id ASC").Find(&scripts).Error; err != nil {
			return "", nil, commandFingerprintSnapshot{}, err
		}
	}
	var activeScript *model.DataScript
	for i := range scripts {
		script := &scripts[i]
		snapshot.Scripts = append(snapshot.Scripts, commandScriptSnapshot{ID: script.ID, DeviceConfigID: script.DeviceConfigID, ScriptType: script.ScriptType, EnableFlag: script.EnableFlag, Content: script.Content})
		if script.EnableFlag != processor.EnableFlagEnabled {
			continue
		}
		if activeScript != nil {
			return "", nil, commandFingerprintSnapshot{}, fmt.Errorf("%w: multiple enabled command scripts for target configuration", brokercommands.ErrRejected)
		}
		activeScript = script
	}
	if len(scripts) > 0 && activeScript == nil {
		return "", nil, commandFingerprintSnapshot{}, fmt.Errorf("%w: command script is configured but disabled", brokercommands.ErrRejected)
	}
	encodedSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return "", nil, commandFingerprintSnapshot{}, err
	}
	digest := sha256.Sum256(encodedSnapshot)
	return fmt.Sprintf("%x", digest[:]), activeScript, snapshot, nil
}

func prepareDurableDownlink(ctx context.Context, db *gorm.DB, message *downlink.Message, tenant, logicalDeviceID, targetRootID, deviceType string, commandData map[string]interface{}) (brokercommands.PreparedCommand, error) {
	prepared := brokercommands.PreparedCommand{Message: *message}
	if message.TopicPrefix != "" {
		return prepared, errors.New("protocol-plugin topic prefixes are not supported for ingress_v1 durable commands")
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fingerprint, script, snapshot, err := readCommandFingerprint(ctx, tx, tenant, logicalDeviceID, targetRootID, message.DeviceConfigID)
		if err != nil {
			return err
		}
		if snapshot.Devices[0].DeviceType != deviceType {
			return fmt.Errorf("%w: command device configuration changed while preparing", brokercommands.ErrFenced)
		}
		for _, config := range snapshot.Configs {
			if config.ProtocolType != nil && *config.ProtocolType != "" && *config.ProtocolType != "MQTT" {
				return fmt.Errorf("%w: protocol-plugin commands are not supported for ingress_v1", brokercommands.ErrRejected)
			}
		}
		transformed, err := transformDurableCommandData(commandData, snapshot.Devices, deviceType)
		if err != nil {
			return err
		}
		data, err := json.Marshal(transformed)
		if err != nil {
			return err
		}
		prepared.Message.Data = data
		prepared.Message.DeviceNumber = snapshot.Devices[len(snapshot.Devices)-1].DeviceNumber
		prepared.Message.DeviceType = deviceType
		prepared.ConfigurationSHA256 = fingerprint
		if script == nil {
			prepared.Encoded = append([]byte(nil), prepared.Message.Data...)
			return nil
		}
		if script.Content == nil || *script.Content == "" {
			return fmt.Errorf("%w: enabled command script has no content", brokercommands.ErrRejected)
		}
		encoded, err := processor.NewLuaExecutor().ExecuteEncode(ctx, *script.Content, prepared.Message.Data)
		if err != nil {
			return fmt.Errorf("encode command using accepted script: %w", err)
		}
		prepared.Encoded = []byte(encoded)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return brokercommands.PreparedCommand{}, err
	}
	return prepared, nil
}

func transformDurableCommandData(payload map[string]interface{}, path []commandDeviceSnapshot, deviceType string) (map[string]interface{}, error) {
	if len(path) == 0 {
		return nil, errors.New("command device path is empty")
	}
	switch deviceType {
	case "3":
		logical := path[0]
		if logical.SubDeviceAddr == nil || *logical.SubDeviceAddr == "" || len(path) < 2 {
			return nil, fmt.Errorf("%w: child device address or parent is missing", brokercommands.ErrRejected)
		}
		var nested map[string]interface{} = map[string]interface{}{"sub_device_data": map[string]interface{}{*logical.SubDeviceAddr: payload}}
		for index := 1; index < len(path)-1; index++ {
			gateway := path[index]
			if gateway.SubDeviceAddr == nil || *gateway.SubDeviceAddr == "" {
				return nil, fmt.Errorf("%w: sub-gateway address is missing", brokercommands.ErrRejected)
			}
			nested = map[string]interface{}{"sub_gateway_data": map[string]interface{}{*gateway.SubDeviceAddr: nested}}
		}
		return nested, nil
	case "2":
		nested := map[string]interface{}{"gateway_data": payload}
		for index := 0; index < len(path)-1; index++ {
			gateway := path[index]
			if gateway.SubDeviceAddr == nil || *gateway.SubDeviceAddr == "" {
				return nil, fmt.Errorf("%w: sub-gateway address is missing", brokercommands.ErrRejected)
			}
			nested = map[string]interface{}{"sub_gateway_data": map[string]interface{}{*gateway.SubDeviceAddr: nested}}
		}
		return nested, nil
	default:
		if len(path) != 1 {
			return nil, fmt.Errorf("%w: direct device cannot have an MQTT parent", brokercommands.ErrRejected)
		}
		return payload, nil
	}
}

func (c *CommandData) submitDurableCommand(ctx context.Context, operatorID, tenantID string, req *model.PutMessageForCommand, operationType string, device, target *model.Device, message *downlink.Message, deviceType string, commandData map[string]interface{}, hooks ...durablePreparationHook) (string, error) {
	if device == nil || target == nil || req == nil || message == nil {
		return "", errors.New("durable command input is incomplete")
	}
	if device.IsEnabled != "enabled" || target.IsEnabled != "enabled" {
		return "", errors.New("command target device is disabled")
	}
	if tenantID == "" {
		tenantID = device.TenantID
	}
	if device.TenantID != tenantID || target.TenantID != tenantID {
		return "", errors.New("command device not found")
	}
	callerID, intent, err := durableCommandIdentity(operationType, operatorID)
	if err != nil {
		return "", err
	}
	store, err := getDurableCommandStore(global.DB)
	if err != nil {
		return "", err
	}
	prepared, err := prepareDurableDownlink(ctx, global.DB, message, tenantID, device.ID, target.ID, deviceType, commandData)
	if err != nil {
		return "", err
	}
	if len(prepared.Encoded) == 0 || len(prepared.Encoded) > brokercommands.MaxCommandPayload {
		return "", fmt.Errorf("%w: encoded command payload exceeds supported limit", brokercommands.ErrInvalid)
	}
	canonical, err := canonicalizeCommand(req, target.ID)
	if err != nil {
		return "", err
	}
	expiresAt := durableCommandExpiry(req, time.Now())
	messageBytes, err := json.Marshal(prepared)
	if err != nil {
		return "", fmt.Errorf("marshal prepared durable downlink: %w", err)
	}
	if err := ValidateDurableCommandCaller(ctx, global.DB, brokercommands.Row{
		TenantID: tenantID, CallerID: callerID, Intent: intent,
		DeviceID: device.ID, TargetDeviceID: target.ID, Payload: messageBytes,
	}); err != nil {
		return "", fmt.Errorf("authorize durable command: %w", err)
	}
	entry := durablePreparation{Request: brokercommands.Request{
		TenantID: tenantID, CallerID: callerID, Intent: intent, IdempotencyKey: req.IdempotencyKey,
		DeviceID: device.ID, TargetDeviceID: target.ID, CanonicalRequest: canonical,
		Payload: messageBytes, ExpiresAt: expiresAt,
	}, Prepared: prepared, Identify: req.Identify, OperationType: operationType}
	if len(hooks) > 0 {
		if len(hooks) != 1 || hooks[0] == nil {
			return "", brokercommands.ErrInvalid
		}
		return hooks[0](entry)
	}
	row, _, err := store.Submit(ctx, entry.Request, func(tx *gorm.DB, row brokercommands.Row) error {
		return persistPreparedCommand(ctx, tx, row, entry)
	})
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

type durablePreparation struct {
	Request                 brokercommands.Request
	Prepared                brokercommands.PreparedCommand
	Identify, OperationType string
}
type durablePreparationHook func(durablePreparation) (string, error)

func persistPreparedCommand(ctx context.Context, tx *gorm.DB, row brokercommands.Row, entry durablePreparation) error {
	// Admission of a planned command uses the same authenticated command
	// permission; its plan is linked atomically by the command owner's hook.
	authorizationRow := row
	authorizationRow.Intent = manualCommandIntent
	if err := ValidateDurableCommandCaller(ctx, tx, authorizationRow); err != nil {
		return err
	}
	fingerprint, _, _, err := readCommandFingerprint(ctx, tx, row.TenantID, row.DeviceID, row.TargetDeviceID, entry.Prepared.Message.DeviceConfigID)
	if err != nil {
		return err
	}
	if fingerprint != entry.Prepared.ConfigurationSHA256 {
		return fmt.Errorf("%w: command configuration changed before durable acceptance", brokercommands.ErrFenced)
	}
	operationType, identify, data := entry.OperationType, entry.Identify, string(entry.Prepared.Message.Data)
	return tx.Create(&model.CommandSetLog{ID: row.ID, DeviceID: row.DeviceID, OperationType: &operationType, MessageID: &row.ID, Identify: &identify, Datum: &data, Status: stringPtr("0"), CreatedAt: row.CreatedAt}).Error
}

// ValidateDurableCommandSnapshot rechecks the prepared message fingerprint at
// dispatch time. A changed device/config/template/script fences the intent.
func ValidateDurableCommandSnapshot(ctx context.Context, tx *gorm.DB, row brokercommands.Row) error {
	if ctx == nil || tx == nil {
		return errors.New("durable command snapshot context is incomplete")
	}
	var prepared brokercommands.PreparedCommand
	if err := json.Unmarshal(row.Payload, &prepared); err != nil {
		return fmt.Errorf("decode prepared command receipt: %w", err)
	}
	if prepared.ConfigurationSHA256 == "" || len(prepared.Encoded) == 0 || len(prepared.Encoded) > brokercommands.MaxCommandPayload {
		return fmt.Errorf("%w: prepared command is incomplete", brokercommands.ErrRejected)
	}
	if prepared.Message.DeviceID != row.DeviceID || prepared.Message.Type != downlink.MessageTypeCommand || prepared.Message.TopicPrefix != "" || prepared.Message.Topic != "" {
		return fmt.Errorf("%w: prepared command routing fields are invalid", brokercommands.ErrRejected)
	}
	fingerprint, _, snapshot, err := readCommandFingerprint(ctx, tx, row.TenantID, row.DeviceID, row.TargetDeviceID, prepared.Message.DeviceConfigID)
	if err != nil {
		return err
	}
	if len(snapshot.Devices) == 0 {
		return fmt.Errorf("%w: prepared command target snapshot is empty", brokercommands.ErrRejected)
	}
	if prepared.Message.DeviceNumber == "" || prepared.Message.DeviceNumber != snapshot.Devices[len(snapshot.Devices)-1].DeviceNumber {
		return fmt.Errorf("%w: prepared command target device number is invalid", brokercommands.ErrRejected)
	}
	if fingerprint != prepared.ConfigurationSHA256 {
		return fmt.Errorf("%w: command configuration changed after acceptance", brokercommands.ErrFenced)
	}
	return nil
}

func stringPtr(s string) *string { return &s }
