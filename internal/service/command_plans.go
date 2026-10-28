package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/spf13/viper"
	"gorm.io/gorm"
	"project/internal/brokercommands"
	"project/internal/brokersemantic"
	"project/internal/brokerstate"
	"project/internal/model"
	"project/pkg/constant"
	"project/pkg/global"
	"project/pkg/utils"
)

const plannedCommandIntent = "device.command.plan"

type CommandPlanPreview struct {
	PlanID                string              `json:"plan_id"`
	PlanHash              string              `json:"plan_hash"`
	StateVersion          string              `json:"state_version"`
	ExpiresAt             time.Time           `json:"expires_at"`
	Command               json.RawMessage     `json:"command"`
	SemanticPins          brokersemantic.Pins `json:"semantic_pins"`
	AuthorizationRequired bool                `json:"authorization_required"`
}

type CommandPlanUse struct {
	PlanHash        string `json:"plan_hash"`
	AuthorizationID string `json:"authorization_id,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

type commandPlanInput struct {
	Command        json.RawMessage     `json:"command"`
	SemanticPins   brokersemantic.Pins `json:"semantic_pins"`
	SemanticSHA256 string              `json:"semantic_sha256"`
}

func commandPlansAvailable() bool {
	return isIngressV1Commands() && viper.GetBool("mqtt.route_fencing") && viper.GetBool("mqtt.command_plans_enabled")
}

func semanticPlanDigest(desc brokersemantic.Description) (string, error) {
	raw, err := json.Marshal(desc)
	if err != nil {
		return "", err
	}
	var canonical any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&canonical); err != nil {
		return "", err
	}
	raw, err = json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// PreviewCommandPlan freezes a deterministic command using the existing
// command preparation owner. It neither grants authority nor publishes bytes.
func (c *CommandData) PreviewCommandPlan(ctx context.Context, claims *utils.UserClaims, token string, req *model.PutMessageForCommand) (CommandPlanPreview, error) {
	if ctx == nil {
		return CommandPlanPreview{}, brokercommands.ErrInvalid
	}
	if !commandPlansAvailable() {
		return CommandPlanPreview{}, ErrBrokerSemanticUnsupported
	}
	if req == nil {
		return CommandPlanPreview{}, brokercommands.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	evidence, err := GroupApp.Device.BrokerSemanticEvidence(ctx, claims, token, req.DeviceID)
	if err != nil {
		return CommandPlanPreview{}, err
	}
	if err := brokersemantic.ValidateCommand(evidence.Description, req.Identify, req.Value); err != nil {
		return CommandPlanPreview{}, err
	}
	semanticSHA, err := semanticPlanDigest(evidence.Description)
	if err != nil {
		return CommandPlanPreview{}, err
	}
	copyReq := *req
	copyReq.IdempotencyKey = ""
	if copyReq.ExpiresAt == nil {
		expiry := time.Now().Add(30 * time.Second).UnixMilli()
		copyReq.ExpiresAt = &expiry
	}
	var preview CommandPlanPreview
	_, err = c.commandPutMessageWithResult(ctx, claims.ID, &copyReq, strconv.Itoa(constant.Manual), claims.TenantID, true, func(entry durablePreparation) (string, error) {
		entry.Request.Intent = plannedCommandIntent
		input := commandPlanInput{Command: entry.Request.CanonicalRequest, SemanticPins: evidence.Description.Pins, SemanticSHA256: semanticSHA}
		encoded, err := json.Marshal(input)
		if err != nil {
			return "", err
		}
		entry.Request.CanonicalRequest = encoded
		var stateSHA []byte
		// Obtain a candidate version under the root binding; CreatePlan rechecks
		// it under the same admission lock order before saving the preview.
		err = global.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var binding struct{ Phase string }
			if err := tx.Raw(`SELECT phase FROM broker_transport_bindings WHERE tenant_id=? AND device_id=? FOR UPDATE`, claims.TenantID, entry.Request.TargetDeviceID).Scan(&binding).Error; err != nil {
				return err
			}
			if binding.Phase != "active" {
				return brokercommands.ErrFenced
			}
			var err error
			stateSHA, err = brokerstate.CommandContext(ctx, tx, claims.TenantID, entry.Request.DeviceID, entry.Request.TargetDeviceID)
			return err
		})
		if err != nil {
			return "", err
		}
		plan, err := brokercommands.New(global.DB).CreatePlan(ctx, entry.Request, stateSHA, validateCommandPlanContext)
		if err != nil {
			return "", err
		}
		preview = CommandPlanPreview{PlanID: plan.ID, PlanHash: hex.EncodeToString(plan.PlanSHA256), StateVersion: hex.EncodeToString(plan.ContextSHA256), ExpiresAt: plan.ExpiresAt, Command: input.Command, SemanticPins: input.SemanticPins, AuthorizationRequired: true}
		return plan.ID, nil
	})
	return preview, err
}

func validateCommandPlanContext(ctx context.Context, tx *gorm.DB, p brokercommands.Plan) error {
	if p.Intent != plannedCommandIntent {
		return brokercommands.ErrRejected
	}
	row := brokercommands.Row{TenantID: p.TenantID, CallerID: p.CallerID, Intent: manualCommandIntent, DeviceID: p.DeviceID, TargetDeviceID: p.TargetDeviceID, Payload: p.Payload}
	if err := ValidateDurableCommandCaller(ctx, tx, row); err != nil {
		return err
	}
	if err := ValidateDurableCommandSnapshot(ctx, tx, row); err != nil {
		return err
	}
	stateSHA, err := brokerstate.CommandContext(ctx, tx, p.TenantID, p.DeviceID, p.TargetDeviceID)
	if errors.Is(err, brokerstate.ErrFenced) {
		return brokercommands.ErrFenced
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(stateSHA, p.ContextSHA256) {
		return brokercommands.ErrFenced
	}
	return nil
}

func validatedPlanUse(ctx context.Context, claims *utils.UserClaims, token, id string, use CommandPlanUse) (brokercommands.Plan, []byte, error) {
	if !commandPlansAvailable() {
		return brokercommands.Plan{}, nil, ErrBrokerSemanticUnsupported
	}
	if err := ValidateBrokerSubject(ctx, claims, token); err != nil {
		return brokercommands.Plan{}, nil, err
	}
	if err := ValidateDurableCommandCaller(ctx, global.DB, brokercommands.Row{TenantID: claims.TenantID, CallerID: claims.ID, Intent: manualCommandIntent}); err != nil {
		return brokercommands.Plan{}, nil, err
	}
	hash, err := hex.DecodeString(use.PlanHash)
	if err != nil || len(hash) != 32 {
		return brokercommands.Plan{}, nil, brokercommands.ErrInvalid
	}
	plan, err := brokercommands.New(global.DB).GetPlan(ctx, claims.TenantID, claims.ID, id)
	if err != nil {
		return plan, nil, err
	}
	if !bytes.Equal(hash, plan.PlanSHA256) {
		return plan, nil, brokercommands.ErrConflict
	}
	if plan.Intent != plannedCommandIntent {
		return plan, nil, brokercommands.ErrRejected
	}
	return plan, hash, nil
}

func verifyCurrentPlanSemantics(ctx context.Context, claims *utils.UserClaims, token string, p brokercommands.Plan) error {
	var input commandPlanInput
	if err := json.Unmarshal(p.CanonicalRequest, &input); err != nil {
		return brokercommands.ErrRejected
	}
	evidence, err := GroupApp.Device.BrokerSemanticEvidence(ctx, claims, token, p.DeviceID)
	if err != nil {
		return err
	}
	digest, err := semanticPlanDigest(evidence.Description)
	if err != nil {
		return err
	}
	if digest != input.SemanticSHA256 || evidence.Description.Pins != input.SemanticPins {
		return brokercommands.ErrFenced
	}
	var command canonicalCommandRequest
	if err := json.Unmarshal(input.Command, &command); err != nil {
		return brokercommands.ErrRejected
	}
	// Immutable model bytes were already typed-validated at preview. Exact
	// current semantic digest prevents replacing the published binding here.
	if command.DeviceID != p.DeviceID {
		return brokercommands.ErrRejected
	}
	return nil
}

// AuthorizeCommandPlan belongs only to a human confirmation route; it is never
// registered in model tools. Models cannot manufacture this server record.
func (c *CommandData) AuthorizeCommandPlan(ctx context.Context, claims *utils.UserClaims, token, id string, use CommandPlanUse) (string, error) {
	if ctx == nil {
		return "", brokercommands.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, hash, err := validatedPlanUse(ctx, claims, token, id, use)
	if err != nil {
		return "", err
	}
	if err := verifyCurrentPlanSemantics(ctx, claims, token, p); err != nil {
		return "", err
	}
	return brokercommands.New(global.DB).AuthorizePlan(ctx, p.TenantID, p.CallerID, p.ID, hash, validateCommandPlanContext)
}

func (c *CommandData) SubmitCommandPlan(ctx context.Context, claims *utils.UserClaims, token, id string, use CommandPlanUse) (string, error) {
	if ctx == nil {
		return "", brokercommands.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, hash, err := validatedPlanUse(ctx, claims, token, id, use)
	if err != nil {
		return "", err
	}
	if p.CommandID == nil {
		if err := verifyCurrentPlanSemantics(ctx, claims, token, p); err != nil {
			return "", err
		}
	}
	var prepared brokercommands.PreparedCommand
	var input commandPlanInput
	var canonical canonicalCommandRequest
	if json.Unmarshal(p.Payload, &prepared) != nil || json.Unmarshal(p.CanonicalRequest, &input) != nil || json.Unmarshal(input.Command, &canonical) != nil {
		return "", brokercommands.ErrRejected
	}
	entry := durablePreparation{Prepared: prepared, Identify: canonical.Identify, OperationType: strconv.Itoa(constant.Manual)}
	row, _, err := brokercommands.New(global.DB).SubmitPlan(ctx, p.TenantID, p.CallerID, p.ID, hash, use.AuthorizationID, use.IdempotencyKey, validateCommandPlanContext, func(tx *gorm.DB, row brokercommands.Row) error { return persistPreparedCommand(ctx, tx, row, entry) })
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

// validatePlannedCommandFinal executes again in the existing dispatcher under
// the root binding lock; no separate AI queue or MQTT client exists.
func validatePlannedCommandFinal(ctx context.Context, tx *gorm.DB, row brokercommands.Row) error {
	if !commandPlansAvailable() {
		return brokercommands.ErrRejected
	}
	var p brokercommands.Plan
	if err := tx.Where("tenant_id=? AND caller_id=? AND command_id=?", row.TenantID, row.CallerID, row.ID).Take(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return brokercommands.ErrRejected
		}
		return err
	}
	if p.AuthorizationID == nil || p.RevokedAt != nil || p.CommandID == nil || *p.CommandID != row.ID || !p.ExpiresAt.After(time.Now()) || p.RouteCell != row.RouteCell || p.RouteEpoch != row.RouteEpoch {
		return brokercommands.ErrRejected
	}
	return validateCommandPlanContext(ctx, tx, p)
}
