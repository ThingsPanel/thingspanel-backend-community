package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"project/internal/brokercontract"
	"project/internal/brokersemantic"
	"project/internal/brokerstate"
	"project/pkg/global"
	"project/pkg/utils"
)

var (
	ErrTelemetryEvidenceInvalid     = errors.New("telemetry evidence request invalid")
	ErrTelemetryEvidenceUnsupported = errors.New("telemetry evidence unsupported")
	ErrTelemetryEvidenceUnavailable = errors.New("telemetry evidence unavailable")
)

const (
	maxTelemetryEvidenceWindow = 24 * time.Hour
	maxTelemetryEvidenceKeys   = 8
	maxTelemetryEvidencePoints = 200
	maxTelemetryReceiptScan    = 201
	maxTelemetryReceiptBytes   = 16 << 20
)

type TelemetryEvidenceRequest struct {
	Start time.Time
	End   time.Time
	Keys  []string
	Limit int
}

type TelemetryMetricDefinition struct {
	Identifier string `json:"identifier"`
	Kind       string `json:"kind"`
	Unit       string `json:"unit,omitempty"`
	UnitKnown  bool   `json:"unit_known"`
}

type TelemetryEvidencePoint struct {
	Key                   string          `json:"key"`
	Value                 json.RawMessage `json:"value"`
	Kind                  string          `json:"kind"`
	Unit                  string          `json:"unit,omitempty"`
	UnitKnown             bool            `json:"unit_known"`
	ReportedAt            *time.Time      `json:"reported_at"`
	ReportedTimeAvailable bool            `json:"reported_time_available"`
	TimestampBasis        string          `json:"timestamp_basis"`
	ObservedAt            time.Time       `json:"observed_at"`
	ProcessedAt           time.Time       `json:"processed_at"`
	ReceiptID             string          `json:"receipt_id"`
	TransportDeviceID     string          `json:"transport_device_id"`
	BrokerCell            string          `json:"broker_cell"`
	RouteEpoch            uint64          `json:"route_epoch"`
	Quality               string          `json:"quality"`
	Source                string          `json:"source"`
}

type TelemetryEvidence struct {
	DeviceID     string                   `json:"device_id"`
	TenantID     string                   `json:"tenant_id"`
	SemanticPins brokersemantic.Pins      `json:"semantic_pins"`
	State        brokerstate.Evidence     `json:"state"`
	Start        time.Time                `json:"start"`
	End          time.Time                `json:"end"`
	WindowBasis  string                   `json:"window_basis"`
	Items        []TelemetryEvidencePoint `json:"items"`
	Truncated    bool                     `json:"truncated"`
}

type telemetryEvidenceReceipt struct {
	ID              string
	DeviceID        string
	BrokerCell      string
	RouteEpoch      int64
	Payload         []byte
	CanonicalSHA256 []byte
	Metadata        []byte
	CreatedAt       time.Time
	ProcessedAt     *time.Time
	ScanTruncated   bool
}

type telemetryReceiptMetadata struct {
	Source          string `json:"source"`
	Timestamp       int64  `json:"timestamp"`
	LogicalDeviceID string `json:"logical_device_id"`
}

type telemetrySemanticModel struct {
	Fields []telemetrySemanticField `json:"fields"`
}
type telemetrySemanticField struct {
	Kind       string                   `json:"kind"`
	Identifier string                   `json:"identifier"`
	Unit       string                   `json:"unit"`
	Access     *telemetrySemanticAccess `json:"access"`
	ValueType  *telemetrySemanticType   `json:"valueType"`
}
type telemetrySemanticAccess struct {
	Read    *bool `json:"read"`
	Observe *bool `json:"observe"`
}
type telemetrySemanticType struct {
	Kind     string `json:"kind"`
	BaseKind string `json:"baseKind"`
}

