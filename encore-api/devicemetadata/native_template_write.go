package devicemetadata

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"encore.app/devicemetadata/accessconfig"
	"encore.app/fileassets"
	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.app/thingmodel/core"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/jackc/pgx/v5/pgconn"
)

type nativeTemplateWriteIdentity struct {
	nativeTemplateReadIdentity
	subjectID string
}

func currentNativeTemplateWriteIdentity() (nativeTemplateWriteIdentity, error) {
	read, err := currentNativeTemplateReadIdentity()
	if err != nil {
		return nativeTemplateWriteIdentity{}, err
	}
	data := authn.CurrentData()
	if !authz.Can(data, "template", "write") || len(read.deviceIDs) != 0 {
		return nativeTemplateWriteIdentity{}, &errs.Error{Code: errs.PermissionDenied, Message: "template changes require tenant-wide template write authority"}
	}
	if !validAccessTemplateText(data.UserID, 1, 128, true) {
		return nativeTemplateWriteIdentity{}, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated subject"}
	}
	return nativeTemplateWriteIdentity{nativeTemplateReadIdentity: read, subjectID: data.UserID}, nil
}

type nativeTemplateAssetCheck func(context.Context, string, string) error

var errNativeTemplateETagMismatch = &errs.Error{Code: errs.FailedPrecondition, Message: "template has changed; reload before saving"}

func validateNativeTemplateAsset(ctx context.Context, tenantID, assetID string) error {
	trustedTenant, err := mustTenantID()
	if err != nil || trustedTenant != tenantID {
		return &errs.Error{Code: errs.PermissionDenied, Message: "invalid template image tenant scope"}
	}
	asset, err := fileassets.ValidateTemplateImage(ctx, &fileassets.ValidateTemplateImageRequest{AssetID: assetID})
	if err != nil {
		return err
	}
	if asset == nil || asset.AssetID != assetID {
		return &errs.Error{Code: errs.Unavailable, Message: "template image asset validation is unavailable"}
	}
	// This owner call returns only the identity, without issuing a capability.
	return nil
}

func validateNativeTemplateConfigurationTx(ctx context.Context, tx *sqldb.Tx, tenantID string, configuration accessconfig.Configuration, check nativeDeviceConfigurationCheck) error {
	if err := configuration.Validate(); err != nil {
		return invalid("configuration", "invalid template configuration")
	}
	access, err := json.Marshal(configuration.Access)
	if err != nil {
		return invalid("configuration.access", "invalid access configuration")
	}
	if check == nil {
		return &errs.Error{Code: errs.Unavailable, Message: "access configuration validation is unavailable"}
	}
	if err := check(ctx, tenantID, configuration.DeviceKind, access, json.RawMessage(`{}`)); err != nil {
		return err
	}
	binding, err := resolveAccessTemplateModelBindingTx(ctx, tx, tenantID, configuration.Model)
	if err != nil {
		return err
	}
	var snapshot core.Model
	if binding != nil {
		var content []byte
		if err := tx.QueryRow(ctx, `SELECT canonical_content FROM thing_model_snapshots WHERE tenant_id=$1 AND thing_model_id=$2 AND id=$3`, tenantID, binding.ModelID, binding.SnapshotID).Scan(&content); err != nil {
			return err
		}
		if json.Unmarshal(content, &snapshot) != nil || snapshot.Fields == nil {
			return &errs.Error{Code: errs.Unavailable, Message: "selected ThingModel snapshot is invalid"}
		}
	}
	for _, mapping := range configuration.TopicMappings {
		if string(mapping.DataIdentifier) == "null" {
			continue
		}
		var kind, identifier string
		_ = json.Unmarshal(mapping.DataKind, &kind)
		_ = json.Unmarshal(mapping.DataIdentifier, &identifier)
		found := false
		for _, field := range snapshot.Fields {
			if string(field.Kind) == kind && field.Identifier == identifier {
				found = true
				break
			}
		}
		if !found {
			return invalid("configuration.topicMappings", "data reference is absent from the selected ThingModel version")
		}
	}
	return nil
}

