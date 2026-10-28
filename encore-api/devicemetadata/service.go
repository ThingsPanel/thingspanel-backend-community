package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"encore.dev/beta/errs"
)

// Service hosts the thing-model domain and devices MQTT bridge.
//
//encore:service
type Service struct {
	mqtt *mqttBridge
}

func initService() (*Service, error) {
	bridge, err := newMQTTBridgeFromEnv()
	if err != nil {
		return nil, err
	}
	return &Service{mqtt: bridge}, nil
}

// Healthz reports whether the service process booted correctly.
//
//encore:api public method=GET path=/api/device-metadata/healthz
func (s *Service) Healthz(ctx context.Context) (*HealthzResponse, error) {
	_ = ctx
	return &HealthzResponse{Status: "ok"}, nil
}


//encore:api auth method=POST path=/api/thing-models
func (s *Service) CreateThingModel(ctx context.Context, req *ThingModel) (*ThingModel, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return createThingModel(ctx, tenantID, req)
}

//encore:api auth method=GET path=/api/thing-models
func (s *Service) ListThingModels(ctx context.Context, req *ThingModelListReq) (*listResponse[ThingModel], error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	items, total, err := listThingModels(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}
	return &listResponse[ThingModel]{Items: items, Page: req.Page, PageSize: req.PageSize, Total: total}, nil
}

//encore:api auth method=GET path=/api/thing-models/:id
func (s *Service) GetThingModel(ctx context.Context, id string) (*ThingModel, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return getThingModelByID(ctx, tenantID, id)
}

//encore:api auth method=PUT path=/api/thing-models/:id
func (s *Service) UpdateThingModel(ctx context.Context, id string, req *ThingModel) (*ThingModel, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return updateThingModel(ctx, tenantID, id, req)
}

//encore:api auth method=DELETE path=/api/thing-models/:id
func (s *Service) DeleteThingModel(ctx context.Context, id string) error {
	tenantID, err := mustTenantID()
	if err != nil {
		return err
	}
	return deleteThingModel(ctx, tenantID, id)
}

//encore:api auth method=POST path=/api/thing-models/:id/publish
func (s *Service) PublishThingModel(ctx context.Context, id string, req *publishReq) (*ThingModelSnapshot, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return publishThingModel(ctx, tenantID, id, currentUserID(), req)
}

//encore:api auth method=POST path=/api/thing-models/:id/derive-draft
func (s *Service) DeriveThingModelDraft(ctx context.Context, id string, req *deriveDraftReq) (*ThingModel, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return deriveThingModelDraft(ctx, tenantID, id, req)
}

//encore:api auth method=POST path=/api/thing-models/:id/rollback
func (s *Service) RollbackThingModel(ctx context.Context, id string, req *rollbackReq) (*ThingModelSnapshot, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return rollbackThingModel(ctx, tenantID, id, currentUserID(), req)
}

//encore:api auth method=POST path=/api/thing-models/:id/archive
func (s *Service) ArchiveThingModel(ctx context.Context, id string) error {
	tenantID, err := mustTenantID()
	if err != nil {
		return err
	}
	return archiveThingModel(ctx, tenantID, id)
}

type ThingModelVersionsResponse struct {
	Items []ThingModelSnapshot `json:"items"`
}

