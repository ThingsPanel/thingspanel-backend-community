package devicemetadata

import (
	"database/sql"
	"encore.app/pkg/dbx"
	"errors"
)

// NativeImportedAccess is an allowlisted, non-secret projection of the retained
// source configuration. It is not an editable native provider configuration.
type NativeImportedAccess struct {
	SourceOwner        string  `json:"sourceOwner"`
	SourceDeviceID     string  `json:"sourceDeviceId"`
	SourceTemplateID   *string `json:"sourceTemplateId"`
	SourceTemplateName *string `json:"sourceTemplateName"`
	Protocol           *string `json:"protocol"`
	AccessWay          *string `json:"accessWay"`
	ServiceAccessID    *string `json:"serviceAccessId"`
	ReadOnly           bool    `json:"readOnly"`
}

const nativeImportedAccessQuery = `SELECT source_owner,source_device_id,source_template_id,
 source_template_name,source_protocol,source_access_way,source_service_access_id
 FROM imported_device_origins WHERE tenant_id=$1 AND device_id=$2`

type importedAccessRow interface{ Scan(...any) error }

func scanNativeImportedAccess(row importedAccessRow) (*NativeImportedAccess, error) {
	var out NativeImportedAccess
	var templateID, templateName, protocol, accessWay, serviceID sql.NullString
	err := row.Scan(&out.SourceOwner, &out.SourceDeviceID, &templateID, &templateName, &protocol, &accessWay, &serviceID)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, dbx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapDBErr(err, "read imported device access failed")
	}
	out.SourceTemplateID = nullableStringPointer(templateID)
	out.SourceTemplateName = nullableStringPointer(templateName)
	out.Protocol = nullableStringPointer(protocol)
	out.AccessWay = nullableStringPointer(accessWay)
	out.ServiceAccessID = nullableStringPointer(serviceID)
	out.ReadOnly = true
	return &out, nil
}