func nativeTemplateMutationResponse(template *NativeDeviceAccessTemplate, requestID string) (metadataWriteResponse, error) {
	body, err := json.Marshal(NativeDeviceAccessTemplateResponse{Data: *template, Meta: RequestMeta{RequestID: requestID}})
	if err != nil {
		return metadataWriteResponse{}, err
	}
	etag, err := nativeTemplateETag(template)
	return metadataWriteResponse{Status: 200, Body: body, ETag: etag}, err
}

func createNativeTemplate(ctx context.Context, identity nativeTemplateWriteIdentity, key string, body []byte, requestID string, check nativeDeviceConfigurationCheck, checkAsset nativeTemplateAssetCheck) (*metadataWriteResponse, error) {
	input, err := ParseNativeTemplateCreate(body)
	if err != nil {
		return nil, err
	}
	command := metadataWriteCommand{TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "createDeviceAccessTemplate", Method: "POST", Path: "/api/v2/device-access-templates", Key: key, RequestID: requestID, CanonicalBody: input.CanonicalBody}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		if err := validateNativeTemplateConfigurationTx(ctx, tx, identity.tenantID, input.Configuration, check); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		row := &accessTemplateConfigurationRecord{Name: input.Name, Description: nativeStringPointer(input.Description), Brand: nativeStringPointer(input.Brand), ProductName: nativeStringPointer(input.ProductName), ModelNumber: nativeStringPointer(input.ModelNumber), Tags: input.Tags, Configuration: input.ConfigurationJSON, AdministrativeState: "enabled", ImageAssetID: nativeStringPointer(input.ImageAssetID)}
		if row.ImageAssetID != nil {
			if checkAsset == nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "template image asset validation is unavailable"}
			}
			if err := checkAsset(ctx, identity.tenantID, *row.ImageAssetID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		created, err := insertAccessTemplateConfigurationTx(ctx, tx, identity.tenantID, identity.subjectID, row)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		template, err := assembleNativeDeviceAccessTemplate(created, 0)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := nativeTemplateMutationResponse(template, requestID)
		response.Status, response.Location = 201, "/api/v2/device-access-templates/"+created.ID
		return response, metadataWriteAudit{ResourceKind: "device_access_template", ResourceID: created.ID, AfterVersion: &created.ResourceVersion, RedactedChanges: json.RawMessage(`{"created":true}`)}, err
	})
	return result, mapNativeTemplateWriteError(err)
}

