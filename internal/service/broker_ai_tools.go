package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"project/internal/brokeraitools"
	"project/internal/brokercommands"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"
)

const MaxBrokerAIToolResponseBytes = 64 << 10

type BrokerAIToolGrantRequest struct {
	DeviceIDs []string `json:"device_ids"`
	Scopes    []string `json:"scopes"`
	Replay    bool     `json:"replay,omitempty"`
}

type BrokerAIToolWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Keys  []string  `json:"keys"`
	Limit int       `json:"limit"`
}

type BrokerAIToolInput struct {
	DeviceID  string                      `json:"device_id"`
	Window    *BrokerAIToolWindow         `json:"window,omitempty"`
	Command   *model.PutMessageForCommand `json:"command,omitempty"`
	MessageID string                      `json:"message_id,omitempty"`
}

type BrokerAIToolOutput struct {
	Operation string          `json:"operation"`
	DeviceID  string          `json:"device_id"`
	TenantID  string          `json:"tenant_id"`
	SubjectID string          `json:"subject_id"`
	Data      json.RawMessage `json:"data"`
}

func brokerAIToolsAvailable() bool {
	return isIngressV1Commands() && viper.GetBool("mqtt.route_fencing") && viper.GetBool("mqtt.ai_tools_enabled")
}

// BrokerAIToolPolicyRoute is an explicit capability in the existing Casbin
// owner. Unlike legacy unconfigured routes, tools require an installed policy.
func BrokerAIToolPolicyRoute(operation string) string {
	switch operation {
	case brokeraitools.Describe, brokeraitools.Telemetry, brokeraitools.Preview, brokeraitools.Result:
		return "api/v1/broker-ai/tools/" + operation
	default:
		return ""
	}
}

func brokerAIToolPermission(claims *utils.UserClaims, operation string) error {
	if claims == nil || global.CasbinEnforcer == nil {
		return brokeraitools.ErrUnavailable
	}
	route := BrokerAIToolPolicyRoute(operation)
	if route == "" || len(global.CasbinEnforcer.GetFilteredNamedGroupingPolicy("g2", 0, route)) == 0 {
		return brokeraitools.ErrUnauthorized
	}
	allowed, err := global.CasbinEnforcer.Enforce(claims.ID, route, "allow")
	if err != nil {
		return brokeraitools.ErrUnavailable
	}
	if !allowed {
		return brokeraitools.ErrUnauthorized
	}
	return nil
}

func brokerAIHumanClaims(ctx context.Context, token string) (*utils.UserClaims, error) {
	if len(token) == 0 || len(token) > 8192 || strings.TrimSpace(token) != token {
		return nil, brokeraitools.ErrUnauthorized
	}
	key := viper.GetString("jwt.key")
	if key == "" {
		return nil, brokeraitools.ErrUnavailable
	}
	claims, err := utils.NewJWT([]byte(key)).ParseToken(token)
	if err != nil || claims == nil || ValidateBrokerSubject(ctx, claims, token) != nil {
		return nil, brokeraitools.ErrUnauthorized
	}
	return claims, nil
}

func brokerAISubject(claims *utils.UserClaims, token string) brokeraitools.Subject {
	return brokeraitools.Subject{TenantID: claims.TenantID, UserID: claims.ID, Authority: claims.Authority, SessionToken: token}
}

func brokerAIDevice(ctx context.Context, tenant, id string) error {
	if global.DB == nil {
		return brokeraitools.ErrUnavailable
	}
	var device model.Device
	err := global.DB.WithContext(ctx).Select("id").Where("id=? AND tenant_id=? AND is_enabled='enabled' AND activate_flag='active'", id, tenant).Take(&device).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return brokeraitools.ErrUnauthorized
	}
	if err != nil {
		return brokeraitools.ErrUnavailable
	}
	return nil
}

