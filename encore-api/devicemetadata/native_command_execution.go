package devicemetadata

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"encore.app/internal/authz"
	"encore.app/internal/communityclient"
	"encore.app/internal/nativehttp"
	"encore.app/l1-access/authn"
	"encore.app/thingmodel/core"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// Device commands (submit, list, get). A submission is validated against the
// device's pinned ThingModel snapshot, recorded natively, and only then handed to the
// Community runtime. Delivery states come from the runtime; nothing here reports a
// command as delivered or acknowledged on its own authority.

const expectedMessagePrefix = "expected:"

const (
	commandPendingUnknownAfter = 2 * time.Minute
	commandRefreshLimit        = 20
	commandReasonMaxRunes      = 500
)

type NativeCommandReceipt struct {
	ID                     string          `json:"id"`
	DeviceID               string          `json:"deviceId"`
	State                  string          `json:"state"`
	AcceptedAt             string          `json:"acceptedAt"`
	DeliveryAt             *string         `json:"deliveryAt"`
	AcknowledgedAt         *string         `json:"acknowledgedAt"`
	Reason                 *string         `json:"reason"`
	ExpiresAt              *string         `json:"expiresAt"`
	Identifier             string          `json:"identifier"`
	SnapshotID             string          `json:"snapshotId"`
	DeliveryMode           string          `json:"deliveryMode"`
	Label                  *string         `json:"label"`
	Parameters             json.RawMessage `json:"parameters"`
	RedactedParameterPaths []string        `json:"redactedParameterPaths"`
}

type NativeCommandReceiptResponse struct {
	Data NativeCommandReceipt `json:"data"`
	Meta RequestMeta          `json:"meta"`
}

type NativeCommandReceiptPage struct {
	Items    []NativeCommandReceipt `json:"items"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"pageSize"`
	Total    int64                  `json:"total"`
}

type NativeCommandReceiptPageResponse struct {
	Data NativeCommandReceiptPage `json:"data"`
	Meta RequestMeta              `json:"meta"`
}

// ---- submission input ----

type nativeCommandSubmission struct {
	Identifier     string
	SnapshotID     string
	Parameters     json.RawMessage
	TimeoutSeconds int
	Label          *string
	DeliveryMode   string
	ExpiresAt      *time.Time
}

var nativeCommandSubmissionKeys = map[string]bool{
	"identifier": true, "snapshotId": true, "parameters": true, "timeoutSeconds": true,
	"deliveryMode": true, "expiresAt": true, "label": true,
}

