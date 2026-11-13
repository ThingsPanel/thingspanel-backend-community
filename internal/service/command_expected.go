package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"project/internal/brokercommands"
	bc "project/internal/brokercontract"
	"project/internal/brokerplugin"
	"project/internal/downlink"
	"project/internal/model"
	"project/internal/processor"
	"project/pkg/global"
)

func expectedDownlinkType(kind string) (downlink.MessageType, string, error) {
	switch kind {
	case "command":
		return downlink.MessageTypeCommand, processor.ScriptTypeCommand, nil
	case "telemetry":
		return downlink.MessageTypeTelemetry, processor.ScriptTypeTelemetryDownlink, nil
	case "attribute":
		return downlink.MessageTypeAttributeSet, processor.ScriptTypeAttributeDownlink, nil
	case "attribute_get":
		return downlink.MessageTypeAttributeGet, "", nil
	}
	return "", "", fmt.Errorf("%w: unsupported expected message family", brokercommands.ErrRejected)
}

// A delayed message preserves the human who scheduled it. It is neither an
// anonymous automatic command nor an AI authorization.
func validateExpectedCreator(ctx context.Context, tx *gorm.DB, tenant, caller string) error {
	return validateControlCaller(ctx, tx, tenant, caller, "api/v1/expected/data")
}

func validateControlCaller(ctx context.Context, tx *gorm.DB, tenant, caller, route string) error {
	if ctx == nil || tx == nil || tenant == "" || caller == "" {
		return brokercommands.ErrRejected
	}
	var u model.User
	if err := tx.WithContext(ctx).Where("id=?", caller).Take(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return brokercommands.ErrRejected
		}
		return err
	}
	if u.Status == nil || *u.Status != "N" || u.TenantID == nil || *u.TenantID != tenant {
		return brokercommands.ErrRejected
	}
	if global.CasbinEnforcer == nil {
		return errors.New("expected message authorization unavailable")
	}
	if len(global.CasbinEnforcer.GetFilteredNamedGroupingPolicy("g2", 0, route)) > 0 {
		ok, err := global.CasbinEnforcer.Enforce(caller, route, "allow")
		if err != nil {
			return err
		}
		if !ok {
			return brokercommands.ErrRejected
		}
	}
	return nil
}

func validateExpectedDownlinkCaller(ctx context.Context, tx *gorm.DB, row brokercommands.Row) error {
	kind := strings.TrimPrefix(row.Intent, "expected.")
	if _, _, err := expectedDownlinkType(kind); err != nil {
		return err
	}
	var saved struct {
		ExpectedID, TenantID, DeviceID, CallerID, SendType, PayloadSHA256 string
	}
	if err := tx.WithContext(ctx).Table("broker_expected_context").Where("expected_id=?", row.IdempotencyKey).Take(&saved).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return brokercommands.ErrRejected
		}
		return err
	}
	var ed model.ExpectedData
	if err := tx.WithContext(ctx).Where("id=? AND tenant_id=?", row.IdempotencyKey, row.TenantID).Take(&ed).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return brokercommands.ErrRejected
		}
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(ed.Payload)))
	if saved.TenantID != row.TenantID || saved.DeviceID != row.DeviceID || saved.CallerID != row.CallerID || saved.SendType != kind || saved.PayloadSHA256 != digest || ed.DeviceID != row.DeviceID || ed.SendType != kind || (ed.Status != "pending" && ed.Status != "sent") || (ed.ExpiryTime != nil && !ed.ExpiryTime.After(time.Now())) {
		return brokercommands.ErrRejected
	}
	var canonical struct {
		ExpectedID, Kind, Payload string
		Expiry                    *time.Time
	}
	if err := json.Unmarshal(row.CanonicalRequest, &canonical); err != nil {
		return brokercommands.ErrRejected
	}
	if canonical.ExpectedID != ed.ID || canonical.Kind != ed.SendType || canonical.Payload != ed.Payload || (canonical.Expiry == nil) != (ed.ExpiryTime == nil) || (canonical.Expiry != nil && !canonical.Expiry.Equal(*ed.ExpiryTime)) {
		return brokercommands.ErrRejected
	}
	return validateExpectedCreator(ctx, tx, row.TenantID, row.CallerID)
}

