package service

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"project/internal/model"
	"project/pkg/global"
)

var ErrDictionaryPermissionDenied = errors.New("current dictionary permission denied")
var ErrDictionaryPermissionUnavailable = errors.New("current dictionary permission unavailable")

func dictionaryAuthorizationResource(operation string) (resource string, administrator bool) {
	switch operation {
	case "create-column":
		return "api/v1/dict/column", true
	case "create-language":
		return "api/v1/dict/language", true
	case "read-enum":
		return "api/v1/dict/enum", false
	case "read-page":
		return "api/v1/dict", true
	case "read-languages":
		return "api/v1/dict/language/:id", false
	case "delete-column":
		return "api/v1/dict/column/:id", true
	case "delete-language":
		return "api/v1/dict/language/:id", true
	case "read-protocols":
		return "api/v1/dict/protocol/service", false
	default:
		return "", false
	}
}

// AuthorizeCurrentDictionaryOperation uses the existing identity and policy
// owner only. The dictionary business tables are never read or written here.
// The trusted caller must already have validated a current native/user session.
func AuthorizeCurrentDictionaryOperation(ctx context.Context, request model.DictionaryAuthorizationRequest) (*model.DictionaryAuthorizationResponse, error) {
	resource, administrator := dictionaryAuthorizationResource(request.Operation)
	if request.ProtocolVersion != 1 || resource == "" || (request.TenantID != "" && strings.TrimSpace(request.TenantID) == "") || strings.TrimSpace(request.SubjectID) == "" {
		return nil, ErrDictionaryPermissionDenied
	}
	if ctx == nil || global.DB == nil || global.CasbinEnforcer == nil {
		return nil, ErrDictionaryPermissionUnavailable
	}
	var user struct {
		ID        string
		TenantID  *string
		Authority *string
		Status    *string
	}
	err := global.DB.WithContext(ctx).Table("users").Select("id,tenant_id,authority,status").Where("id = ?", request.SubjectID).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDictionaryPermissionDenied
	}
	if err != nil {
		return nil, ErrDictionaryPermissionUnavailable
	}
	if user.ID != request.SubjectID || user.Status == nil || *user.Status != "N" || user.Authority == nil {
		return nil, ErrDictionaryPermissionDenied
	}
	// authcore reads COALESCE(tenant_id,'') and admits an empty tenant only for
	// the existing SYS_ADMIN authority. Do not invent a synthetic tenant or
	// accept a client-supplied platform role as proof of this exception.
	actualTenant := ""
	if user.TenantID != nil {
		actualTenant = *user.TenantID
	}
	if actualTenant != request.TenantID || (actualTenant == "" && *user.Authority != "SYS_ADMIN") {
		return nil, ErrDictionaryPermissionDenied
	}
	switch *user.Authority {
	case "SYS_ADMIN", "TENANT_ADMIN", "TENANT_USER":
	default:
		return nil, ErrDictionaryPermissionDenied
	}
	if administrator && *user.Authority != "SYS_ADMIN" {
		return nil, ErrDictionaryPermissionDenied
	}
	if !(new(Casbin)).GetUrl(resource) {
		return nil, ErrDictionaryPermissionDenied
	}
	allowed, err := global.CasbinEnforcer.Enforce(request.SubjectID, resource, "allow")
	if err != nil {
		return nil, ErrDictionaryPermissionUnavailable
	}
	if !allowed {
		return nil, ErrDictionaryPermissionDenied
	}
	return &model.DictionaryAuthorizationResponse{ProtocolVersion: 1, Authorized: true, TenantID: request.TenantID, SubjectID: request.SubjectID, Operation: request.Operation, Resource: resource}, nil
}