func decodeNativeCommandSubmission(body []byte) (nativeCommandSubmission, error) {
	out := nativeCommandSubmission{TimeoutSeconds: 30, DeliveryMode: "immediate"}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return out, invalidStructure("request body must be a JSON object")
	}
	for key := range raw {
		if !nativeCommandSubmissionKeys[key] {
			return out, invalid(key, "unknown field")
		}
	}
	required := func(key string) (json.RawMessage, error) {
		value, ok := raw[key]
		if !ok || strings.TrimSpace(string(value)) == "null" {
			return nil, invalid(key, "field is required")
		}
		return value, nil
	}
	value, err := required("identifier")
	if err != nil {
		return out, err
	}
	if json.Unmarshal(value, &out.Identifier) != nil || out.Identifier == "" {
		return out, invalid("identifier", "identifier must be a non-empty string")
	}
	value, err = required("snapshotId")
	if err != nil {
		return out, err
	}
	var snapshot string
	parsed, parseErr := uuid.Parse("")
	if json.Unmarshal(value, &snapshot) != nil {
		return out, invalid("snapshotId", "snapshotId must be a UUID")
	}
	parsed, parseErr = uuid.Parse(snapshot)
	if parseErr != nil || len(snapshot) != 36 || parsed == uuid.Nil {
		return out, invalid("snapshotId", "snapshotId must be a UUID")
	}
	out.SnapshotID = parsed.String()
	value, err = required("parameters")
	if err != nil {
		return out, err
	}
	var parameters map[string]json.RawMessage
	if json.Unmarshal(value, &parameters) != nil || parameters == nil {
		return out, invalid("parameters", "parameters must be a JSON object")
	}
	out.Parameters = append(json.RawMessage(nil), value...)
	if value, ok := raw["timeoutSeconds"]; ok {
		var seconds int
		if json.Unmarshal(value, &seconds) != nil || seconds < 1 || seconds > 300 {
			return out, invalid("timeoutSeconds", "timeoutSeconds must be an integer from 1 to 300")
		}
		out.TimeoutSeconds = seconds
	}
	if value, ok := raw["deliveryMode"]; ok {
		var mode string
		if json.Unmarshal(value, &mode) != nil || (mode != "immediate" && mode != "when_online") {
			return out, invalid("deliveryMode", "deliveryMode must be immediate or when_online")
		}
		out.DeliveryMode = mode
	}
	if value, ok := raw["expiresAt"]; ok && strings.TrimSpace(string(value)) != "null" {
		if out.DeliveryMode != "when_online" {
			return out, invalid("expiresAt", "expiresAt is only accepted with when_online delivery")
		}
		var text string
		var parsed time.Time
		var parseErr error
		if json.Unmarshal(value, &text) != nil {
			return out, invalid("expiresAt", "expiresAt must be an RFC 3339 timestamp")
		}
		parsed, parseErr = time.Parse(time.RFC3339, text)
		if parseErr != nil || !parsed.After(time.Now()) {
			return out, invalid("expiresAt", "expiresAt must be a future RFC 3339 timestamp")
		}
		out.ExpiresAt = &parsed
	}
	if value, ok := raw["label"]; ok && strings.TrimSpace(string(value)) != "null" {
		var label string
		if json.Unmarshal(value, &label) != nil || utf8.RuneCountInString(label) > 100 {
			return out, invalid("label", "label must be a string of at most 100 characters")
		}
		out.Label = &label
	}
	return out, nil
}

// validateNativeCommandParameters checks the submitted parameters against the pinned
// COMMAND field: every declared parameter is required, undeclared ones are refused.
func validateNativeCommandParameters(field core.Field, parameters json.RawMessage) error {
	var values map[string]json.RawMessage
	if json.Unmarshal(parameters, &values) != nil || values == nil {
		return invalid("parameters", "parameters must be a JSON object")
	}
	declared := map[string]core.Parameter{}
	if field.Parameters != nil {
		for _, parameter := range field.Parameters.Items {
			declared[parameter.Identifier] = parameter
		}
	}
	supplied := make([]string, 0, len(values))
	for key := range values {
		supplied = append(supplied, key)
	}
	sort.Strings(supplied)
	for _, key := range supplied {
		if _, ok := declared[key]; !ok {
			return invalid("parameters."+key, "parameter is not declared by this command")
		}
	}
	names := make([]string, 0, len(declared))
	for key := range declared {
		names = append(names, key)
	}
	sort.Strings(names)
	for _, key := range names {
		raw, ok := values[key]
		if !ok {
			return invalid("parameters."+key, "parameter is required")
		}
		if err := core.ValidateScalar(declared[key].ValueType, raw); err != nil {
			return invalid("parameters."+key, err.Error())
		}
	}
	return nil
}

// runtimeParametersText is the value text sent to the runtime: no params for an
// empty object, otherwise the compact JSON of the accepted object.
func runtimeParametersText(parameters json.RawMessage) string {
	var values map[string]json.RawMessage
	if json.Unmarshal(parameters, &values) != nil || len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(string(parameters))
}

// ---- runtime state mapping ----

func commandStateRank(state string) int {
	switch state {
	case "pending":
		return 0
	case "accepted":
		return 1
	case "delivered":
		return 2
	}
	return 3
}

// mapRuntimeCommandStatus converts a Community command status to the contract state.
func mapRuntimeCommandStatus(status string) string {
	switch status {
	case "accepted":
		return "accepted"
	case "published":
		return "delivered"
	case "publish_failed", "device_failed":
		return "failed"
	case "device_succeeded":
		return "acknowledged"
	case "timeout":
		return "timed_out"
	}
	return "unknown"
}

