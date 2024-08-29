package api

import (
	"fmt"
	"net/http"

	model "project/internal/model"
	service "project/service"
	utils "project/utils"

	"github.com/gin-gonic/gin"
)

type RoleApi struct{}

// CreateRole 创建角色管理
// @Tags     角色管理
// @Summary  创建角色管理
// @Description 创建角色管理
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateRoleReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "创建角色管理成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/role [post]
func (api *RoleApi) CreateRole(c *gin.Context) {
	var req model.CreateRoleReq
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.Role.CreateRole(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Create role successfully", nil)
}

// UpdateRole 更新角色管理
// @Tags     角色管理
// @Summary  更新角色管理
// @Description 更新角色管理
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateRoleReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "更新角色管理成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/role [put]
func (api *RoleApi) UpdateRole(c *gin.Context) {
	var req model.UpdateRoleReq
	if !BindAndValidate(c, &req) {
		return
	}

	if req.Description == nil && req.Name == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "message": "修改内容不能为空"})
		return
	}

	data, err := service.GroupApp.Role.UpdateRole(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update role successfully", data)
}

// DeleteRole 删除角色管理
// @Tags     角色管理
// @Summary  删除角色管理
// @Description 删除角色管理
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "ID"
// @Success  200  {object}  ApiResponse  "删除角色成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/role/{id} [delete]
func (api *RoleApi) DeleteRole(c *gin.Context) {
	id := c.Param("id")

	// 需要角色没有被用户使用
	if service.GroupApp.Casbin.HasRole(id) {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("role has user delete failed,The role is bound by the user"))
		return
	}

	err := service.GroupApp.Role.DeleteRole(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete role successfully", nil)
}

// GetRoleListByPage 角色管理分页查询
// @Tags     角色管理
// @Summary  角色管理分页查询
// @Description 角色管理分页查询
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetRoleListByPageReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/role [get]
func (api *RoleApi) GetRoleListByPage(c *gin.Context) {
	var req model.GetRoleListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	roleList, err := service.GroupApp.Role.GetRoleListByPage(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get role list successfully", roleList)
}
