package devicemetadata

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// NativeDeviceAccessTemplate is the target OAS projection. JSON-backed configuration
// content stays raw so numeric values retain their database representation.
type NativeDeviceAccessTemplate struct {
	ID                  string          `json:"id"`
	TenantID            string          `json:"tenantId"`
	CreatedAt           time.Time       `json:"createdAt"`
	UpdatedAt           time.Time       `json:"updatedAt"`
	CreatedBy           string          `json:"createdBy"`
	UpdatedBy           string          `json:"updatedBy"`
	ResourceVersion     int64           `json:"resourceVersion"`
	Name                string          `json:"name"`
	Description         *string         `json:"description"`
	Brand               *string         `json:"brand"`
	ProductName         *string         `json:"productName"`
	ModelNumber         *string         `json:"modelNumber"`
	Tags                []string        `json:"tags"`
	Configuration       json.RawMessage `json:"configuration"`
	AdministrativeState string          `json:"administrativeState"`
	ImageAssetID        *string         `json:"imageAssetId"`
	DeviceCount         int64           `json:"deviceCount"`
}

// loadNativeDeviceAccessTemplateTx reads a complete target template projection
// inside the caller-owned transaction. authorizedDeviceIDs is the already
// resolved principal scope; an empty slice means tenant-wide visibility.
func loadNativeDeviceAccessTemplateTx(
	ctx context.Context,
	tx *sqldb.Tx,
	tenantID, templateID string,
	authorizedDeviceIDs []string,
) (*NativeDeviceAccessTemplate, error) {
	if err := validateNativeTemplateReadContext(tx, tenantID, authorizedDeviceIDs); err != nil {
		return nil, err
	}
	templateUUID, err := uuid.Parse(templateID)
	if err != nil || templateUUID == uuid.Nil || templateUUID.String() != templateID {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid device access template ID"}
	}
	stored, err := loadAccessTemplateConfigurationTx(ctx, tx, tenantID, templateID, false)
	if err != nil {
		return nil, nativeTemplateReadStoreError(err)
	}
	if err := validateNativeTemplateReadRecord(stored); err != nil {
		return nil, err
	}

	countQuery := `SELECT count(DISTINCT d.id)::bigint
		FROM device_access_bindings b
		JOIN devices d ON d.tenant_id=b.tenant_id AND d.id=b.device_id
		WHERE b.tenant_id=$1 AND b.template_id=$2 AND d.deleted_at IS NULL`
	countArgs := []any{tenantID, templateID}
	if len(authorizedDeviceIDs) > 0 {
		countQuery += ` AND d.id=ANY(string_to_array($3,',')::uuid[])`
		countArgs = append(countArgs, strings.Join(authorizedDeviceIDs, ","))
	}
	var deviceCount int64
	if err := tx.QueryRow(ctx, countQuery, countArgs...).Scan(&deviceCount); err != nil {
		return nil, err
	}
	if deviceCount < 0 || deviceCount > 9007199254740991 {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "template device count is outside the safe range"}
	}

	return assembleNativeDeviceAccessTemplate(stored, deviceCount)
}

func validateNativeTemplateReadContext(tx *sqldb.Tx, tenantID string, authorizedDeviceIDs []string) error {
	if tx == nil {
		return &errs.Error{Code: errs.Internal, Message: "template projection requires an existing transaction"}
	}
	tenantUUID, err := uuid.Parse(tenantID)
	if err != nil || tenantUUID == uuid.Nil || tenantUUID.String() != tenantID {
		return &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated tenant context"}
	}
	for _, rawID := range authorizedDeviceIDs {
		id, parseErr := uuid.Parse(rawID)
		if parseErr != nil || id == uuid.Nil || id.String() != rawID {
			return &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated device scope"}
		}
	}
	return nil
}

func nativeTemplateReadStoreError(err error) error {
	if errs.Code(err) == errs.FailedPrecondition {
		return &errs.Error{Code: errs.Unavailable, Message: "template is not ready for the target read projection"}
	}
	return err
}

func validateNativeTemplateReadRecord(stored *accessTemplateConfigurationRecord) error {
	if stored == nil {
		return &errs.Error{Code: errs.Unavailable, Message: "template is not ready for the target read projection"}
	}
	if stored.ResourceVersion < 1 || stored.ResourceVersion > 9007199254740991 {
		return &errs.Error{Code: errs.Unavailable, Message: "stored template version is outside the safe range"}
	}
	return nil
}

func assembleNativeDeviceAccessTemplate(
	stored *accessTemplateConfigurationRecord,
	deviceCount int64,
) (*NativeDeviceAccessTemplate, error) {
	if err := validateNativeTemplateReadRecord(stored); err != nil {
		return nil, err
	}
	if deviceCount < 0 || deviceCount > 9007199254740991 {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "template device count is outside the safe range"}
	}
	return &NativeDeviceAccessTemplate{
		ID: stored.ID, TenantID: stored.TenantID,
		CreatedAt: stored.CreatedAt.UTC(), UpdatedAt: stored.UpdatedAt.UTC(),
		CreatedBy: stored.CreatedBy, UpdatedBy: stored.UpdatedBy,
		ResourceVersion: stored.ResourceVersion, Name: stored.Name,
		Description: stored.Description, Brand: stored.Brand, ProductName: stored.ProductName,
		ModelNumber: stored.ModelNumber, Tags: stored.Tags,
		Configuration: append(json.RawMessage(nil), stored.Configuration...), AdministrativeState: stored.AdministrativeState,
		ImageAssetID: stored.ImageAssetID, DeviceCount: deviceCount,
	}, nil
}