// mapExpectedStatus converts a Community expected-message status (pending, sent,
// expired). "sent" means Community published it; it says nothing about the device.
func mapExpectedStatus(status string) string {
	switch status {
	case "pending":
		return "accepted"
	case "sent":
		return "delivered"
	case "expired":
		return "expired"
	}
	return "unknown"
}

func truncateReason(value string) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= commandReasonMaxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:commandReasonMaxRunes])
}

// ---- control identity ----

func currentNativeDeviceControlIdentity() (nativeDeviceWriteIdentity, error) {
	read, err := currentNativeDeviceReadIdentity()
	if err != nil {
		return nativeDeviceWriteIdentity{}, err
	}
	data := authn.CurrentData()
	if !authz.Can(data, "device", "control") {
		return nativeDeviceWriteIdentity{}, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to control devices"}
	}
	if !validAccessTemplateText(data.UserID, 1, 128, true) {
		return nativeDeviceWriteIdentity{}, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated subject"}
	}
	return nativeDeviceWriteIdentity{nativeDeviceReadIdentity: read, subjectID: data.UserID}, nil
}

func nativeCommandConflict(message string) error {
	return &errs.Error{Code: errs.Aborted, Message: message}
}

// ---- storage ----

const nativeCommandColumns = `id::text, device_id::text, state, accepted_at, delivery_at, acknowledged_at, reason, expires_at,
	identifier, snapshot_id::text, delivery_mode, label, parameters::text`

type nativeCommandScanner interface {
	Scan(dest ...any) error
}

func scanNativeCommandReceipt(row nativeCommandScanner) (NativeCommandReceipt, error) {
	var out NativeCommandReceipt
	var accepted time.Time
	var delivery, acknowledged, expires *time.Time
	var parameters string
	if err := row.Scan(&out.ID, &out.DeviceID, &out.State, &accepted, &delivery, &acknowledged, &out.Reason, &expires,
		&out.Identifier, &out.SnapshotID, &out.DeliveryMode, &out.Label, &parameters); err != nil {
		return out, err
	}
	out.AcceptedAt = rfc3339Millis(accepted)
	format := func(value *time.Time) *string {
		if value == nil {
			return nil
		}
		text := rfc3339Millis(*value)
		return &text
	}
	out.DeliveryAt, out.AcknowledgedAt, out.ExpiresAt = format(delivery), format(acknowledged), format(expires)
	out.Parameters = json.RawMessage(parameters)
	out.RedactedParameterPaths = []string{}
	return out, nil
}

func nativeCommandLocation(deviceID, executionID string) string {
	return "/api/v2/devices/" + deviceID + "/command-executions/" + executionID
}

// ---- submit ----

type nativeCommandAccepted struct {
	deliveryMode string
	expiresAt    *time.Time
	label        *string
	executionID  string
	runtimeID    string
	parameters   json.RawMessage
	identifier   string
}