// BrokerTelemetryEvidence reuses the authenticated semantic evidence path once,
// then returns only telemetry receipts accepted by the trusted batch writer.
func (d *Device) BrokerTelemetryEvidence(ctx context.Context, claims *utils.UserClaims, sessionToken, deviceID string, req TelemetryEvidenceRequest) (TelemetryEvidence, error) {
	if ctx == nil || claims == nil || strings.TrimSpace(deviceID) == "" || global.DB == nil {
		return TelemetryEvidence{}, ErrBrokerSemanticUnauthorized
	}
	if err := validateTelemetryEvidenceRequest(req); err != nil {
		return TelemetryEvidence{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	semanticEvidence, err := d.BrokerSemanticEvidence(ctx, claims, sessionToken, deviceID)
	if err != nil {
		return TelemetryEvidence{}, err
	}
	definitions, err := telemetryMetricDefinitions(semanticEvidence.Description, req.Keys)
	if err != nil {
		return TelemetryEvidence{}, err
	}
	for _, key := range req.Keys {
		if _, ok := definitions[key]; !ok {
			return TelemetryEvidence{}, ErrTelemetryEvidenceUnsupported
		}
	}
	items, truncated, err := queryTrustedTelemetryEvidence(ctx, global.DB, claims.TenantID, deviceID, req, definitions)
	if err != nil {
		return TelemetryEvidence{}, err
	}
	return TelemetryEvidence{
		DeviceID: deviceID, TenantID: claims.TenantID, SemanticPins: semanticEvidence.Description.Pins,
		State: semanticEvidence.State, Start: req.Start.UTC(), End: req.End.UTC(),
		WindowBasis: "observed_at", Items: items, Truncated: truncated,
	}, nil
}

func validateTelemetryEvidenceRequest(req TelemetryEvidenceRequest) error {
	if req.Start.IsZero() || req.End.IsZero() || !req.Start.Before(req.End) || req.End.Sub(req.Start) > maxTelemetryEvidenceWindow || req.Limit < 1 || req.Limit > maxTelemetryEvidencePoints || len(req.Keys) < 1 || len(req.Keys) > maxTelemetryEvidenceKeys {
		return ErrTelemetryEvidenceInvalid
	}
	seen := map[string]bool{}
	for _, key := range req.Keys {
		if key == "" || strings.TrimSpace(key) != key || len(key) > 255 || seen[key] {
			return ErrTelemetryEvidenceInvalid
		}
		seen[key] = true
	}
	return nil
}

func ValidateTelemetryEvidenceRequest(req TelemetryEvidenceRequest) error {
	return validateTelemetryEvidenceRequest(req)
}

func telemetryMetricDefinitions(description brokersemantic.Description, requested []string) (map[string]TelemetryMetricDefinition, error) {
	if len(description.Model) == 0 || brokercontract.ValidateUniqueJSON(description.Model) != nil {
		return nil, ErrTelemetryEvidenceUnsupported
	}
	var model telemetrySemanticModel
	if err := json.Unmarshal(description.Model, &model); err != nil || model.Fields == nil {
		return nil, ErrTelemetryEvidenceUnsupported
	}
	defs := make(map[string]TelemetryMetricDefinition, len(requested))
	for _, key := range requested {
		var selected *telemetrySemanticField
		for i := range model.Fields {
			if model.Fields[i].Identifier != key {
				continue
			}
			if selected != nil {
				return nil, ErrTelemetryEvidenceUnsupported
			}
			selected = &model.Fields[i]
		}
		if selected == nil || selected.Kind != "TELEMETRY" || selected.Access == nil || selected.Access.Read == nil || selected.Access.Observe == nil || !*selected.Access.Read || !*selected.Access.Observe || selected.ValueType == nil {
			return nil, ErrTelemetryEvidenceUnsupported
		}
		kind := strings.ToUpper(selected.ValueType.Kind)
		switch kind {
		case "INT", "FLOAT", "NUMBER", "BOOL", "STRING":
		default:
			return nil, ErrTelemetryEvidenceUnsupported
		}
		defs[key] = TelemetryMetricDefinition{Identifier: key, Kind: kind, Unit: selected.Unit, UnitKnown: selected.Unit != ""}
	}
	return defs, nil
}

func queryTrustedTelemetryEvidence(ctx context.Context, db *gorm.DB, tenantID, logicalDeviceID string, req TelemetryEvidenceRequest, defs map[string]TelemetryMetricDefinition) ([]TelemetryEvidencePoint, bool, error) {
	if db == nil || tenantID == "" || logicalDeviceID == "" {
		return nil, false, ErrTelemetryEvidenceUnavailable
	}
	rows := make([]telemetryEvidenceReceipt, 0, maxTelemetryReceiptScan)
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout = '2s'").Error; err != nil {
			return err
		}
		return tx.Raw(`WITH candidates AS (
 SELECT id,device_id,broker_cell,route_epoch,payload,canonical_sha256,metadata,created_at,processed_at,
        sum(octet_length(payload)) OVER (ORDER BY created_at,id ROWS UNBOUNDED PRECEDING) AS payload_bytes
 FROM (
  SELECT id,device_id,broker_cell,route_epoch,payload,canonical_sha256,metadata,created_at,processed_at
  FROM broker_ingress_receipts
  WHERE tenant_id=? AND stream='telemetry_sample' AND state='processed'
    AND metadata->>'source'='trusted_telemetry_batch'
    AND metadata->>'logical_device_id'=? AND created_at>=? AND created_at<?
  ORDER BY created_at,id LIMIT ?
 ) bounded_by_receipt_count
), bounded AS (
 SELECT candidates.*,
        (SELECT count(*) FROM candidates) >
        (SELECT count(*) FROM candidates WHERE payload_bytes<=?) AS scan_truncated
 FROM candidates WHERE payload_bytes<=?
)
SELECT id,device_id,broker_cell,route_epoch,payload,canonical_sha256,metadata,created_at,processed_at,scan_truncated
FROM bounded ORDER BY created_at,id`, tenantID, logicalDeviceID, req.Start.UTC(), req.End.UTC(), maxTelemetryReceiptScan, maxTelemetryReceiptBytes, maxTelemetryReceiptBytes).Scan(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, false, ErrTelemetryEvidenceUnavailable
	}
	truncated := len(rows) > maxTelemetryEvidencePoints
	if len(rows) > 0 && rows[0].ScanTruncated {
		truncated = true
	}
	if len(rows) > maxTelemetryEvidencePoints {
		rows = rows[:maxTelemetryEvidencePoints]
	}
	items := make([]TelemetryEvidencePoint, 0, req.Limit)
	for _, row := range rows {
		if len(items) >= req.Limit {
			truncated = true
			break
		}
		if row.ID == "" || row.DeviceID == "" || row.BrokerCell == "" || row.RouteEpoch < 1 || row.ProcessedAt == nil || row.ProcessedAt.IsZero() {
			return nil, false, ErrTelemetryEvidenceUnavailable
		}
		var meta telemetryReceiptMetadata
		if brokercontract.ValidateUniqueJSON(row.Metadata) != nil || json.Unmarshal(row.Metadata, &meta) != nil || meta.Source != "trusted_telemetry_batch" || meta.LogicalDeviceID != logicalDeviceID || meta.Timestamp <= 0 {
			return nil, false, ErrTelemetryEvidenceUnavailable
		}
		if row.CreatedAt.IsZero() {
			return nil, false, ErrTelemetryEvidenceUnavailable
		}
		if row.CreatedAt.Before(req.Start) || !row.CreatedAt.Before(req.End) {
			continue
		}
		// The current trusted writer stamps metadata.timestamp from the broker
		// observation clock. It is not a device-reported timestamp.
		if meta.Timestamp != row.CreatedAt.UnixMilli() {
			return nil, false, ErrTelemetryEvidenceUnavailable
		}
		if brokercontract.ValidateUniqueJSON(row.Payload) != nil {
			return nil, false, ErrTelemetryEvidenceUnavailable
		}
		payloadDigest := sha256.Sum256(row.Payload)
		if len(row.CanonicalSHA256) != len(payloadDigest) || !bytes.Equal(row.CanonicalSHA256, payloadDigest[:]) {
			return nil, false, ErrTelemetryEvidenceUnavailable
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(row.Payload, &payload); err != nil || payload == nil {
			return nil, false, ErrTelemetryEvidenceUnavailable
		}
		for _, key := range req.Keys {
			raw, exists := payload[key]
			if !exists {
				continue
			}
			def := defs[key]
			if !telemetryValueMatches(def.Kind, raw) {
				return nil, false, ErrTelemetryEvidenceUnavailable
			}
			if len(items) >= req.Limit {
				truncated = true
				break
			}
			items = append(items, TelemetryEvidencePoint{Key: key, Value: append(json.RawMessage(nil), raw...), Kind: def.Kind, Unit: def.Unit, UnitKnown: def.UnitKnown, ReportedAt: nil, ReportedTimeAvailable: false, TimestampBasis: "broker_observed", ObservedAt: row.CreatedAt.UTC(), ProcessedAt: row.ProcessedAt.UTC(), ReceiptID: row.ID, TransportDeviceID: row.DeviceID, BrokerCell: row.BrokerCell, RouteEpoch: uint64(row.RouteEpoch), Quality: "trusted_processed", Source: meta.Source})
		}
	}
	return items, truncated, nil
}

func telemetryValueMatches(kind string, raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return false
	}
	switch kind {
	case "INT":
		if !startsWithJSONNumber(s) {
			return false
		}
		var n json.Number
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		return dec.Decode(&n) == nil && strings.ContainsAny(n.String(), ".eE") == false && func() bool { _, err := strconv.ParseInt(n.String(), 10, 64); return err == nil }()
	case "FLOAT", "NUMBER":
		if !startsWithJSONNumber(s) {
			return false
		}
		var n json.Number
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		if dec.Decode(&n) != nil {
			return false
		}
		f, err := n.Float64()
		return err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
	case "BOOL":
		return s == "true" || s == "false"
	case "STRING":
		var v string
		return json.Unmarshal(raw, &v) == nil
	default:
		return false
	}
}

func startsWithJSONNumber(raw string) bool {
	if raw == "" {
		return false
	}
	return raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')
}