//encore:api auth method=GET path=/api/thing-models/:id/versions
func (s *Service) ListVersions(ctx context.Context, id string) (*ThingModelVersionsResponse, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	items, err := listVersions(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return &ThingModelVersionsResponse{Items: items}, nil
}

//encore:api auth method=GET path=/api/thing-models/:id/versions/:version
func (s *Service) GetVersion(ctx context.Context, id string, version int) (*ThingModelSnapshot, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return getVersion(ctx, tenantID, id, version)
}

type DiffVersionsReq struct {
	From int `query:"from"`
	To   int `query:"to"`
}

//encore:api auth method=GET path=/api/thing-models/:id/diff
func (s *Service) DiffVersions(ctx context.Context, id string, req *DiffVersionsReq) (*ThingModelDiffResponse, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return diffThingModelVersions(ctx, tenantID, id, req.From, req.To)
}

type ListItemsReq struct {
	pageRequest
	Type  string `query:"type"`
	Query string `query:"query"`
}

//encore:api auth method=POST path=/api/thing-models/:tmId/items
func (s *Service) CreateItem(ctx context.Context, tmId string, req *ThingModelItem) (*ThingModelItem, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return createThingModelItem(ctx, tenantID, tmId, req)
}

type BatchCreateItemsReq struct {
	Items []ThingModelItem `json:"items"`
}

type BatchCreateItemsResponse struct {
	Items []ThingModelItem `json:"items"`
}

//encore:api auth method=POST path=/api/thing-models/:tmId/items/batch
func (s *Service) BatchCreateItems(ctx context.Context, tmId string, req *BatchCreateItemsReq) (*BatchCreateItemsResponse, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	items := make([]ThingModelItem, 0, len(req.Items))
	for _, item := range req.Items {
		created, err := createThingModelItem(ctx, tenantID, tmId, &item)
		if err != nil {
			return nil, err
		}
		items = append(items, *created)
	}
	return &BatchCreateItemsResponse{Items: items}, nil
}

//encore:api auth method=GET path=/api/thing-models/:tmId/items
func (s *Service) ListItems(ctx context.Context, tmId string, req *ListItemsReq) (*listResponse[ThingModelItem], error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	items, total, err := listThingModelItems(ctx, tenantID, tmId, req.Type, req.Query, req.pageRequest)
	if err != nil {
		return nil, err
	}
	return &listResponse[ThingModelItem]{Items: items, Page: req.Page, PageSize: req.PageSize, Total: total}, nil
}

//encore:api auth method=GET path=/api/thing-models/:tmId/items/:id
func (s *Service) GetItem(ctx context.Context, tmId string, id string) (*ThingModelItem, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return getThingModelItem(ctx, tenantID, tmId, id)
}

//encore:api auth method=PUT path=/api/thing-models/:tmId/items/:id
func (s *Service) UpdateItem(ctx context.Context, tmId string, id string, req *ThingModelItem) (*ThingModelItem, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return updateThingModelItem(ctx, tenantID, tmId, id, req)
}

//encore:api auth method=DELETE path=/api/thing-models/:tmId/items/:id
func (s *Service) DeleteItem(ctx context.Context, tmId string, id string) error {
	tenantID, err := mustTenantID()
	if err != nil {
		return err
	}
	return deleteThingModelItem(ctx, tenantID, tmId, id)
}

//encore:api auth method=POST path=/api/device-templates
func (s *Service) CreateTemplate(ctx context.Context, req *DeviceTemplate) (*DeviceTemplate, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return createDeviceTemplate(ctx, tenantID, req)
}

//encore:api auth method=GET path=/api/device-templates
func (s *Service) ListTemplates(ctx context.Context, req *ListDeviceTemplatesReq) (*listResponse[DeviceTemplate], error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	items, total, err := listDeviceTemplates(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}
	return &listResponse[DeviceTemplate]{Items: items, Page: req.Page, PageSize: req.PageSize, Total: total}, nil
}

//encore:api auth method=GET path=/api/device-templates/:id
func (s *Service) GetTemplate(ctx context.Context, id string) (*DeviceTemplate, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return getDeviceTemplate(ctx, tenantID, id)
}

//encore:api auth method=PUT path=/api/device-templates/:id
func (s *Service) UpdateTemplate(ctx context.Context, id string, req *DeviceTemplate) (*DeviceTemplate, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return updateDeviceTemplate(ctx, tenantID, id, req)
}

//encore:api auth method=DELETE path=/api/device-templates/:id
func (s *Service) DeleteTemplate(ctx context.Context, id string) error {
	tenantID, err := mustTenantID()
	if err != nil {
		return err
	}
	return deleteDeviceTemplate(ctx, tenantID, id)
}

//encore:api auth method=POST path=/api/device-templates/:id/publish
func (s *Service) PublishTemplate(ctx context.Context, id string) (*DeviceTemplate, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return publishDeviceTemplate(ctx, tenantID, id)
}

//encore:api auth method=POST path=/api/device-templates/:id/archive
func (s *Service) ArchiveTemplate(ctx context.Context, id string) (*DeviceTemplate, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return archiveDeviceTemplate(ctx, tenantID, id)
}

//encore:api auth method=POST path=/api/device-templates/:id/derive-draft
func (s *Service) DeriveTemplateDraft(ctx context.Context, id string) (*DeviceTemplate, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return deriveDeviceTemplateDraftAdvanced(ctx, tenantID, id)
}

//encore:api auth method=POST path=/api/device-templates/:id/bind-thing-model
func (s *Service) BindThingModelVersion(ctx context.Context, id string, req *bindThingModelReq) (*DeviceTemplate, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return bindThingModelVersionAdvanced(ctx, tenantID, id, req.ThingModelSnapshotID)
}

type DeviceTemplatesByThingModelResponse struct {
	Items []DeviceTemplate `json:"items"`
}

//encore:api auth method=GET path=/api/thing-models/:thingModelID/device-templates
func (s *Service) ListTemplatesByThingModel(ctx context.Context, thingModelID string) (*DeviceTemplatesByThingModelResponse, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	items, err := listDeviceTemplatesByThingModel(ctx, tenantID, thingModelID)
	if err != nil {
		return nil, err
	}
	return &DeviceTemplatesByThingModelResponse{Items: items}, nil
}

//encore:api auth method=POST path=/api/products
func (s *Service) CreateProduct(ctx context.Context, req *Product) (*Product, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return createProduct(ctx, tenantID, req)
}

//encore:api auth method=GET path=/api/products
func (s *Service) ListProducts(ctx context.Context, req *ListProductsReq) (*listResponse[*Product], error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	items, total, err := listProducts(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}
	return &listResponse[*Product]{Items: items, Page: req.Page, PageSize: req.PageSize, Total: total}, nil
}

//encore:api auth method=GET path=/api/products/:id
func (s *Service) GetProduct(ctx context.Context, id string) (*Product, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return getProduct(ctx, tenantID, id)
}

//encore:api auth method=PUT path=/api/products/:id
func (s *Service) UpdateProduct(ctx context.Context, id string, req *Product) (*Product, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return updateProduct(ctx, tenantID, id, req)
}

//encore:api auth method=DELETE path=/api/products/:id
func (s *Service) DeleteProduct(ctx context.Context, id string) error {
	tenantID, err := mustTenantID()
	if err != nil {
		return err
	}
	return deleteProduct(ctx, tenantID, id)
}

//encore:api auth method=GET path=/api/product-keys/:productKey
func (s *Service) GetProductByKey(ctx context.Context, productKey string) (*Product, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return getProductByProductKey(ctx, tenantID, productKey)
}

type ProductsByTemplateResponse struct {
	Items []*Product `json:"items"`
}

//encore:api auth method=GET path=/api/templates/:templateID/products
func (s *Service) ListProductsByTemplate(ctx context.Context, templateID string) (*ProductsByTemplateResponse, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	items, err := listProductsByTemplate(ctx, tenantID, templateID)
	if err != nil {
		return nil, err
	}
	return &ProductsByTemplateResponse{Items: items}, nil
}

//encore:api auth method=POST path=/api/devices
func (s *Service) CreateDevice(ctx context.Context, req *Device) (*Device, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return createDevice(ctx, tenantID, req)
}

//encore:api auth method=GET path=/api/devices
func (s *Service) ListDevices(ctx context.Context, req *ListDevicesReq) (*listResponse[Device], error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	items, total, err := listDevices(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}
	return &listResponse[Device]{Items: items, Page: req.Page, PageSize: req.PageSize, Total: total}, nil
}

//encore:api auth method=GET path=/api/devices/:id
func (s *Service) GetDevice(ctx context.Context, id string) (*Device, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return getDevice(ctx, tenantID, id)
}

//encore:api auth method=PUT path=/api/devices/:id
func (s *Service) UpdateDevice(ctx context.Context, id string, req *Device) (*Device, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return updateDevice(ctx, tenantID, id, req)
}

//encore:api auth method=DELETE path=/api/devices/:id
func (s *Service) DeleteDevice(ctx context.Context, id string) error {
	tenantID, err := mustTenantID()
	if err != nil {
		return err
	}
	return deleteDevice(ctx, tenantID, id)
}

//encore:api auth method=POST path=/api/devices/:id/actuate
func (s *Service) Actuate(ctx context.Context, id string, req *ActuateReq) (*ActuateResp, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}

	// Validate request
	if strings.TrimSpace(req.Identifier) == "" {
		return &ActuateResp{Valid: false, Errors: []ValidationError{{
			Field:   "identifier",
			Code:    "REQUIRED",
			Message: "identifier is required",
		}}}, nil
	}

	// Perform actuate validation
	resp, err := actuate(ctx, tenantID, id, req)
	if err != nil {
		return nil, err
	}

	// If validation passed, publish to MQTT
	if resp.Valid {
		device, err := getDevice(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}

		// Publish downlink message
		payload := &CommandPayload{
			MsgID:      id + "-command",
			Identifier: strings.TrimSpace(req.Identifier),
			Params:     nil,
		}
		if len(req.Params) > 0 {
			json.Unmarshal(req.Params, &payload.Params)
		} else if len(req.Value) > 0 {
			// For PROPERTY write, pass value as params
			var val any
			json.Unmarshal(req.Value, &val)
			payload.Params = map[string]any{"value": val}
		}

		if err := s.mqtt.publishCommandDown(device.SN, payload); err != nil {
			return nil, &errs.Error{Code: errs.Unavailable, Message: err.Error()}
		}
	}

	return resp, nil
}

