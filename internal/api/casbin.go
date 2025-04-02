package api

import (
	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/errcode"

	"github.com/gin-gonic/gin"
)

type CasbinApi struct{}

var casbinService = service.GroupApp.Casbin

// AddFunctionToRole 角色添加多个权限
// @Router   /api/v1/casbin/function [post]
func (*CasbinApi) AddFunctionToRole(c *gin.Context) {
	var req model.FunctionsRoleValidate
	if !BindAndValidate(c, &req) {
		return
	}

	ok := casbinService.AddFunctionToRole(req.RoleID, req.FunctionsIDs)
	if !ok {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"role_id":      req.RoleID,
			"function_ids": req.FunctionsIDs,
			"error":        "AddFunctionToRole failed",
		}))
		return
	}

	c.Set("data", nil)
}

// GetFunctionFromRole 查询角色的权限
// @Router   /api/v1/casbin/function [get]
func (*CasbinApi) HandleFunctionFromRole(c *gin.Context) {
	var req model.RoleValidate
	if !BindAndValidate(c, &req) {
		return
	}

	roles, ok := casbinService.GetFunctionFromRole(req.RoleID)
	if !ok {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"role_id": req.RoleID,
			"error":   "GetFunctionFromRole failed",
		}))
		return
	}

	c.Set("data", roles)
}

// UpdateFunctionFromRole 修改角色的权限
// @Router   /api/v1/casbin/function [put]
func (*CasbinApi) UpdateFunctionFromRole(c *gin.Context) {
	var req model.FunctionsRoleValidate
	if !BindAndValidate(c, &req) {
		return
	}

	if req.RoleID == "" && req.FunctionsIDs == nil {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"role_id":      req.RoleID,
			"function_ids": req.FunctionsIDs,
			"error":        "UpdateFunctionFromRole failed",
		}))
		return
	}

	f, _ := casbinService.GetFunctionFromRole(req.RoleID)
	if len(f) > 0 {
		//没有记录删除会返回false
		ok := casbinService.RemoveRoleAndFunction(req.RoleID)
		if !ok {
			c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
				"role_id": req.RoleID,
				"error":   "RemoveRoleAndFunction failed",
			}))
			return
		}
	}
	ok := casbinService.AddFunctionToRole(req.RoleID, req.FunctionsIDs)
	if !ok {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"role_id":      req.RoleID,
			"function_ids": req.FunctionsIDs,
			"error":        "AddFunctionToRole failed",
		}))
	}
	c.Set("data", nil)
}

// DeleteFunctionFromRole 删除角色的权限
// @Router   /api/v1/casbin/function/{id} [delete]
func (*CasbinApi) DeleteFunctionFromRole(c *gin.Context) {
	id := c.Param("id")
	ok := casbinService.RemoveRoleAndFunction(id)
	if !ok {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"role_id": id,
			"error":   "RemoveRoleAndFunction failed",
		}))
		return
	}
	c.Set("data", nil)
}

// AddRoleToUser 用户添加多个角色
// @Router   /api/v1/casbin/user [post]
func (*CasbinApi) AddRoleToUser(c *gin.Context) {
	var req model.RolesUserValidate
	if !BindAndValidate(c, &req) {
		return
	}

	ok := casbinService.AddRolesToUser(req.UserID, req.RolesIDs)
	if !ok {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"user_id": req.UserID,
			"role_id": req.RolesIDs,
			"error":   "AddRolesToUser failed",
		}))
		return
	}

	c.Set("data", nil)

}

// GetRolesFromUser 查询用户的角色
// @Router   /api/v1/casbin/user [get]
func (*CasbinApi) HandleRolesFromUser(c *gin.Context) {
	var req model.UserValidate
	if !BindAndValidate(c, &req) {
		return
	}

	roles, ok := casbinService.GetRoleFromUser(req.UserID)
	if !ok {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"user_id": req.UserID,
			"error":   "GetRoleFromUser failed",
		}))
		return
	}

	c.Set("data", roles)

}

// UpdateRolesFromUser 修改用户的角色
// @Router   /api/v1/casbin/user [put]
func (*CasbinApi) UpdateRolesFromUser(c *gin.Context) {
	var req model.RolesUserValidate
	if !BindAndValidate(c, &req) {
		return
	}

	if req.UserID == "" && req.RolesIDs == nil {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"user_id": req.UserID,
			"role_id": req.RolesIDs,
			"error":   "UpdateRolesFromUser failed",
		}))
		return
	}

	casbinService.RemoveUserAndRole(req.UserID)
	ok := casbinService.AddRolesToUser(req.UserID, req.RolesIDs)
	if !ok {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"user_id": req.UserID,
			"role_id": req.RolesIDs,
			"error":   "AddRolesToUser failed",
		}))
	}
	c.Set("data", nil)
}

// DeleteRolesFromUser 删除用户的角色
// @Router   /api/v1/casbin/user/{id} [delete]
func (*CasbinApi) DeleteRolesFromUser(c *gin.Context) {
	id := c.Param("id")
	ok := casbinService.RemoveUserAndRole(id)
	if !ok {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"user_id": id,
			"error":   "RemoveUserAndRole failed",
		}))
		return
	}
	c.Set("data", nil)
}