func submitNativeDeviceCommand(
	ctx context.Context,
	identity nativeDeviceWriteIdentity,
	deviceID, key string,
	body []byte,
	requestID string,
) (*metadataWriteResponse, *nativeCommandAccepted, error) {
	if !identity.permits(deviceID) {
		return nil, nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	input, err := decodeNativeCommandSubmission(body)
	if err != nil {
		return nil, nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, nil, invalidStructure("request body must be a valid JSON object")
	}
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "submitDeviceCommand",
		Method: http.MethodPost, Path: "/api/v2/devices/" + deviceID + "/command-executions", Key: key,
		RequestID: requestID, CanonicalBody: canonical,
	}
	var accepted *nativeCommandAccepted
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		var lockedID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR SHARE`,
			identity.tenantID, deviceID).Scan(&lockedID); err != nil {
			if errors.Is(err, sqldb.ErrNoRows) {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "device not found"}
			}
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		device, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if device.AdministrativeState != "enabled" {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeCommandConflict("device is disabled; enable it before sending commands")
		}
		if device.ModelBinding == nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeCommandConflict("device has no pinned ThingModel snapshot to validate commands against")
		}
		if device.ModelBinding.SnapshotID != input.SnapshotID {
			return metadataWriteResponse{}, metadataWriteAudit{}, nativeCommandConflict("snapshotId is not the snapshot the device currently runs")
		}
		var content []byte
		if err := tx.QueryRow(ctx, `SELECT canonical_content FROM thing_model_snapshots WHERE tenant_id=$1 AND thing_model_id=$2 AND id=$3`,
			identity.tenantID, device.ModelBinding.ModelID, device.ModelBinding.SnapshotID).Scan(&content); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "pinned ThingModel snapshot could not be read"}
		}
		var model core.Model
		if json.Unmarshal(content, &model) != nil || model.Fields == nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "pinned ThingModel snapshot is invalid"}
		}
		var field *core.Field
		for i := range model.Fields {
			if model.Fields[i].Kind == core.FieldCommand && model.Fields[i].Identifier == input.Identifier {
				field = &model.Fields[i]
				break
			}
		}
		if field == nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, invalid("identifier", "identifier is not a COMMAND of the device's pinned snapshot")
		}
		if err := validateNativeCommandParameters(*field, input.Parameters); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		runtimeID, err := resolveRuntimeDeviceID(device.RuntimeRef, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		executionID := uuid.NewString()
		row := tx.QueryRow(ctx, `INSERT INTO device_command_executions
				(tenant_id,id,device_id,identifier,snapshot_id,parameters,label,delivery_mode,timeout_seconds,state,created_by,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$10::varchar,$8,'pending',$9,$11)
			RETURNING `+nativeCommandColumns,
			identity.tenantID, executionID, deviceID, input.Identifier, input.SnapshotID, []byte(input.Parameters),
			input.Label, input.TimeoutSeconds, identity.subjectID, input.DeliveryMode, input.ExpiresAt)
		receipt, err := scanNativeCommandReceipt(row)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		encoded, err := json.Marshal(NativeCommandReceiptResponse{Data: receipt, Meta: RequestMeta{RequestID: requestID}})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		accepted = &nativeCommandAccepted{deliveryMode: input.DeliveryMode, expiresAt: input.ExpiresAt, label: input.Label, executionID: executionID, runtimeID: runtimeID, parameters: input.Parameters, identifier: input.Identifier}
		changes, _ := json.Marshal(map[string]any{"commandExecutionId": executionID, "identifier": input.Identifier, "deliveryMode": input.DeliveryMode})
		version := device.ResourceVersion
		return metadataWriteResponse{Status: http.StatusAccepted, Body: encoded, Location: nativeCommandLocation(deviceID, executionID)},
			metadataWriteAudit{ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &version, AfterVersion: &version, RedactedChanges: changes}, nil
	})
	if err != nil {
		return nil, nil, mapNativeDeviceWriteError(err)
	}
	if result.Replayed {
		accepted = nil
	}
	return result, accepted, nil
}

// dispatchNativeCommand hands an accepted command to the runtime and records only what
// the runtime answered. A failure leaves a failed receipt; an unreachable runtime is
// never reported as delivered.
func dispatchNativeCommand(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID string, accepted *nativeCommandAccepted) error {
	var messageID string
	var submitErr error
	if accepted.deliveryMode == "when_online" {
		var record communityclient.ExpectedRecord
		record, submitErr = nativeRuntimeClient().CreateExpectedCommand(ctx, accepted.runtimeID, accepted.identifier, runtimeParametersText(accepted.parameters), accepted.expiresAt, accepted.label)
		messageID = expectedMessagePrefix + record.ID
	} else {
		// The execution id is the runtime idempotency key, so repeating the call after an
		// unclear failure cannot send the command twice.
		for attempt := 0; attempt < 2; attempt++ {
			messageID, submitErr = nativeRuntimeClient().SubmitCommand(ctx, accepted.runtimeID, accepted.identifier, runtimeParametersText(accepted.parameters), accepted.executionID)
			if submitErr == nil || errors.Is(submitErr, communityclient.ErrRejected) || ctx.Err() != nil {
				break
			}
		}
	}
	tx, err := beginTenantScopedSQLTx(context.WithoutCancel(ctx), identity.tenantID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	write := context.WithoutCancel(ctx)
	if submitErr != nil {
		// Only a definite refusal is a failure. When the runtime did not answer, the
		// command may or may not have been sent, so it is recorded as unknown.
		state, reason := "unknown", "the device runtime did not confirm whether it received the command"
		if errors.Is(submitErr, communityclient.ErrRejected) {
			state, reason = "failed", "the device runtime did not accept the command"
		}
		rlog.Warn("device command was not confirmed by the runtime", "device_id", deviceID, "execution_id", accepted.executionID, "state", state)
		if _, err := tx.Exec(write, `UPDATE device_command_executions SET state=$5::varchar,reason=$4,updated_at=NOW()
			WHERE tenant_id=$1 AND device_id=$2 AND id=$3 AND state='pending'`,
			identity.tenantID, deviceID, accepted.executionID, reason, state); err != nil {
			return err
		}
	} else if _, err := tx.Exec(write, `UPDATE device_command_executions SET state='accepted',runtime_message_id=$4,updated_at=NOW()
		WHERE tenant_id=$1 AND device_id=$2 AND id=$3 AND state='pending'`,
		identity.tenantID, deviceID, accepted.executionID, messageID); err != nil {
		return err
	}
	return tx.Commit()
}

// refreshNativeDeviceCommands advances recent non-terminal receipts from the runtime.
// States only move forward. Runtime errors leave the last known state untouched.
func refreshNativeDeviceCommands(ctx context.Context, identity nativeDeviceReadIdentity, deviceID, onlyID string) {
	tenantID := identity.tenantID
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, true)
	if err != nil {
		return
	}
	type candidate struct {
		id, state string
		messageID *string
		accepted  time.Time
	}
	var candidates []candidate
	query := `SELECT id::text,state,runtime_message_id,accepted_at FROM device_command_executions
		WHERE tenant_id=$1 AND device_id=$2 AND state IN ('pending','accepted','delivered')`
	args := []any{tenantID, deviceID}
	if onlyID != "" {
		query += ` AND id=$3`
		args = append(args, onlyID)
	}
	rows, err := tx.Query(ctx, query+` ORDER BY accepted_at DESC, id DESC LIMIT `+"20", args...)
	if err != nil {
		_ = tx.Rollback()
		return
	}
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.id, &c.state, &c.messageID, &c.accepted) == nil {
			candidates = append(candidates, c)
		}
	}
	rows.Close()
	_ = tx.Rollback()

	var expected map[string]string
	for _, c := range candidates {
		newState, reason := "", ""
		switch {
		case c.messageID == nil:
			if time.Since(c.accepted) < commandPendingUnknownAfter {
				continue
			}
			newState, reason = "unknown", "the submission outcome could not be confirmed"
		case strings.HasPrefix(*c.messageID, expectedMessagePrefix):
			if expected == nil {
				legacyID, err := loadRuntimeDeviceID(ctx, identity, deviceID)
				if err != nil {
					continue
				}
				rows, err := nativeRuntimeClient().ListExpectedCommands(ctx, legacyID, 1, 100)
				if err != nil {
					continue
				}
				expected = map[string]string{}
				for _, row := range rows {
					expected[row.ID] = row.Status
				}
			}
			status, found := expected[strings.TrimPrefix(*c.messageID, expectedMessagePrefix)]
			if !found {
				continue
			}
			newState = mapExpectedStatus(status)
		default:
			status, err := nativeRuntimeClient().GetCommandStatus(ctx, *c.messageID)
			if err != nil {
				continue
			}
			newState = mapRuntimeCommandStatus(status.Status)
			if status.ErrorMessage != nil {
				reason = truncateReason(*status.ErrorMessage)
			}
		}
		if newState == c.state || commandStateRank(newState) < commandStateRank(c.state) {
			continue
		}
		write, err := beginTenantScopedSQLTx(ctx, tenantID, false)
		if err != nil {
			return
		}
		var reasonValue *string
		if reason != "" {
			reasonValue = &reason
		}
		_, execErr := write.Exec(ctx, `UPDATE device_command_executions SET state=$4::varchar,reason=COALESCE($5,reason),
			delivery_at=CASE WHEN $4::text='delivered' AND delivery_at IS NULL THEN NOW() ELSE delivery_at END,
			acknowledged_at=CASE WHEN $4::text='acknowledged' AND acknowledged_at IS NULL THEN NOW() ELSE acknowledged_at END,
			updated_at=NOW()
			WHERE tenant_id=$1 AND device_id=$2 AND id=$3 AND state=$6`, tenantID, deviceID, c.id, newState, reasonValue, c.state)
		if execErr != nil || write.Commit() != nil {
			_ = write.Rollback()
		}
	}
}

// ---- read ----

type nativeCommandListQuery struct {
	page, pageSize int
	state          string
	deliveryMode   string
	identifier     string
	from, to       *time.Time
	labelContains  string
}

var nativeCommandStates = map[string]bool{"pending": true, "accepted": true, "delivered": true, "acknowledged": true,
	"failed": true, "timed_out": true, "expired": true, "cancelled": true, "unknown": true}

var nativeCommandListKeys = []string{"page", "pageSize", "state", "deliveryMode", "identifier", "from", "to", "labelContains"}

func decodeNativeCommandListQuery(values url.Values) (nativeCommandListQuery, error) {
	var out nativeCommandListQuery
	var err error
	out.page, out.pageSize, err = normalizeDeviceGroupPagination(values.Get("page"), values.Get("pageSize"))
	if err != nil {
		return out, err
	}
	if values.Has("state") {
		out.state = values.Get("state")
		if !nativeCommandStates[out.state] {
			return out, invalidGroupQuery("invalid state")
		}
	}
	if values.Has("deliveryMode") {
		out.deliveryMode = values.Get("deliveryMode")
		if out.deliveryMode != "immediate" && out.deliveryMode != "when_online" {
			return out, invalidGroupQuery("deliveryMode must be immediate or when_online")
		}
	}
	if values.Has("identifier") {
		out.identifier = values.Get("identifier")
		if out.identifier == "" {
			return out, invalidGroupQuery("identifier must not be empty")
		}
	}
	for _, name := range []string{"from", "to"} {
		if !values.Has(name) {
			continue
		}
		parsed, parseErr := time.Parse(time.RFC3339, values.Get(name))
		if parseErr != nil {
			return out, invalidGroupQuery(name + " must be an RFC 3339 timestamp")
		}
		if name == "from" {
			out.from = &parsed
		} else {
			out.to = &parsed
		}
	}
	if out.from != nil && out.to != nil && out.to.Before(*out.from) {
		return out, invalidGroupQuery("to must not be before from")
	}
	if values.Has("labelContains") {
		out.labelContains = values.Get("labelContains")
		if out.labelContains == "" || utf8.RuneCountInString(out.labelContains) > 100 {
			return out, invalidGroupQuery("invalid labelContains")
		}
	}
	return out, nil
}

func listNativeDeviceCommands(ctx context.Context, identity nativeDeviceReadIdentity, query nativeCommandListQuery, deviceID, requestID string) (*NativeCommandReceiptPageResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	if err := requireNativeCommandDevice(ctx, identity.tenantID, deviceID); err != nil {
		return nil, err
	}
	refreshNativeDeviceCommands(ctx, identity, deviceID, "")
	tx, err := beginTenantScopedSQLTx(ctx, identity.tenantID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	where := ` WHERE tenant_id=$1 AND device_id=$2`
	args := []any{identity.tenantID, deviceID}
	add := func(clause string, value any) {
		args = append(args, value)
		where += strings.Replace(clause, "?", "$"+strconv.Itoa(len(args)), 1)
	}
	if query.state != "" {
		add(` AND state=?`, query.state)
	}
	if query.deliveryMode != "" {
		add(` AND delivery_mode=?`, query.deliveryMode)
	}
	if query.identifier != "" {
		add(` AND identifier=?`, query.identifier)
	}
	if query.from != nil {
		add(` AND accepted_at>=?`, *query.from)
	}
	if query.to != nil {
		add(` AND accepted_at<=?`, *query.to)
	}
	if query.labelContains != "" {
		add(` AND position(? in label)>0`, query.labelContains)
	}
	page := NativeCommandReceiptPage{Items: []NativeCommandReceipt{}, Page: query.page, PageSize: query.pageSize}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM device_command_executions`+where, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	args = append(args, query.pageSize, (query.page-1)*query.pageSize)
	rows, err := tx.Query(ctx, `SELECT `+nativeCommandColumns+` FROM device_command_executions`+where+
		` ORDER BY accepted_at DESC, id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		receipt, err := scanNativeCommandReceipt(rows)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, receipt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &NativeCommandReceiptPageResponse{Data: page, Meta: RequestMeta{RequestID: requestID}}, nil
}

func getNativeDeviceCommand(ctx context.Context, identity nativeDeviceReadIdentity, deviceID, executionID, requestID string) (*NativeCommandReceiptResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	refreshNativeDeviceCommands(ctx, identity, deviceID, executionID)
	tx, err := beginTenantScopedSQLTx(ctx, identity.tenantID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	receipt, err := scanNativeCommandReceipt(tx.QueryRow(ctx, `SELECT `+nativeCommandColumns+` FROM device_command_executions
		WHERE tenant_id=$1 AND device_id=$2 AND id=$3`, identity.tenantID, deviceID, executionID))
	if err != nil {
		if errors.Is(err, sqldb.ErrNoRows) {
			return nil, &errs.Error{Code: errs.NotFound, Message: "command execution not found"}
		}
		return nil, err
	}
	return &NativeCommandReceiptResponse{Data: receipt, Meta: RequestMeta{RequestID: requestID}}, nil
}

// requireNativeCommandDevice returns 404 for a device that does not exist in the tenant.
func requireNativeCommandDevice(ctx context.Context, tenantID, deviceID string) error {
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, deviceID).Scan(&id); err != nil {
		if errors.Is(err, sqldb.ErrNoRows) {
			return &errs.Error{Code: errs.NotFound, Message: "device not found"}
		}
		return err
	}
	return nil
}

// ---- endpoints ----

//encore:api auth raw method=POST path=/api/v2/devices/:deviceID/command-executions
func (s *Service) SubmitNativeDeviceCommand(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceControlIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	body, err := nativeDeviceWriteBody(w, r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, accepted, err := submitNativeDeviceCommand(r.Context(), identity, deviceID, key, body, requestID)
	if err != nil {
		writeNativeDeviceOperationError(w, r, requestID, err)
		return
	}
	if accepted != nil {
		if err := dispatchNativeCommand(r.Context(), identity, deviceID, accepted); err != nil {
			writeNativeDeviceOperationError(w, r, requestID, err)
			return
		}
		fresh, err := getNativeDeviceCommand(r.Context(), identity.nativeDeviceReadIdentity, deviceID, accepted.executionID, requestID)
		if err != nil {
			writeNativeDeviceOperationError(w, r, requestID, err)
			return
		}
		encoded, err := json.Marshal(fresh)
		if err != nil {
			writeNativeDeviceOperationError(w, r, requestID, err)
			return
		}
		result = &metadataWriteResponse{Status: http.StatusAccepted, Body: encoded, Location: result.Location, RequestID: requestID}
	}
	writeNativeDeviceMutationResponse(w, result)
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/command-executions
func (s *Service) ListNativeDeviceCommands(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceControlIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	values, err := parseNativeReadValues(r, nativeCommandListKeys)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	query, err := decodeNativeCommandListQuery(values)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	response, err := listNativeDeviceCommands(r.Context(), identity.nativeDeviceReadIdentity, query, deviceID, requestID)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/command-executions/:executionID
func (s *Service) GetNativeDeviceCommand(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceControlIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	executionID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("executionID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	response, err := getNativeDeviceCommand(r.Context(), identity.nativeDeviceReadIdentity, deviceID, executionID, requestID)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}