type DeviceLatestResponse struct {
	Values map[string]json.RawMessage `json:"values"`
}

//encore:api auth method=GET path=/api/devices/:id/latest
func (s *Service) GetDeviceLatest(ctx context.Context, id string) (*DeviceLatestResponse, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	if _, err := getDevice(ctx, tenantID, id); err != nil {
		return nil, err
	}
	rawValues := s.mqtt.latestForDevice(id)
	values := make(map[string]json.RawMessage, len(rawValues))
	for key, value := range rawValues {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		values[key] = data
	}
	return &DeviceLatestResponse{Values: values}, nil
}

type MetaItemKeysResponse struct {
	Items []MetaItemKey `json:"items"`
}

//encore:api auth method=GET path=/api/meta-item-keys
func (s *Service) ListMetaItemKeys(ctx context.Context) (*MetaItemKeysResponse, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	items, err := listMetaItemKeys(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &MetaItemKeysResponse{Items: items}, nil
}

//encore:api auth method=POST path=/api/validate/thing-model-item
func (s *Service) ValidateItem(ctx context.Context, req *validateThingModelItemReq) (*validateResp, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	validationErrors := validateInlineThingModelItem(*req)
	metaItems, err := parseMetaItems(req.MetaItems)
	if err != nil {
		validationErrors = appendValidationError(validationErrors, "meta_items", err, "THING_MODEL_META_ITEM_INVALID")
	} else {
		validationErrors = append(validationErrors, validateMetaItems(ctx, tenantID, metaItems)...)
	}
	if req.ThingModelID != "" && strings.TrimSpace(req.Identifier) != "" {
		exists, err := checkIdentifierExists(ctx, tenantID, req.ThingModelID, req.Identifier, req.ItemID)
		if err != nil {
			return nil, err
		}
		if exists {
			validationErrors = append(validationErrors, ValidationError{
				Field:   "identifier",
				Code:    "THING_MODEL_IDENTIFIER_DUPLICATE",
				Message: "thing model identifier already exists",
			})
		}
	}
	sortValidationErrors(validationErrors)
	return &validateResp{Valid: len(validationErrors) == 0, Errors: validationErrors}, nil
}

//encore:api auth method=POST path=/api/validate/thing-model
func (s *Service) ValidateThingModel(ctx context.Context, req *validateThingModelReq) (*validateResp, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	validationErrors := make([]ValidationError, 0)
	if desc, err := parseI18n(req.DescriptionI18n); err != nil {
		validationErrors = appendValidationError(validationErrors, "description_i18n", err, "THING_MODEL_I18N_INVALID")
	} else if desc != nil {
		validationErrors = appendValidationError(validationErrors, "description_i18n", desc.ValidateAsDescription(), "THING_MODEL_I18N_INVALID")
	}
	validationErrors = append(validationErrors, findDuplicateIdentifiers(req.Items)...)
	for i, item := range req.Items {
		itemErrs := validateInlineThingModelItem(item)
		metaItems, err := parseMetaItems(item.MetaItems)
		if err != nil {
			itemErrs = appendValidationError(itemErrs, "meta_items", err, "THING_MODEL_META_ITEM_INVALID")
		} else {
			itemErrs = append(itemErrs, validateMetaItems(ctx, tenantID, metaItems)...)
		}
		for _, itemErr := range itemErrs {
			itemErr.Field = fmt.Sprintf("items[%d].%s", i, itemErr.Field)
			validationErrors = append(validationErrors, itemErr)
		}
	}
	sortValidationErrors(validationErrors)
	return &validateResp{Valid: len(validationErrors) == 0, Errors: validationErrors}, nil
}

//encore:api auth method=POST path=/api/validate/device-template
func (s *Service) ValidateTemplate(ctx context.Context, req *validateDeviceTemplateReq) (*validateResp, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	errors := make([]ValidationError, 0, 4)
	if strings.TrimSpace(req.Name) == "" {
		errors = append(errors, ValidationError{Field: "name", Code: "DEVICE_TEMPLATE_NAME_INVALID", Message: "name is required"})
	}
	if _, err := getThingModelByID(ctx, tenantID, req.ThingModelID); err != nil {
		errors = append(errors, ValidationError{Field: "thing_model_id", Code: "THING_MODEL_NOT_FOUND", Message: "thing model not found"})
	}
	var snapshotExists int
	if err := db.QueryRow(ctx, `SELECT 1 FROM thing_model_snapshots WHERE tenant_id = $1 AND id = $2`, tenantID, req.ThingModelSnapshotID).Scan(&snapshotExists); err != nil {
		errors = append(errors, ValidationError{Field: "thing_model_snapshot_id", Code: "THING_MODEL_SNAPSHOT_NOT_FOUND", Message: "thing model snapshot not found"})
	}
	return &validateResp{Valid: len(errors) == 0, Errors: errors}, nil
}

// DictionaryEntry is a single enum entry in dictionaries
type DictionaryEntry struct {
	Code      string          `json:"code"`
	LabelI18n json.RawMessage `json:"label_i18n"`
}

// DictionariesResponse is the full response for the /api/dictionaries endpoint
type DictionariesResponse struct {
	ItemTypes       []DictionaryEntry `json:"item_types"`
	ValueKinds      []DictionaryEntry `json:"value_kinds"`
	AccessKeys      []string          `json:"access_keys"`
	ProtocolTypes   []string          `json:"protocol_types"`
	ConnectionTypes []string          `json:"connection_types"`
	TemplateStatus  []string          `json:"template_status"`
	DeviceStatus    []string          `json:"device_status"`
	ActivateFlags   []string          `json:"activate_flags"`
}

// i18nLabel creates an I18nString JSON value for enums
func i18nLabel(zhCN, enUS string) json.RawMessage {
	data := map[string]interface{}{
		"default": zhCN,
		"locales": map[string]string{
			"en-US": enUS,
		},
	}
	raw, _ := json.Marshal(data)
	return raw
}

// dictionariesCache holds the cached dictionaries
type dictionariesCache struct {
	data      *DictionariesResponse
	timestamp time.Time
	ttl       time.Duration
	mu        sync.RWMutex
}

var (
	dictCache = &dictionariesCache{
		ttl: 5 * time.Minute,
	}
	dictOnce sync.Once
)

// buildDictionaries constructs the complete dictionaries response
func buildDictionaries() *DictionariesResponse {
	return &DictionariesResponse{
		ItemTypes: []DictionaryEntry{
			{
				Code:      "PROPERTY",
				LabelI18n: i18nLabel("属性", "Property"),
			},
			{
				Code:      "COMMAND",
				LabelI18n: i18nLabel("命令", "Command"),
			},
			{
				Code:      "EVENT",
				LabelI18n: i18nLabel("事件", "Event"),
			},
		},
		ValueKinds: []DictionaryEntry{
			{
				Code:      "INT",
				LabelI18n: i18nLabel("整数", "Integer"),
			},
			{
				Code:      "FLOAT",
				LabelI18n: i18nLabel("浮点数", "Float"),
			},
			{
				Code:      "BOOL",
				LabelI18n: i18nLabel("布尔值", "Boolean"),
			},
			{
				Code:      "STRING",
				LabelI18n: i18nLabel("字符串", "String"),
			},
			{
				Code:      "ENUM",
				LabelI18n: i18nLabel("枚举", "Enum"),
			},
		},
		AccessKeys: []string{
			"read",
			"write",
			"observe",
		},
		ProtocolTypes: []string{
			"MQTT",
			"HTTP",
			"COAP",
			"MODBUS",
		},
		ConnectionTypes: []string{
			"DIRECT",
			"GATEWAY_CHILD",
		},
		TemplateStatus: []string{
			"DRAFT",
			"PUBLISHED",
			"ARCHIVED",
		},
		DeviceStatus: []string{
			"INACTIVE",
			"ACTIVE",
		},
		ActivateFlags: []string{
			"INACTIVE",
			"ACTIVE",
		},
	}
}

// buildMetaItemKeysForAPI constructs the list of meta item keys for API response
func buildMetaItemKeysForAPI() []MetaItemKey {
	return []MetaItemKey{
		{
			ID:        "store_policy",
			Key:       "store_policy",
			LabelI18n: i18nLabel("存储策略", "Store Policy"),
			ValueSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"archive_days": {"type": "integer", "minimum": 0},
					"retention_days": {"type": "integer", "minimum": 0}
				}
			}`),
			IsBuiltin: true,
		},
		{
			ID:        "chart_unit",
			Key:       "chart_unit",
			LabelI18n: i18nLabel("图表单位", "Chart Unit"),
			ValueSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"unit": {"type": "string"},
					"display_name": {"type": "string"}
				}
			}`),
			IsBuiltin: true,
		},
		{
			ID:        "chart_config",
			Key:       "chart_config",
			LabelI18n: i18nLabel("图表配置", "Chart Configuration"),
			ValueSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"type": {"type": "string", "enum": ["line", "bar", "scatter"]},
					"title": {"type": "string"}
				}
			}`),
			IsBuiltin: true,
		},
		{
			ID:        "alert_threshold",
			Key:       "alert_threshold",
			LabelI18n: i18nLabel("告警阈值", "Alert Threshold"),
			ValueSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"min": {"type": "number"},
					"max": {"type": "number"},
					"enabled": {"type": "boolean"}
				}
			}`),
			IsBuiltin: true,
		},
		{
			ID:        "validation_rules",
			Key:       "validation_rules",
			LabelI18n: i18nLabel("验证规则", "Validation Rules"),
			ValueSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pattern": {"type": "string"},
					"format": {"type": "string"}
				}
			}`),
			IsBuiltin: true,
		},
		{
			ID:        "display_format",
			Key:       "display_format",
			LabelI18n: i18nLabel("显示格式", "Display Format"),
			ValueSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"precision": {"type": "integer"},
					"thousands_separator": {"type": "boolean"}
				}
			}`),
			IsBuiltin: true,
		},
	}
}