func (*Device) IssueBrokerAIToolDelegation(ctx context.Context, token string, request BrokerAIToolGrantRequest) (brokeraitools.Issued, error) {
	if ctx == nil {
		return brokeraitools.Issued{}, brokeraitools.ErrInvalid
	}
	if !brokerAIToolsAvailable() {
		return brokeraitools.Issued{}, ErrBrokerSemanticUnsupported
	}
	if brokeraitools.ValidateScope(request.DeviceIDs, request.Scopes) != nil {
		return brokeraitools.Issued{}, brokeraitools.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	claims, err := brokerAIHumanClaims(ctx, token)
	if err != nil {
		return brokeraitools.Issued{}, err
	}
	for _, op := range request.Scopes {
		if request.Replay && op == brokeraitools.Preview {
			return brokeraitools.Issued{}, brokeraitools.ErrUnauthorized
		}
		if err := brokerAIToolPermission(claims, op); err != nil {
			return brokeraitools.Issued{}, err
		}
		if op == brokeraitools.Preview && !commandPlansAvailable() {
			return brokeraitools.Issued{}, ErrBrokerSemanticUnsupported
		}
		if op != brokeraitools.Result {
			if _, err := configuredSemanticClient(); err != nil {
				return brokeraitools.Issued{}, ErrBrokerSemanticUnsupported
			}
		}
	}
	for _, id := range request.DeviceIDs {
		if err := brokerAIDevice(ctx, claims.TenantID, id); err != nil {
			return brokeraitools.Issued{}, err
		}
	}
	return brokeraitools.New(global.REDIS).Issue(ctx, brokerAISubject(claims, token), request.DeviceIDs, request.Scopes)
}

func (*Device) RevokeBrokerAIToolDelegation(ctx context.Context, token, id string) error {
	if ctx == nil {
		return brokeraitools.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	claims, err := brokerAIHumanClaims(ctx, token)
	if err != nil {
		return err
	}
	return brokeraitools.New(global.REDIS).Revoke(ctx, brokerAISubject(claims, token), id)
}

func validateBrokerAIToolInput(operation string, input BrokerAIToolInput) error {
	if input.DeviceID == "" || len(input.DeviceID) > 36 {
		return brokeraitools.ErrInvalid
	}
	switch operation {
	case brokeraitools.Describe:
		if input.Window != nil || input.Command != nil || input.MessageID != "" {
			return brokeraitools.ErrInvalid
		}
	case brokeraitools.Telemetry:
		if input.Window == nil || input.Command != nil || input.MessageID != "" {
			return brokeraitools.ErrInvalid
		}
		if err := validateTelemetryEvidenceRequest(TelemetryEvidenceRequest{Start: input.Window.Start, End: input.Window.End, Keys: input.Window.Keys, Limit: input.Window.Limit}); err != nil {
			return err
		}
	case brokeraitools.Preview:
		if input.Window != nil || input.Command == nil || input.MessageID != "" || input.Command.DeviceID != input.DeviceID {
			return brokeraitools.ErrInvalid
		}
	case brokeraitools.Result:
		if input.Window != nil || input.Command != nil {
			return brokeraitools.ErrInvalid
		}
		id, err := uuid.Parse(input.MessageID)
		if err != nil || id.String() != input.MessageID {
			return brokeraitools.ErrInvalid
		}
	default:
		return brokeraitools.ErrUnauthorized
	}
	return nil
}

// ExecuteBrokerAITool owns the restricted delegation boundary, not the model
// runtime. Credentials never enter model arguments or returned tool data.
func (d *Device) ExecuteBrokerAITool(ctx context.Context, token, operation string, input BrokerAIToolInput) (BrokerAIToolOutput, error) {
	if ctx == nil {
		return BrokerAIToolOutput{}, brokeraitools.ErrInvalid
	}
	if !brokerAIToolsAvailable() {
		return BrokerAIToolOutput{}, ErrBrokerSemanticUnsupported
	}
	if global.REDIS == nil || global.DB == nil {
		return BrokerAIToolOutput{}, brokeraitools.ErrUnavailable
	}
	if err := validateBrokerAIToolInput(operation, input); err != nil {
		return BrokerAIToolOutput{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	credential, err := brokeraitools.New(global.REDIS).Consume(ctx, token, operation, input.DeviceID)
	if err != nil {
		return BrokerAIToolOutput{}, err
	}
	parent := credential.Subject.SessionToken
	claims, err := brokerAIHumanClaims(ctx, parent)
	if err != nil {
		return BrokerAIToolOutput{}, err
	}
	if claims.ID != credential.Subject.UserID || claims.TenantID != credential.Subject.TenantID || claims.Authority != credential.Subject.Authority {
		return BrokerAIToolOutput{}, brokeraitools.ErrUnauthorized
	}
	if err := brokerAIToolPermission(claims, operation); err != nil {
		return BrokerAIToolOutput{}, err
	}
	if err := brokerAIDevice(ctx, claims.TenantID, input.DeviceID); err != nil {
		return BrokerAIToolOutput{}, err
	}
	var data any
	switch operation {
	case brokeraitools.Describe:
		data, err = d.BrokerSemanticEvidence(ctx, claims, parent, input.DeviceID)
	case brokeraitools.Telemetry:
		w := input.Window
		data, err = d.BrokerTelemetryEvidence(ctx, claims, parent, input.DeviceID, TelemetryEvidenceRequest{Start: w.Start, End: w.End, Keys: w.Keys, Limit: w.Limit})
	case brokeraitools.Preview:
		data, err = GroupApp.CommandData.PreviewCommandPlan(ctx, claims, parent, input.Command)
	case brokeraitools.Result:
		var command brokercommands.Row
		command, err = brokercommands.New(global.DB).GetScoped(ctx, claims.TenantID, input.MessageID)
		if err == nil {
			if command.DeviceID != input.DeviceID || command.CallerID != claims.ID {
				return BrokerAIToolOutput{}, brokeraitools.ErrUnauthorized
			}
			data = struct {
				MessageID string    `json:"message_id"`
				Status    string    `json:"status"`
				CreatedAt time.Time `json:"created_at"`
			}{command.ID, command.State, command.CreatedAt}
		}
	}
	if err != nil {
		return BrokerAIToolOutput{}, err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return BrokerAIToolOutput{}, brokeraitools.ErrUnavailable
	}
	if len(raw) > MaxBrokerAIToolResponseBytes {
		return BrokerAIToolOutput{}, brokeraitools.ErrBudget
	}
	// Even a misconfigured trusted upstream must not echo a credential into
	// a model-visible response. Do not truncate or log the offending bytes.
	if bytes.Contains(raw, []byte(parent)) || bytes.Contains(raw, []byte(token)) {
		return BrokerAIToolOutput{}, brokeraitools.ErrUnavailable
	}
	// RawMessage fields may preserve upstream JSON unicode escapes. Inspect
	// decoded strings too, so an escaped credential cannot reach the model.
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil {
		return BrokerAIToolOutput{}, brokeraitools.ErrUnavailable
	}
	normalized, err := json.Marshal(decoded)
	if err != nil || bytes.Contains(normalized, []byte(parent)) || bytes.Contains(normalized, []byte(token)) {
		return BrokerAIToolOutput{}, brokeraitools.ErrUnavailable
	}
	output := BrokerAIToolOutput{Operation: operation, DeviceID: input.DeviceID, TenantID: claims.TenantID, SubjectID: claims.ID, Data: raw}
	// The transport cap covers the complete response, including the identity
	// binding, rather than only its inner business data.
	encoded, err := json.Marshal(output)
	if err != nil {
		return BrokerAIToolOutput{}, brokeraitools.ErrUnavailable
	}
	if len(encoded) > MaxBrokerAIToolResponseBytes {
		return BrokerAIToolOutput{}, brokeraitools.ErrBudget
	}
	return output, nil
}
