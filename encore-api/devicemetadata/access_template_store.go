package devicemetadata

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"encore.app/devicemetadata/accessconfig"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// accessTemplateConfigurationRecord is a storage value, not the public template DTO.
// Device counts and file URLs must be supplied by their authorized owners.
// Transitional S3 rows without a validated configuration or trusted actors are refused
// rather than returned as fabricated, apparently complete v2 templates.
type accessTemplateConfigurationRecord struct {
	ID, TenantID                    string
	Name                            string
	Description, Brand, ProductName *string
	ModelNumber, ImageAssetID       *string
	Tags                            []string
	Configuration                   json.RawMessage
	AdministrativeState             string
	ResourceVersion                 int64
	CreatedBy, UpdatedBy            string
	CreatedAt, UpdatedAt            time.Time
}

func validateAccessTemplateConfigurationRecord(row *accessTemplateConfigurationRecord) error {
	if row == nil {
		return &errs.Error{Code: errs.InvalidArgument, Message: "template configuration is required"}
	}
	if !validAccessTemplateText(row.Name, 1, 128, true) {
		return &errs.Error{Code: errs.InvalidArgument, Message: "template name must contain 1 to 128 characters without surrounding whitespace or control characters"}
	}
	for _, field := range []struct {
		value *string
		max   int
	}{
		{row.Description, 2000}, {row.Brand, 64}, {row.ProductName, 128}, {row.ModelNumber, 64},
	} {
		if field.value != nil && !validAccessTemplateText(*field.value, 0, field.max, false) {
			return &errs.Error{Code: errs.InvalidArgument, Message: "template display field is invalid"}
		}
	}
	if row.ImageAssetID != nil {
		id, err := uuid.Parse(*row.ImageAssetID)
		if err != nil || id == uuid.Nil || id.String() != *row.ImageAssetID {
			return &errs.Error{Code: errs.InvalidArgument, Message: "invalid template image asset ID"}
		}
	}
	if row.AdministrativeState != "enabled" && row.AdministrativeState != "disabled" {
		return &errs.Error{Code: errs.InvalidArgument, Message: "invalid template administrative state"}
	}
	if row.Tags == nil || len(row.Tags) > 32 {
		return &errs.Error{Code: errs.InvalidArgument, Message: "template tags must be an array of at most 32 unique strings"}
	}
	seen := make(map[string]bool, len(row.Tags))
	for _, tag := range row.Tags {
		if !validAccessTemplateText(tag, 1, 64, true) || seen[tag] {
			return &errs.Error{Code: errs.InvalidArgument, Message: "template tags must contain unique nonempty strings of at most 64 characters"}
		}
		seen[tag] = true
	}
	if _, err := accessconfig.DecodeConfiguration(row.Configuration); err != nil {
		return &errs.Error{Code: errs.InvalidArgument, Message: "invalid template configuration"}
	}
	return nil
}