// getDictionaries returns the cached dictionaries, refreshing if expired
func getDictionaries(ctx context.Context) (*DictionariesResponse, error) {
	dictOnce.Do(func() {
		dictCache.mu.Lock()
		defer dictCache.mu.Unlock()
		dictCache.data = buildDictionaries()
		dictCache.timestamp = time.Now()
	})

	dictCache.mu.RLock()
	data := dictCache.data
	timestamp := dictCache.timestamp
	ttl := dictCache.ttl
	dictCache.mu.RUnlock()

	if data == nil || time.Since(timestamp) > ttl {
		dictCache.mu.Lock()
		dictCache.data = buildDictionaries()
		dictCache.timestamp = time.Now()
		data = dictCache.data
		dictCache.mu.Unlock()
	}

	return data, nil
}

// DeleteThingModelItemByID deletes a ThingModel item using only its item ID.
// The thingModelID is resolved internally, so callers do not need to supply it.
//
//encore:api auth method=DELETE path=/api/thing-model-items/:id
func (s *Service) DeleteThingModelItemByID(ctx context.Context, id string) error {
	tenantID, err := mustTenantID()
	if err != nil {
		return err
	}
	return deleteThingModelItemByID(ctx, tenantID, id)
}

// UpdateThingModelItemByID updates a ThingModel item using only its item ID.
//
//encore:api auth method=PUT path=/api/thing-model-items/:id
func (s *Service) UpdateThingModelItemByID(ctx context.Context, id string, req *ThingModelItem) (*ThingModelItem, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	return updateThingModelItemByID(ctx, tenantID, id, req)
}

// GetPublicMetaItemKeys returns the list of available meta item keys (public endpoint)
//
//encore:api public method=GET path=/api/public/meta-item-keys
func (s *Service) GetPublicMetaItemKeys(ctx context.Context) (*MetaItemKeysResponse, error) {
	keys := buildMetaItemKeysForAPI()
	return &MetaItemKeysResponse{Items: keys}, nil
}

// GetDictionaries returns all system dictionaries and enums (public endpoint)
//
//encore:api public method=GET path=/api/dictionaries
func (s *Service) GetDictionaries(ctx context.Context) (*DictionariesResponse, error) {
	return getDictionaries(ctx)
}
