package api

import (
	"project/internal/dal"
	"project/internal/middleware"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type ServiceAccessApi struct{}

// Empty tenant on a verified managed identity means the platform administrator installed one shared service
// instance. That instance may consume every tenant's access points. Ordinary managed identities remain limited to
// their own tenant, and user-facing service-access handlers below continue to apply their existing tenant checks.
func pluginIdentityCanAccessTenant(identity *middleware.PluginRuntimeIdentity, tenant string) bool {
	if identity == nil || tenant == "" {
		return false
	}
	return identity.TenantID == "" || identity.TenantID == tenant
}

// /api/v1/service/access [post]
func (*ServiceAccessApi) Create(c *gin.Context) {
	var req model.CreateAccessReq
	if !BindAndValidate(c, &req) {
		return
	}
	if key := c.GetHeader("Idempotency-Key"); key != "" {
		if req.OperationKey != "" && req.OperationKey != key {
			c.Error(errcode.NewWithMessage(errcode.CodeParamError, "Idempotency-Key and operation_key must match"))
			return
		}
		req.OperationKey = key
	}
	claimsValue, _ := c.Get("claims")
	userClaims, _ := claimsValue.(*utils.UserClaims)
	resp, err := service.GroupApp.ServiceAccess.CreateAccessContext(c.Request.Context(), &req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// /api/v1/service/access/operations/:key [get]
func (*ServiceAccessApi) HandleCreateOperation(c *gin.Context) {
	claimsValue, _ := c.Get("claims")
	claims, _ := claimsValue.(*utils.UserClaims)
	observation, err := service.GroupApp.ServiceAccess.GetCreateOperation(c.Request.Context(), c.Param("key"), claims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", observation)
}

// /api/v1/service/access/list
func (*ServiceAccessApi) HandleList(c *gin.Context) {
	var req model.GetServiceAccessByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	claimsValue, _ := c.Get("claims")
	userClaims, _ := claimsValue.(*utils.UserClaims)
	resp, err := service.GroupApp.ServiceAccess.List(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// /api/v1/service/access [put]
func (*ServiceAccessApi) Update(c *gin.Context) {
	var req model.UpdateAccessReq
	if !BindAndValidate(c, &req) {
		return
	}
	claimsValue, _ := c.Get("claims")
	claims, _ := claimsValue.(*utils.UserClaims)
	err := service.GroupApp.ServiceAccess.Update(&req, claims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/service/access/:id [delete]
func (*ServiceAccessApi) Delete(c *gin.Context) {
	id := c.Param("id")
	claimsValue, _ := c.Get("claims")
	claims, _ := claimsValue.(*utils.UserClaims)
	err := service.GroupApp.ServiceAccess.Delete(id, claims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/service/access/voucher/form [get]
// 服务接入点凭证表单查询
func (*ServiceAccessApi) HandleVoucherForm(c *gin.Context) {
	var req model.GetServiceAccessVoucherFormReq
	if !BindAndValidate(c, &req) {
		return
	}
	resp, err := service.GroupApp.ServiceAccess.GetVoucherForm(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// /api/v1/service/access/device/list
// 三方服务设备列表查询
func (*ServiceAccessApi) HandleDeviceList(c *gin.Context) {
	var req model.ServiceAccessDeviceListReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ServiceAccess.GetServiceAccessDeviceList(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// /api/v1/plugin/service/access/list
// 服务接入点插件列表查询
func (*ServiceAccessApi) HandlePluginServiceAccessList(c *gin.Context) {
	logrus.Info("get plugin list")
	var req model.GetPluginServiceAccessListReq
	if !BindAndValidate(c, &req) {
		return
	}
	identity, ok := requirePluginService(c, req.ServiceIdentifier)
	if !ok {
		return
	}
	resp, err := service.GroupApp.ServiceAccess.GetPluginServiceAccessList(&req)
	if err != nil {
		c.Error(err)
		return
	}
	// Platform-installed service instances consume each tenant's access points; tenant-issued identities stay local.
	if list, isList := resp.([]map[string]interface{}); isList && identity != nil {
		own := make([]map[string]interface{}, 0, len(list))
		for _, item := range list {
			if tenant, _ := item["tenant_id"].(string); pluginIdentityCanAccessTenant(identity, tenant) {
				own = append(own, item)
			}
		}
		resp = own
	}
	c.Set("data", resp)
}

// /api/v1/pugin/service/access
func (*ServiceAccessApi) HandlePluginServiceAccess(c *gin.Context) {
	var req model.GetPluginServiceAccessReq
	if !BindAndValidate(c, &req) {
		return
	}
	accessRow, err := dal.GetServiceAccessByID(req.ServiceAccessID)
	if err != nil {
		c.Error(err)
		return
	}
	plugin, err := dal.GetServicePluginByID(accessRow.ServicePluginID)
	if err != nil || plugin == nil {
		scopeDenied(c)
		return
	}
	identity, ok := requirePluginService(c, plugin.ServiceIdentifier)
	if !ok {
		return
	}
	resp, err := service.GroupApp.ServiceAccess.GetPluginServiceAccess(&req)
	if err != nil {
		c.Error(err)
		return
	}
	if identity == nil {
		c.Set("data", resp)
		return
	}
	// The access point must belong to the identity's scope and to the identity's own service plugin.
	access, isMap := resp.(map[string]interface{})
	tenant, _ := access["tenant_id"].(string)
	pluginID, _ := access["service_plugin_id"].(string)
	own, ownErr := dal.GetServicePluginByServiceIdentifier(identity.ServiceIdentifier)
	if !isMap || !pluginIdentityCanAccessTenant(identity, tenant) || ownErr != nil || own == nil || own.ID != pluginID {
		scopeDenied(c)
		return
	}
	c.Set("data", resp)
}