func validAccessTemplateText(value string, min, max int, identifier bool) bool {
	if !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length < min || length > max {
		return false
	}
	if !identifier {
		return true
	}
	if value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validateAccessTemplateStoreContext(tx *sqldb.Tx, tenantID, actorID string) error {
	if tx == nil {
		return &errs.Error{Code: errs.Internal, Message: "template storage requires an existing transaction"}
	}
	id, err := uuid.Parse(tenantID)
	if err != nil || id == uuid.Nil || id.String() != tenantID {
		return &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated tenant context"}
	}
	if actorID == "" || !utf8.ValidString(actorID) || utf8.RuneCountInString(actorID) > 128 || strings.TrimSpace(actorID) != actorID {
		return &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated actor"}
	}
	for _, r := range actorID {
		if unicode.IsControl(r) {
			return &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated actor"}
		}
	}
	return nil
}

// insertAccessTemplateConfigurationTx does not commit. Authorization, authoritative
// provider/model/file validation, idempotency and audit belong to its caller
// and must complete in the same transaction before any public route uses it.
func insertAccessTemplateConfigurationTx(ctx context.Context, tx *sqldb.Tx, tenantID, actorID string, row *accessTemplateConfigurationRecord) (*accessTemplateConfigurationRecord, error) {
	if err := validateAccessTemplateStoreContext(tx, tenantID, actorID); err != nil {
		return nil, err
	}
	if err := validateAccessTemplateConfigurationRecord(row); err != nil {
		return nil, err
	}
	tags, err := json.Marshal(row.Tags)
	if err != nil {
		return nil, err
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO device_access_templates
		(id,tenant_id,name,description,brand,product_name,model_number,image_asset_id,tags,configuration,administrative_state,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12,$12) RETURNING id`,
		uuid.NewString(), tenantID, row.Name, row.Description, row.Brand, row.ProductName, row.ModelNumber, row.ImageAssetID, tags, []byte(row.Configuration), row.AdministrativeState, actorID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return loadAccessTemplateConfigurationTx(ctx, tx, tenantID, id, false)
}

// loadAccessTemplateConfigurationTx locks the sole template aggregate when requested.
// It never resolves old Community IDs or falls back to legacy template columns.
func loadAccessTemplateConfigurationTx(ctx context.Context, tx *sqldb.Tx, tenantID, id string, forUpdate bool) (*accessTemplateConfigurationRecord, error) {
	if tx == nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "template storage requires an existing transaction"}
	}
	query := `SELECT id,tenant_id,name,description,brand,product_name,model_number,image_asset_id,
		tags,configuration,administrative_state,resource_version,created_by,updated_by,created_at,updated_at
		FROM device_access_templates WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var row accessTemplateConfigurationRecord
	var tags []byte
	var createdBy, updatedBy *string
	err := tx.QueryRow(ctx, query, tenantID, id).Scan(&row.ID, &row.TenantID, &row.Name, &row.Description, &row.Brand, &row.ProductName, &row.ModelNumber, &row.ImageAssetID,
		&tags, &row.Configuration, &row.AdministrativeState, &row.ResourceVersion, &createdBy, &updatedBy, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device access template not found"}
	}
	if err != nil {
		return nil, err
	}
	if createdBy == nil || updatedBy == nil || *createdBy == "" || *updatedBy == "" || len(row.Configuration) == 0 {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "template offline conversion is incomplete"}
	}
	row.CreatedBy, row.UpdatedBy = *createdBy, *updatedBy
	if json.Unmarshal(tags, &row.Tags) != nil || validateAccessTemplateConfigurationRecord(&row) != nil {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "stored template configuration is invalid"}
	}
	return &row, nil
}

// saveAccessTemplateConfigurationTx replaces an already merged and validated aggregate.
// It increments exactly once and cannot overwrite a different resource version.
// This repository method intentionally has no independent commit/audit boundary.
func saveAccessTemplateConfigurationTx(ctx context.Context, tx *sqldb.Tx, tenantID, actorID, id string, expectedVersion int64, row *accessTemplateConfigurationRecord) (*accessTemplateConfigurationRecord, error) {
	if err := validateAccessTemplateStoreContext(tx, tenantID, actorID); err != nil {
		return nil, err
	}
	if expectedVersion < 1 || expectedVersion >= 9007199254740991 {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "template version cannot be updated"}
	}
	if err := validateAccessTemplateConfigurationRecord(row); err != nil {
		return nil, err
	}
	tags, err := json.Marshal(row.Tags)
	if err != nil {
		return nil, err
	}
	var updatedID string
	err = tx.QueryRow(ctx, `UPDATE device_access_templates SET name=$4,description=$5,brand=$6,product_name=$7,model_number=$8,
		image_asset_id=$9,tags=$10::jsonb,configuration=$11::jsonb,administrative_state=$12,updated_by=$13,updated_at=NOW(),resource_version=resource_version+1
		WHERE tenant_id=$1 AND id=$2 AND resource_version=$3 AND deleted_at IS NULL RETURNING id`,
		tenantID, id, expectedVersion, row.Name, row.Description, row.Brand, row.ProductName, row.ModelNumber, row.ImageAssetID, tags, []byte(row.Configuration), row.AdministrativeState, actorID).Scan(&updatedID)
	if errors.Is(err, sqldb.ErrNoRows) {
		if _, err := loadAccessTemplateConfigurationTx(ctx, tx, tenantID, id, false); err != nil {
			return nil, err
		}
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device access template has changed"}
	}
	if err != nil {
		return nil, err
	}
	return loadAccessTemplateConfigurationTx(ctx, tx, tenantID, updatedID, false)
}
