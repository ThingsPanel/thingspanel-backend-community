package notification

import (
	"context"
	"strings"

	"encore.app/l1-access/authn"
	"encore.app/l3-core/notification/contract"
)

// Platform instance operations use a separately verified platform principal
// and an explicit tenant path target. The selected tenant is never copied
// into authn.Data or used to impersonate a tenant principal.
func (s *ManagementService) platformTenant(ctx context.Context, data *authn.Data, bearer, tenantID string) (UserPrincipal, error) {
	principal, err := s.verifiedPrincipal(ctx, data, bearer)
	if err != nil {
		return UserPrincipal{}, ErrIdentityUnauthorized
	}
	if err := s.checkPlatformTenant(ctx, principal, tenantID); err != nil {
		return UserPrincipal{}, err
	}
	return principal, nil
}

func (s *ManagementService) checkPlatformTenant(ctx context.Context, principal UserPrincipal, tenantID string) error {
	if !containsRole(principal.Roles, "SYS_ADMIN") {
		return ErrIdentityForbidden
	}
	if !validPlatformTenantID(tenantID) {
		return ErrManagementInvalid
	}
	if s.tenantDirectory == nil {
		return ErrManagementUnavailable
	}
	exists, err := s.tenantDirectory.Exists(ctx, tenantID)
	if err != nil {
		return ErrManagementUnavailable
	}
	if !exists {
		return ErrManagementNotFound
	}
	return nil
}

func validPlatformTenantID(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func (s *ManagementService) ListPlatformTenantPlugins(ctx context.Context, data *authn.Data, bearer, tenantID string) ([]contract.PluginView, error) {
	if _, err := s.platformTenant(ctx, data, bearer, tenantID); err != nil {
		return nil, err
	}
	plugins, err := s.plugins.ListPlugins(ctx, tenantID)
	if err != nil {
		return nil, mapManagementError(err)
	}
	return plugins, nil
}

func (s *ManagementService) ListPlatformTenantInstances(ctx context.Context, data *authn.Data, bearer, tenantID string) ([]contract.InstanceView, error) {
	if _, err := s.platformTenant(ctx, data, bearer, tenantID); err != nil {
		return nil, err
	}
	items, err := s.instances.ListInstances(ctx, tenantID)
	if err != nil {
		return nil, mapManagementError(err)
	}
	return items, nil
}

func (s *ManagementService) GetPlatformTenantInstance(ctx context.Context, data *authn.Data, bearer, tenantID, instanceID string) (contract.InstanceView, error) {
	if _, err := s.platformTenant(ctx, data, bearer, tenantID); err != nil {
		return contract.InstanceView{}, err
	}
	if instanceID == "" {
		return contract.InstanceView{}, ErrManagementNotFound
	}
	view, err := s.instances.GetInstance(ctx, tenantID, instanceID)
	if err != nil {
		return contract.InstanceView{}, mapManagementError(err)
	}
	return view, nil
}

func (s *ManagementService) CreatePlatformTenantInstance(ctx context.Context, data *authn.Data, bearer, tenantID, idempotencyKey, requestDigest string, request contract.InstanceCreate) (contract.InstanceView, error) {
	if _, err := s.platformTenant(ctx, data, bearer, tenantID); err != nil {
		return contract.InstanceView{}, err
	}
	return s.createInstanceInTenant(ctx, tenantID, idempotencyKey, requestDigest, request)
}

func (s *ManagementService) UpdatePlatformTenantInstance(ctx context.Context, data *authn.Data, bearer, tenantID, instanceID, idempotencyKey, requestDigest string, update contract.InstanceUpdate) (contract.InstanceView, error) {
	if _, err := s.platformTenant(ctx, data, bearer, tenantID); err != nil {
		return contract.InstanceView{}, err
	}
	return s.updateInstanceInTenant(ctx, tenantID, instanceID, idempotencyKey, requestDigest, update)
}

func (s *ManagementService) ValidatePlatformTenantInstance(ctx context.Context, data *authn.Data, bearer, tenantID string, request contract.InstanceValidate) (contract.PluginValidateResultV1, error) {
	if _, err := s.platformTenant(ctx, data, bearer, tenantID); err != nil {
		return contract.PluginValidateResultV1{}, err
	}
	return s.validateInstanceInTenant(ctx, tenantID, request)
}

func (s *ManagementService) RotatePlatformTenantReceiptCredential(ctx context.Context, data *authn.Data, bearer, tenantID, instanceID, idempotencyKey, requestDigest string) (contract.ReceiptCredentialResponse, error) {
	if _, err := s.platformTenant(ctx, data, bearer, tenantID); err != nil {
		return contract.ReceiptCredentialResponse{}, err
	}
	return s.rotateReceiptCredentialInTenant(ctx, tenantID, instanceID, idempotencyKey, requestDigest)
}