func prepareExpectedDownlink(ctx context.Context, ed *model.ExpectedData) (brokercommands.PreparedCommand, string, error) {
	var prepared brokercommands.PreparedCommand
	typ, scriptType, err := expectedDownlinkType(ed.SendType)
	if err != nil {
		return prepared, "", err
	}
	if len(ed.Payload) == 0 || len(ed.Payload) > brokercommands.MaxCommandPayload {
		return prepared, "", brokercommands.ErrInvalid
	}
	if err := bc.ValidateUniqueJSON([]byte(ed.Payload)); err != nil {
		return prepared, "", err
	}
	decoder := json.NewDecoder(strings.NewReader(ed.Payload))
	decoder.UseNumber()
	var data map[string]interface{}
	if err := decoder.Decode(&data); err != nil || data == nil {
		return prepared, "", brokercommands.ErrInvalid
	}
	if typ == downlink.MessageTypeCommand {
		if method, ok := data["method"].(string); !ok || strings.TrimSpace(method) == "" {
			return prepared, "", brokercommands.ErrInvalid
		}
	}
	var target string
	err = global.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current := ed.DeviceID
		seen := map[string]bool{}
		var root model.Device
		for depth := 0; depth < 8; depth++ {
			if seen[current] {
				return brokercommands.ErrRejected
			}
			seen[current] = true
			if err := tx.Where("id=? AND tenant_id=?", current, ed.TenantID).Take(&root).Error; err != nil {
				return err
			}
			pluginRoute, routeErr := brokerplugin.Resolve(tx.WithContext(ctx), &root)
			if routeErr != nil {
				return routeErr
			}
			if pluginRoute != nil || root.ParentID == nil || *root.ParentID == "" {
				target = root.ID
				break
			}
			current = *root.ParentID
		}
		if target == "" {
			return brokercommands.ErrRejected
		}
		cfg := ""
		if root.DeviceConfigID != nil {
			cfg = *root.DeviceConfigID
		}
		fingerprint, script, snapshot, err := readDownlinkFingerprint(ctx, tx, ed.TenantID, ed.DeviceID, target, cfg, scriptType)
		if err != nil {
			return err
		}
		deviceType := snapshot.Devices[0].DeviceType
		if snapshot.Plugin != nil {
			deviceType = snapshotWireDeviceType(snapshot)
		}
		nested, err := transformDurableDownlinkData(data, snapshot, deviceType)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(nested)
		if err != nil {
			return err
		}
		prepared.Message = downlink.Message{DeviceID: ed.DeviceID, DeviceNumber: root.DeviceNumber, DeviceType: deviceType, DeviceConfigID: cfg, Type: typ, Data: raw, TopicPrefix: snapshotTopicPrefix(snapshot)}
		prepared.ConfigurationSHA256 = fingerprint
		prepared.MappingVersion = effectiveMappingVersion(snapshot.MappingVersion)
		prepared.Encoded = append([]byte(nil), raw...)
		if script != nil {
			if script.Content == nil || *script.Content == "" {
				return brokercommands.ErrRejected
			}
			encoded, err := processor.NewLuaExecutor().ExecuteEncode(ctx, *script.Content, raw)
			if err != nil {
				return err
			}
			prepared.Encoded = []byte(encoded)
		}
		if len(prepared.Encoded) == 0 || len(prepared.Encoded) > brokercommands.MaxCommandPayload {
			return brokercommands.ErrInvalid
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	return prepared, target, err
}

func submitExpectedDownlink(ctx context.Context, ed *model.ExpectedData, caller string) (string, error) {
	if ctx == nil || ed == nil || !isIngressV1Commands() {
		return "", brokercommands.ErrInvalid
	}
	store, err := getDurableCommandStore(global.DB)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		ExpectedID, Kind, Payload string
		Expiry                    *time.Time
	}{ed.ID, ed.SendType, ed.Payload, ed.ExpiryTime})
	if err != nil {
		return "", err
	}
	intent := "expected." + ed.SendType
	// Retry an accepted intent without rereading/re-encoding a changed device.
	row, found, err := store.FindIdempotent(ctx, ed.TenantID, caller, intent, ed.ID, canonical)
	if err != nil {
		return "", err
	}
	if found {
		return row.ID, nil
	}
	prepared, target, err := prepareExpectedDownlink(ctx, ed)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(prepared)
	if err != nil {
		return "", err
	}
	expiry := time.Now().Add(defaultCommandTTL).UTC()
	if ed.ExpiryTime != nil && ed.ExpiryTime.Before(expiry) {
		expiry = *ed.ExpiryTime
	}
	row, _, err = store.Submit(ctx, brokercommands.Request{TenantID: ed.TenantID, CallerID: caller, Intent: intent, IdempotencyKey: ed.ID, DeviceID: ed.DeviceID, TargetDeviceID: target, CanonicalRequest: canonical, Payload: payload, ExpiresAt: expiry}, func(tx *gorm.DB, row brokercommands.Row) error {
		if err := validateExpectedDownlinkCaller(ctx, tx, row); err != nil {
			return err
		}
		if err := validateExpectedDownlinkSnapshot(ctx, tx, row, prepared); err != nil {
			return err
		}
		op, status, data := "1", "0", string(prepared.Message.Data)
		switch prepared.Message.Type {
		case downlink.MessageTypeCommand:
			var command struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal([]byte(ed.Payload), &command)
			if err := tx.Create(&model.CommandSetLog{ID: row.ID, DeviceID: row.DeviceID, OperationType: &op, MessageID: &row.ID, Identify: &command.Method, Datum: &data, Status: &status, CreatedAt: row.CreatedAt}).Error; err != nil {
				return err
			}
		case downlink.MessageTypeAttributeSet:
			if err := tx.Create(&model.AttributeSetLog{ID: row.ID, DeviceID: row.DeviceID, OperationType: &op, MessageID: &row.ID, Datum: &data, Status: &status, CreatedAt: row.CreatedAt, UserID: &row.CallerID}).Error; err != nil {
				return err
			}
		case downlink.MessageTypeTelemetry:
			if err := tx.Create(&model.TelemetrySetLog{ID: row.ID, DeviceID: row.DeviceID, OperationType: &op, Datum: &data, Status: &status, CreatedAt: row.CreatedAt, UserID: &row.CallerID}).Error; err != nil {
				return err
			}
		}
		res := tx.Model(&model.ExpectedData{}).Where("id=? AND tenant_id=? AND status='pending'", ed.ID, ed.TenantID).Updates(map[string]any{"status": "sent", "message": row.ID, "send_time": time.Now().UTC()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return brokercommands.ErrRejected
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

func validateExpectedDownlinkSnapshot(ctx context.Context, tx *gorm.DB, row brokercommands.Row, p brokercommands.PreparedCommand) error {
	typ, scriptType, err := expectedDownlinkType(durableControlKind(row.Intent))
	if err != nil {
		return err
	}
	if p.Message.Type != typ || p.Message.DeviceID != row.DeviceID || p.Message.Topic != "" {
		return brokercommands.ErrRejected
	}
	fingerprint, _, snapshot, err := readDownlinkFingerprint(ctx, tx, row.TenantID, row.DeviceID, row.TargetDeviceID, p.Message.DeviceConfigID, scriptType)
	if err != nil {
		return err
	}
	if len(snapshot.Devices) == 0 || p.Message.DeviceNumber != snapshot.Devices[len(snapshot.Devices)-1].DeviceNumber || p.Message.TopicPrefix != snapshotTopicPrefix(snapshot) || p.Message.DeviceType != snapshotWireDeviceType(snapshot) || effectiveMappingVersion(p.MappingVersion) != effectiveMappingVersion(snapshot.MappingVersion) || fingerprint != p.ConfigurationSHA256 {
		return brokercommands.ErrFenced
	}
	return nil
}

func durableControlKind(intent string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(intent, "expected."), "control."), nativeAutomationPrefix)
}
func standardControlRoute(kind string) string {
	switch kind {
	case "telemetry":
		return "api/v1/telemetry/datas/pub"
	case "attribute":
		return "api/v1/attribute/datas/pub"
	case "attribute_get":
		return "api/v1/attribute/datas/get"
	}
	return ""
}
func validateStandardControlCaller(ctx context.Context, tx *gorm.DB, row brokercommands.Row) error {
	route := standardControlRoute(durableControlKind(row.Intent))
	if route == "" {
		return brokercommands.ErrRejected
	}
	return validateControlCaller(ctx, tx, row.TenantID, row.CallerID, route)
}

// Standard manual controls reuse the same durable owner and frozen encoding as
// expected messages, without creating a scheduled-message row or a second bus.
func submitStandardControl(ctx context.Context, caller, deviceID, kind, value string) (string, error) {
	if ctx == nil || global.DB == nil || caller == "" || deviceID == "" || standardControlRoute(kind) == "" {
		return "", brokercommands.ErrRejected
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var user model.User
	if err := global.DB.WithContext(ctx).Where("id=?", caller).Take(&user).Error; err != nil {
		return "", brokercommands.ErrRejected
	}
	if user.TenantID == nil {
		return "", brokercommands.ErrRejected
	}
	tenant := *user.TenantID
	if err := validateControlCaller(ctx, global.DB, tenant, caller, standardControlRoute(kind)); err != nil {
		return "", err
	}
	prepared, target, err := prepareExpectedDownlink(ctx, &model.ExpectedData{DeviceID: deviceID, TenantID: tenant, SendType: kind, Payload: value})
	if err != nil {
		return "", err
	}
	store, err := getDurableCommandStore(global.DB)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(prepared)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct{ DeviceID, Kind, Value string }{deviceID, kind, value})
	if err != nil {
		return "", err
	}
	row, _, err := store.Submit(ctx, brokercommands.Request{TenantID: tenant, CallerID: caller, Intent: "control." + kind, DeviceID: deviceID, TargetDeviceID: target, CanonicalRequest: canonical, Payload: payload, ExpiresAt: time.Now().Add(defaultCommandTTL)}, func(tx *gorm.DB, row brokercommands.Row) error {
		if err := validateStandardControlCaller(ctx, tx, row); err != nil {
			return err
		}
		if err := validateExpectedDownlinkSnapshot(ctx, tx, row, prepared); err != nil {
			return err
		}
		op, status, data := "1", "0", string(prepared.Message.Data)
		if kind == "attribute" {
			return tx.Create(&model.AttributeSetLog{ID: row.ID, DeviceID: row.DeviceID, OperationType: &op, MessageID: &row.ID, Datum: &data, Status: &status, CreatedAt: row.CreatedAt, UserID: &row.CallerID}).Error
		}
		if kind == "telemetry" {
			return tx.Create(&model.TelemetrySetLog{ID: row.ID, DeviceID: row.DeviceID, OperationType: &op, Datum: &data, Status: &status, CreatedAt: row.CreatedAt, UserID: &row.CallerID}).Error
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

func snapshotWireDeviceType(snapshot commandFingerprintSnapshot) string {
	if snapshot.Plugin != nil && snapshot.Plugin.ServiceType == 2 {
		return "1"
	}
	if len(snapshot.Devices) == 0 {
		return ""
	}
	return snapshot.Devices[0].DeviceType
}