func updateNativeTemplate(ctx context.Context, identity nativeTemplateWriteIdentity, id, key, ifMatch string, body []byte, requestID string, check nativeDeviceConfigurationCheck, checkAsset nativeTemplateAssetCheck) (*metadataWriteResponse, error) {
	input, err := ParseNativeTemplatePatch(body)
	if err != nil {
		return nil, err
	}
	command := metadataWriteCommand{TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "updateDeviceAccessTemplate", Method: "PATCH", Path: "/api/v2/device-access-templates/" + id, Key: key, RequestID: requestID, IfMatch: ifMatch, CanonicalBody: input.CanonicalBody}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		stored, err := loadAccessTemplateConfigurationTx(ctx, tx, identity.tenantID, id, true)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		current, err := loadNativeDeviceAccessTemplateTx(ctx, tx, identity.tenantID, id, nil)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		etag, err := nativeTemplateETag(current)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if ifMatch != etag {
			return metadataWriteResponse{}, metadataWriteAudit{}, errNativeTemplateETagMismatch
		}
		next := *stored
		if input.Name.Present {
			next.Name = input.Name.Value
		}
		for _, field := range []struct {
			input NativeDeviceField[string]
			out   **string
		}{{input.Description, &next.Description}, {input.Brand, &next.Brand}, {input.ProductName, &next.ProductName}, {input.ModelNumber, &next.ModelNumber}, {input.ImageAssetID, &next.ImageAssetID}} {
			if field.input.Present {
				*field.out = nativeStringPointer(field.input)
			}
		}
		if input.Tags.Present {
			next.Tags = input.Tags.Value
		}
		if input.AdministrativeState.Present {
			next.AdministrativeState = input.AdministrativeState.Value
		}
		if input.ConfigurationJSON.Present {
			var previous accessconfig.Configuration
			if json.Unmarshal(stored.Configuration, &previous) != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "stored template configuration is invalid"}
			}
			// The existing edit form keeps the device type fixed. Changing it
			// would leave gateway/member identities inconsistent with the template.
			if input.Configuration.Value.DeviceKind != previous.DeviceKind {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("configuration.deviceKind", "device kind cannot be changed after template creation")
			}
			if err := validateNativeTemplateConfigurationTx(ctx, tx, identity.tenantID, input.Configuration.Value, check); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			if err := validateNativeCustomFieldChangesAgainstDevices(ctx, tx, identity.tenantID, id, previous.CustomFieldDefinitions, input.Configuration.Value.CustomFieldDefinitions); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			var equal bool
			if err := tx.QueryRow(ctx, `SELECT $1::jsonb=$2::jsonb`, []byte(stored.Configuration), []byte(input.ConfigurationJSON.Value)).Scan(&equal); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			if !equal {
				next.Configuration = input.ConfigurationJSON.Value
			}
		}
		if input.CustomFieldChanges.Present {
			merged, oldDefinitions, nextDefinitions, err := mergeNativeCustomFieldChanges(stored.Configuration, input.CustomFieldChanges.Value)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			configuration, err := accessconfig.DecodeConfiguration(merged)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("customFieldChanges", "resulting definitions are invalid")
			}
			var previous accessconfig.Configuration
			if json.Unmarshal(stored.Configuration, &previous) != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "stored template configuration is invalid"}
			}
			if configuration.DeviceKind != previous.DeviceKind {
				return metadataWriteResponse{}, metadataWriteAudit{}, invalid("configuration.deviceKind", "device kind cannot be changed after template creation")
			}
			if err := validateNativeTemplateConfigurationTx(ctx, tx, identity.tenantID, configuration, check); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			if err := validateNativeCustomFieldChangesAgainstDevices(ctx, tx, identity.tenantID, id, oldDefinitions, nextDefinitions); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			var equal bool
			if err := tx.QueryRow(ctx, `SELECT $1::jsonb=$2::jsonb`, []byte(stored.Configuration), []byte(merged)).Scan(&equal); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			if !equal {
				next.Configuration = merged
			}
		}
		if input.ImageAssetID.Present && next.ImageAssetID != nil {
			if checkAsset == nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "template image asset validation is unavailable"}
			}
			if err := checkAsset(ctx, identity.tenantID, *next.ImageAssetID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		changed := !reflect.DeepEqual(*stored, next)
		if changed {
			if _, err := saveAccessTemplateConfigurationTx(ctx, tx, identity.tenantID, identity.subjectID, id, stored.ResourceVersion, &next); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		template, err := loadNativeDeviceAccessTemplateTx(ctx, tx, identity.tenantID, id, nil)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := nativeTemplateMutationResponse(template, requestID)
		changes, _ := json.Marshal(map[string]bool{"changed": changed})
		return response, metadataWriteAudit{ResourceKind: "device_access_template", ResourceID: id, BeforeVersion: &stored.ResourceVersion, AfterVersion: &template.ResourceVersion, RedactedChanges: changes}, err
	})
	return result, mapNativeTemplateWriteError(err)
}

func nativeStringPointer(field NativeDeviceField[string]) *string {
	if !field.Present || field.Null {
		return nil
	}
	value := field.Value
	return &value
}

func mapNativeTemplateWriteError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "uq_device_access_templates_live_tenant_name" {
		return &errs.Error{Code: errs.AlreadyExists, Message: "template name already exists in this tenant"}
	}
	return err
}
