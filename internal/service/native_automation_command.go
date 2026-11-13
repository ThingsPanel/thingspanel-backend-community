package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"project/internal/automationbridge"
	"project/internal/brokercommands"
	"project/internal/model"
	"project/pkg/global"
	"strings"
	"time"
)

const nativeAutomationPrefix = "native_automation."

var ErrNativeSuperseded = errors.New("native automation generation superseded")

type NativeCommand struct {
	TenantID, DeploymentID, Kind, DeviceID, Identifier, Value, Key, Scope string
	Generation                                                            int64
}

func nativeAutomationScope(deployment, tenant, scope string) string {
	return fmt.Sprintf("native:%x", sha256.Sum256([]byte(deployment+"\n"+tenant+"\n"+scope)))
}

func SubmitNativeAutomationCommand(ctx context.Context, req NativeCommand) (string, error) {
	if !automationbridge.NativeEnabled() || !isIngressV1Commands() || req.TenantID == "" || req.DeviceID == "" || req.DeploymentID == "" || req.DeploymentID != automationbridge.Deployment() || req.Key == "" || req.Scope == "" || req.Generation < 1 {
		return "", brokercommands.ErrRejected
	}
	if _, _, err := expectedDownlinkType(req.Kind); err != nil {
		return "", err
	}
	if req.Kind != "telemetry" && req.Kind != "attribute" && req.Kind != "command" {
		return "", brokercommands.ErrRejected
	}
	scope := nativeAutomationScope(req.DeploymentID, req.TenantID, req.Scope)
	if err := global.DB.WithContext(ctx).Exec(`INSERT INTO automation_scope_generation(scope_key,generation,updated_at) VALUES(?,?,now()) ON CONFLICT(scope_key) DO UPDATE SET generation=GREATEST(automation_scope_generation.generation,excluded.generation),updated_at=now()`, scope, req.Generation).Error; err != nil {
		return "", err
	}
	value := req.Value
	if req.Kind == "command" {
		if req.Identifier == "" || !json.Valid([]byte(value)) {
			return "", brokercommands.ErrInvalid
		}
		raw, err := json.Marshal(map[string]any{"method": req.Identifier, "params": json.RawMessage(value)})
		if err != nil {
			return "", err
		}
		value = string(raw)
	}
	prepared, target, err := prepareExpectedDownlink(ctx, &model.ExpectedData{TenantID: req.TenantID, DeviceID: req.DeviceID, SendType: req.Kind, Payload: value})
	if err != nil {
		return "", err
	}
	prepared.NativeAutomation = &brokercommands.NativeAutomationProof{DeploymentID: req.DeploymentID, Scope: scope, Generation: req.Generation}
	payload, err := json.Marshal(prepared)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	row, _, err := brokercommands.New(global.DB).Submit(ctx, brokercommands.Request{TenantID: req.TenantID, CallerID: "native-automation:" + req.DeploymentID, Intent: nativeAutomationPrefix + req.Kind, IdempotencyKey: req.Key, DeviceID: req.DeviceID, TargetDeviceID: target, CanonicalRequest: canonical, Payload: payload, ExpiresAt: time.Now().Add(defaultCommandTTL)}, func(tx *gorm.DB, row brokercommands.Row) error {
		if err := ValidateNativeAutomationCommand(ctx, tx, row); err != nil {
			return err
		}
		if err := validateExpectedDownlinkSnapshot(ctx, tx, row, prepared); err != nil {
			return err
		}
		op, status, data := "2", "0", string(prepared.Message.Data)
		switch req.Kind {
		case "telemetry":
			return tx.Create(&model.TelemetrySetLog{ID: row.ID, DeviceID: row.DeviceID, OperationType: &op, Datum: &data, Status: &status, CreatedAt: row.CreatedAt}).Error
		case "attribute":
			return tx.Create(&model.AttributeSetLog{ID: row.ID, DeviceID: row.DeviceID, OperationType: &op, MessageID: &row.ID, Datum: &data, Status: &status, CreatedAt: row.CreatedAt}).Error
		default:
			identify := req.Identifier
			return tx.Create(&model.CommandSetLog{ID: row.ID, DeviceID: row.DeviceID, OperationType: &op, MessageID: &row.ID, Identify: &identify, Datum: &data, Status: &status, CreatedAt: row.CreatedAt}).Error
		}
	})
	return row.ID, err
}

func ValidateNativeAutomationCommand(ctx context.Context, tx *gorm.DB, row brokercommands.Row) error {
	if !automationbridge.NativeEnabled() {
		return brokercommands.ErrFenced
	}
	var prepared brokercommands.PreparedCommand
	if json.Unmarshal(row.Payload, &prepared) != nil {
		return brokercommands.ErrRejected
	}
	p := prepared.NativeAutomation
	if p == nil || p.DeploymentID == "" || p.DeploymentID != automationbridge.Deployment() || row.CallerID != "native-automation:"+p.DeploymentID || !strings.HasPrefix(row.Intent, nativeAutomationPrefix) || p.Generation < 1 {
		return brokercommands.ErrRejected
	}
	var accepted NativeCommand
	if json.Unmarshal(row.CanonicalRequest, &accepted) != nil || accepted.TenantID != row.TenantID || accepted.DeviceID != row.DeviceID || accepted.DeploymentID != p.DeploymentID || accepted.Kind != strings.TrimPrefix(row.Intent, nativeAutomationPrefix) || accepted.Key != row.IdempotencyKey || accepted.Generation != p.Generation || accepted.Scope == "" || nativeAutomationScope(accepted.DeploymentID, accepted.TenantID, accepted.Scope) != p.Scope {
		return brokercommands.ErrRejected
	}
	var generation int64
	if err := tx.WithContext(ctx).Raw("SELECT generation FROM automation_scope_generation WHERE scope_key=?", p.Scope).Scan(&generation).Error; err != nil {
		return err
	}
	if generation != p.Generation {
		return errors.Join(ErrNativeSuperseded, brokercommands.ErrFenced)
	}
	return nil
}
