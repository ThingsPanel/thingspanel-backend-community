package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"project/internal/automationbridge"
	"project/internal/brokercommands"
	"project/internal/model"
)

// Validate before saving a delayed command as pending, then validate again at
// preparation against the current binding. Existing records cannot bypass it.
func validateStoredNativeCommand(ctx context.Context, tx *gorm.DB, ed *model.ExpectedData) error {
	if ed.SendType != "command" {
		return nil
	}
	var data map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(ed.Payload))
	decoder.UseNumber()
	if decoder.Decode(&data) != nil {
		return brokercommands.ErrInvalid
	}
	var device model.Device
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=?", ed.DeviceID, ed.TenantID).Take(&device).Error; err != nil {
		return err
	}
	if device.DeviceConfigID == nil || *device.DeviceConfigID == "" {
		return nil
	}
	var config model.DeviceConfig
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=?", *device.DeviceConfigID, ed.TenantID).Take(&config).Error; err != nil {
		return err
	}
	if !hasNativeDeviceConfigBinding(&config) {
		return nil
	}
	method, ok := data["method"].(string)
	if !ok {
		return brokercommands.ErrInvalid
	}
	params, err := json.Marshal(data["params"])
	if err != nil {
		return brokercommands.ErrInvalid
	}
	return validateOwnerNativeCommand(ctx, ed.TenantID, ed.DeviceID, &config, method, params)
}

// validateSnapshotCommand uses the same configuration snapshot which is hashed
// into the accepted row. The logical child, not its transport gateway, owns the
// command schema. Binding changes are fenced by the existing dispatch check.
func validateSnapshotCommand(ctx context.Context, snapshot commandFingerprintSnapshot, data map[string]interface{}) error {
	if len(snapshot.Devices) == 0 {
		return brokercommands.ErrRejected
	}
	device := snapshot.Devices[0]
	if device.DeviceConfigID == nil || *device.DeviceConfigID == "" {
		return nil
	}
	for _, cfg := range snapshot.Configs {
		if cfg.ID != *device.DeviceConfigID {
			continue
		}
		config := &model.DeviceConfig{ID: cfg.ID, TenantID: cfg.TenantID, DeviceTemplateID: cfg.DeviceTemplateID,
			NativeThingModelID: cfg.NativeThingModelID, NativeThingModelSnapshotID: cfg.NativeThingModelSnapshotID,
			NativeThingModelRevision: cfg.NativeThingModelRevision, NativeBindingRevision: cfg.NativeBindingRevision}
		if !hasNativeDeviceConfigBinding(config) {
			return nil
		}
		method, ok := data["method"].(string)
		if !ok {
			return brokercommands.ErrInvalid
		}
		params, err := json.Marshal(data["params"])
		if err != nil {
			return brokercommands.ErrInvalid
		}
		return validateOwnerNativeCommand(ctx, device.TenantID, device.ID, config, method, params)
	}
	return brokercommands.ErrFenced
}

type nativeCommandValidationRequest struct {
	DeviceID        string          `json:"device_id"`
	ConfigID        string          `json:"config_id"`
	ModelID         string          `json:"model_id"`
	SnapshotID      string          `json:"snapshot_id"`
	Revision        int             `json:"revision"`
	BindingRevision int             `json:"binding_revision"`
	Identifier      string          `json:"identifier"`
	Params          json.RawMessage `json:"params"`
}

// Only a trusted Community owner calls this result-only service. No user token
// is stored for scheduled work and no model bytes leave the model owner.
func validateOwnerNativeCommand(ctx context.Context, tenant, device string, cfg *model.DeviceConfig, identifier string, params json.RawMessage) error {
	if cfg == nil || tenant == "" || device == "" || cfg.TenantID != tenant || cfg.DeviceTemplateID != nil || cfg.NativeThingModelID == nil || cfg.NativeThingModelSnapshotID == nil || cfg.NativeThingModelRevision == nil || cfg.NativeBindingRevision < 1 {
		return brokercommands.ErrRejected
	}
	if strings.TrimSpace(*cfg.NativeThingModelID) == "" || strings.TrimSpace(*cfg.NativeThingModelSnapshotID) == "" || *cfg.NativeThingModelRevision < 1 {
		return brokercommands.ErrRejected
	}
	token := strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_TOKEN"))
	deployment := automationbridge.Deployment()
	if len(token) < 32 || deployment == "" {
		return fmt.Errorf("native command validation owner identity unavailable")
	}
	client, err := newNativeMarketImportClient()
	if err != nil {
		return err
	}
	input := nativeCommandValidationRequest{device, cfg.ID, *cfg.NativeThingModelID, *cfg.NativeThingModelSnapshotID, *cfg.NativeThingModelRevision, cfg.NativeBindingRevision, identifier, params}
	body, err := json.Marshal(input)
	if err != nil {
		return brokercommands.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/internal/thingmodel/validate-command", bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(automationbridge.TenantHeader, tenant)
	r.Header.Set(automationbridge.DeploymentHeader, deployment)
	response, err := client.httpClient.Do(r)
	if err != nil {
		return fmt.Errorf("native command validation unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusConflict {
		return fmt.Errorf("%w: native command does not match its published binding", brokercommands.ErrInvalid)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("native command validation unavailable (status %d)", response.StatusCode)
	}
	var result struct {
		Valid           bool   `json:"valid"`
		DeviceID        string `json:"device_id"`
		SnapshotID      string `json:"snapshot_id"`
		BindingRevision int    `json:"binding_revision"`
	}
	d := json.NewDecoder(io.LimitReader(response.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&result) != nil || d.Decode(new(any)) != io.EOF || !result.Valid || result.DeviceID != device || result.SnapshotID != input.SnapshotID || result.BindingRevision != input.BindingRevision {
		return brokercommands.ErrRejected
	}
	return nil
}
