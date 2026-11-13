package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"encore.app/internal/tenantidentity"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

type pageRequest struct {
	Page     int `query:"page"`
	PageSize int `query:"page_size"`
}

func (r *pageRequest) normalize() {
	if r.Page < 1 {
		r.Page = 1
	}
	if r.PageSize < 1 {
		r.PageSize = 20
	}
	if r.PageSize > 200 {
		r.PageSize = 200
	}
}

type listResponse[T any] struct {
	Items    []T `json:"items"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

type HealthzResponse struct {
	Status string `json:"status"`
}

type ThingModel struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenant_id"`
	Name              string          `json:"name"`
	DescriptionI18n   json.RawMessage `json:"description_i18n,omitempty"`
	Category          string          `json:"category,omitempty"`
	Status            string          `json:"status"`
	CurrentSnapshotID *string         `json:"current_snapshot_id,omitempty"`
	WebChartConfig    json.RawMessage `json:"web_chart_config,omitempty"`
	AppChartConfig    json.RawMessage `json:"app_chart_config,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type ThingModelItem struct {
	ID              string          `json:"id"`
	ThingModelID    string          `json:"thing_model_id"`
	TenantID        string          `json:"tenant_id"`
	Type            string          `json:"type"`
	Identifier      string          `json:"identifier"`
	NameI18n        json.RawMessage `json:"name_i18n"`
	DescriptionI18n json.RawMessage `json:"description_i18n,omitempty"`
	ValueType       json.RawMessage `json:"value_type,omitempty"`
	Parameters      json.RawMessage `json:"parameters,omitempty"`
	Access          json.RawMessage `json:"access"`
	WebChartConfig  json.RawMessage `json:"web_chart_config,omitempty"`
	AppChartConfig  json.RawMessage `json:"app_chart_config,omitempty"`
	MetaItems       json.RawMessage `json:"meta_items,omitempty"`
	SortOrder       int             `json:"sort_order"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type ThingModelSnapshot struct {
	ID           string          `json:"id"`
	ThingModelID string          `json:"thing_model_id"`
	TenantID     string          `json:"tenant_id"`
	Version      int             `json:"version"`
	Content      json.RawMessage `json:"content"`
	Changelog    string          `json:"changelog,omitempty"`
	PublishedAt  time.Time       `json:"published_at"`
	PublishedBy  string          `json:"published_by"`
}

type DeviceTemplate struct {
	ID                   string          `json:"id"`
	TenantID             string          `json:"tenant_id"`
	Name                 string          `json:"name"`
	Brand                string          `json:"brand,omitempty"`
	ProductName          string          `json:"product_name,omitempty"`
	ModelNumber          string          `json:"model_number,omitempty"`
	ThingModelID         string          `json:"thing_model_id"`
	ThingModelSnapshotID string          `json:"thing_model_snapshot_id"`
	ProtocolType         string          `json:"protocol_type,omitempty"`
	ProtocolConfig       json.RawMessage `json:"protocol_config,omitempty"`
	VoucherType          string          `json:"voucher_type,omitempty"`
	ConnectionType       string          `json:"connection_type,omitempty"`
	AutoRegister         bool            `json:"auto_register"`
	OnlineConfig         json.RawMessage `json:"online_config,omitempty"`
	Tags                 json.RawMessage `json:"tags,omitempty"`
	Extras               json.RawMessage `json:"extras,omitempty"`
	DescriptionI18n      json.RawMessage `json:"description_i18n,omitempty"`
	ImageURL             string          `json:"image_url,omitempty"`
	Status               string          `json:"status"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type ListDeviceTemplatesReq struct {
	pageRequest
	Status       string `query:"status"`
	Brand        string `query:"brand"`
	ThingModelID string `query:"thing_model_id"`
	Tag          string `query:"tag"`
	Query        string `query:"query"`
}

type Product struct {
	ID               string          `json:"id"`
	TenantID         string          `json:"tenant_id"`
	Name             string          `json:"name"`
	DeviceTemplateID string          `json:"device_template_id"`
	ProductKey       string          `json:"product_key,omitempty"`
	SKU              string          `json:"sku,omitempty"`
	ImageURL         string          `json:"image_url,omitempty"`
	OTAPackageID     *string         `json:"ota_package_id,omitempty"`
	DescriptionI18n  json.RawMessage `json:"description_i18n,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	DeletedAt        *time.Time      `json:"deleted_at,omitempty"`
}

type ListProductsReq struct {
	pageRequest
	DeviceTemplateID string `query:"device_template_id"`
	Name             string `query:"name"`
}

type GetProductByKeyReq struct {
	ProductKey string `query:"product_key"`
}

type Device struct {
	ID               string     `json:"id"`
	TenantID         string     `json:"tenant_id"`
	DeviceTemplateID string     `json:"device_template_id"`
	ProductID        *string    `json:"product_id,omitempty"`
	SN               string     `json:"sn"`
	Voucher          string     `json:"voucher,omitempty"`
	IsOnline         int        `json:"is_online"`
	ActivateFlag     string     `json:"activate_flag"`
	ParentDeviceID   *string    `json:"parent_device_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ActivatedAt      *time.Time `json:"activated_at,omitempty"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

type ListDevicesReq struct {
	pageRequest
	SN               string `query:"sn"`
	DeviceTemplateID string `query:"device_template_id"`
	ProductID        string `query:"product_id"`
	Status           string `query:"status"` // ACTIVE/INACTIVE
}

type ActuateReq struct {
	Identifier     string          `json:"identifier"`
	Value          json.RawMessage `json:"value,omitempty"`
	Params         json.RawMessage `json:"params,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

type ActuateResp struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

type MetaItemKey struct {
	ID              string          `json:"id"`
	TenantID        *string         `json:"tenant_id,omitempty"`
	Key             string          `json:"key"`
	LabelI18n       json.RawMessage `json:"label_i18n"`
	Scope           string          `json:"scope"`
	ValueSchema     json.RawMessage `json:"value_schema"`
	IsBuiltin       bool            `json:"is_builtin"`
	DescriptionI18n json.RawMessage `json:"description_i18n,omitempty"`
}

type ThingModelDiffResponse struct {
	FromVersion  int                   `json:"from_version"`
	ToVersion    int                   `json:"to_version"`
	AddedItems   []snapshotContentItem `json:"added_items,omitempty"`
	RemovedItems []snapshotContentItem `json:"removed_items,omitempty"`
	ChangedItems []diffChangedItem     `json:"changed_items,omitempty"`
}

type CommandPayload struct {
	MsgID      string         `json:"msgId"`
	Identifier string         `json:"identifier"`
	Params     map[string]any `json:"params"`
}

type propertyPayload struct {
	TS   int64          `json:"ts"`
	Data map[string]any `json:"data"`
}

type eventPayload struct {
	TS         int64          `json:"ts"`
	Identifier string         `json:"identifier"`
	Level      string         `json:"level"`
	Data       map[string]any `json:"data"`
}

type commandRespPayload struct {
	MsgID string         `json:"msgId"`
	Code  int            `json:"code"`
	Data  map[string]any `json:"data,omitempty"`
}

type statusPayload struct {
	Online bool  `json:"online"`
	TS     int64 `json:"ts"`
}

type publishReq struct {
	Changelog string `json:"changelog"`
}

type rollbackReq struct {
	TargetVersion int    `json:"target_version"`
	Changelog     string `json:"changelog"`
}

type deriveDraftReq struct {
	Name string `json:"name"`
}

type bindThingModelReq struct {
	ThingModelSnapshotID string `json:"thing_model_snapshot_id"`
}

func mustTenantID() (string, error) {
	tenantID, err := requiredTenantID(authn.CurrentData())
	if err != nil {
		return "", err
	}
	return normalizePrincipalID(tenantID), nil
}

// mustRawTenantID returns the tenant_id exactly as it appears in the JWT token,
// without UUID normalization. Use this for queries against the legacy ThingsPanel
// database, which stores tenant_id as the raw short hex string (e.g. "d616bcbb")
// rather than a padded UUID.
func mustRawTenantID() (string, error) {
	return requiredTenantID(authn.CurrentData())
}

func requiredTenantID(data *authn.Data) (string, error) {
	return tenantidentity.RequiredTenantID(data)
}

func newID() string {
	return uuid.NewString()
}

func normalizePrincipalID(raw string) string {
	return tenantidentity.NormalizePrincipalID(raw)
}

func nowJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func ensureNonEmpty(name, v string) error {
	if strings.TrimSpace(v) == "" {
		return &errs.Error{Code: errs.InvalidArgument, Message: fmt.Sprintf("%s is required", name)}
	}
	return nil
}

func wrapDBErr(err error, notFound string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return &errs.Error{Code: errs.NotFound, Message: notFound}
	}
	rlog.Error("device metadata db error", "err", err)
	return &errs.Error{Code: errs.Internal, Message: "device metadata persistence error"}
}

func validateItemKind(kind string) error {
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case "PROPERTY", "TELEMETRY", "ATTRIBUTE", "EVENT", "COMMAND":
		return nil
	default:
		return &errs.Error{Code: errs.InvalidArgument, Message: "type must be TELEMETRY, ATTRIBUTE, EVENT, COMMAND, or legacy PROPERTY"}
	}
}

func validateIdentifier(ident string) error {
	return ValidateIdentifier(ident)
}

func validateItemValueType(raw json.RawMessage, value any) error {
	vt, err := parseValueType(raw)
	if err != nil {
		return err
	}
	rawValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal runtime value: %w", err)
	}
	return vt.ValidateValue(rawValue)
}

func collectSnapshotContent(ctx context.Context, thingModelID, tenantID string) (json.RawMessage, error) {
	model, err := getThingModelByID(ctx, tenantID, thingModelID)
	if err != nil {
		return nil, err
	}
	items, _, err := listThingModelItems(ctx, tenantID, thingModelID, "", "", pageRequest{Page: 1, PageSize: 500})
	if err != nil {
		return nil, err
	}
	payload, err := buildSnapshotContent(model, items, 0)
	if err != nil {
		return nil, err
	}
	return nowJSON(payload), nil
}
