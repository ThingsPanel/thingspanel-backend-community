package api

import (
	"fmt"
	"net/http"

	model "project/internal/model"
	service "project/internal/service"

	"github.com/gin-gonic/gin"
)

type CasbinApi struct{}

var casbinService = service.GroupApp.Casbin

// AddFunctionToRole 角色添加多个权限
// @Tags     权限
// @Summary  角色添加权限
// @Description 角色添加权限
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.FunctionsRoleValidate   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "角色添加权限成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/casbin/function [post]
func (*CasbinApi) AddFunctionToRole(c *gin.Context) {
	var req model.FunctionsRoleValidate
	if !BindAndValidate(c, &req) {
		return
	}

	ok := casbinService.AddFunctionToRole(req.RoleID, req.FunctionsIDs)
	if !ok {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("failed"))
		return
	}

	SuccessHandler(c, "AddFunctionToRole successfully", nil)
}

// GetFunctionFromRole 查询角色的权限
// @Tags     权限
// @Summary  查询角色的权限
// @Description 查询角色的权限
// @accept    application/json
// @Produce   application/json
// @Param   data query model.RoleValidate true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/casbin/function [get]
func (*CasbinApi) GetFunctionFromRole(c *gin.Context) {
	var req model.RoleValidate
	if !BindAndValidate(c, &req) {
		return
	}

	roles, ok := casbinService.GetFunctionFromRole(req.RoleID)
	if !ok {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("failed"))
		return
	}

	SuccessHandler(c, "GetFunctionFromRole successfully", roles)
}

// UpdateFunctionFromRole 修改角色的权限
// @Tags     权限
// @Summary  修改角色的权限
// @Description 修改角色的权限
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.FunctionsRoleValidate   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "修改角色的权限成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/casbin/function [put]
func (*CasbinApi) UpdateFunctionFromRole(c *gin.Context) {
	var req model.FunctionsRoleValidate
	if !BindAndValidate(c, &req) {
		return
	}

	if req.RoleID == "" && req.FunctionsIDs == nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "message": "修改内容不能为空"})
		return
	}

	f, _ := casbinService.GetFunctionFromRole(req.RoleID)
	if len(f) > 0 {
		//没有记录删除会返回false
		ok := casbinService.RemoveRoleAndFunction(req.RoleID)
		if !ok {
			ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("failed"))
			return
		}
	}
	ok := casbinService.AddFunctionToRole(req.RoleID, req.FunctionsIDs)
	if !ok {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("failed"))
	}
	SuccessHandler(c, "Update role successfully", nil)
}

// DeleteFunctionFromRole 删除角色的权限
// @Tags     权限
// @Summary  删除角色的权限
// @Description 删除角色的权限
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "权限id"
// @Success  200  {object}  ApiResponse  "删除角色的权限成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/casbin/function/{id} [delete]
func (*CasbinApi) DeleteFunctionFromRole(c *gin.Context) {
	id := c.Param("id")
	ok := casbinService.RemoveRoleAndFunction(id)
	if !ok {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("failed"))
		return
	}
	SuccessHandler(c, "Delete role successfully", nil)
}

// AddRoleToUser 用户添加多个角色
// @Tags     权限
// @Summary  用户添加多个角色
// @Description 用户添加多个角色
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.RolesUserValidate   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "增加权限成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/casbin/user [post]
func (*CasbinApi) AddRoleToUser(c *gin.Context) {
	var req model.RolesUserValidate
	if !BindAndValidate(c, &req) {
		return
	}

	ok := casbinService.AddRolesToUser(req.UserID, req.RolesIDs)
	if !ok {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("failed"))
		return
	}

	SuccessHandler(c, "AddRoleToUser successfully", nil)

}

// GetRolesFromUser 查询用户的角色
// @Tags     权限
// @Summary  查询用户的角色
// @Description 查询用户的角色
// @accept    application/json
// @Produce   application/json
// @Param   data query model.UserValidate true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/casbin/user [get]
func (*CasbinApi) GetRolesFromUser(c *gin.Context) {
	var req model.UserValidate
	if !BindAndValidate(c, &req) {
		return
	}

	roles, ok := casbinService.GetRoleFromUser(req.UserID)
	if !ok {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("failed"))
		return
	}

	SuccessHandler(c, "GetRolesFromUser successfully", roles)

}

// UpdateRolesFromUser 修改用户的角色
// @Tags     权限
// @Summary  修改用户的角色
// @Description 修改用户的角色
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.RolesUserValidate   true  "角色用户关系"
// @Success  200  {object}  ApiResponse  "修改用户的角色成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/casbin/user [put]
func (*CasbinApi) UpdateRolesFromUser(c *gin.Context) {
	var req model.RolesUserValidate
	if !BindAndValidate(c, &req) {
		return
	}

	if req.UserID == "" && req.RolesIDs == nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "message": "修改内容不能为空"})
		return
	}

	casbinService.RemoveUserAndRole(req.UserID)
	ok := casbinService.AddRolesToUser(req.UserID, req.RolesIDs)
	if !ok {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("failed"))
	}
	SuccessHandler(c, "UpdateRolesFromUser successfully", nil)
}

// DeleteRolesFromUser 删除用户的角色
// @Tags     权限
// @Summary  删除用户的角色
// @Description 删除用户的角色
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "角色ID"
// @Success  200  {object}  ApiResponse  "删除用户的角色成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/casbin/user/{id} [delete]
func (*CasbinApi) DeleteRolesFromUser(c *gin.Context) {
	id := c.Param("id")
	ok := casbinService.RemoveUserAndRole(id)
	if !ok {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("failed"))
		return
	}
	SuccessHandler(c, "DeleteRolesFromUser successfully", nil)
}
