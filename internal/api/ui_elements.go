package api

import (
	"net/http"

	model "project/internal/model"
	service "project/internal/service"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type UiElementsApi struct{}

// CreateUiElements 创建ui元素控制
// @Tags     ui元素控制
// @Summary  创建ui元素控制
// @Description 创建ui元素控制
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateUiElementsReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "创建ui元素控制成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/ui_elements [post]
func (api *UiElementsApi) CreateUiElements(c *gin.Context) {
	var req model.CreateUiElementsReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.UiElements.CreateUiElements(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Create UiElements successfully", nil)
}

// UpdateUiElements 更新ui元素控制
// @Tags     ui元素控制
// @Summary  更新ui元素控制
// @Description 更新ui元素控制
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateUiElementsReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "更新ui元素控制成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/ui_elements [put]
func (api *UiElementsApi) UpdateUiElements(c *gin.Context) {
	var req model.UpdateUiElementsReq
	if !BindAndValidate(c, &req) {
		return
	}

	if req.ElementType == nil && req.Authority == nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "message": "修改内容不能为空"})
		return
	}

	err := service.GroupApp.UiElements.UpdateUiElements(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update UiElements successfully", nil)
}

// DeleteUiElements 删除ui元素控制
// @Tags     ui元素控制
// @Summary  删除ui元素控制
// @Description 删除ui元素控制
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "字典ID"
// @Success  200  {object}  ApiResponse  "更新ui元素控制成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/ui_elements/{id} [delete]
func (api *UiElementsApi) DeleteUiElements(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.UiElements.DeleteUiElements(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete UiElements successfully", nil)
}

// GetUiElementsListByPage ui元素控制分页查询
// @Tags     ui元素控制
// @Summary  ui元素控制分页查询
// @Description ui元素控制分页查询
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetUiElementsListByPageReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/ui_elements [get]
func (api *UiElementsApi) GetUiElementsListByPage(c *gin.Context) {
	var req model.GetUiElementsListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	UiElementsList, err := service.GroupApp.UiElements.GetUiElementsListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get UiElements list successfully", UiElementsList)
}

// GetUiElementsListByPage 根据用户权限查询ui元素
// @Tags     ui元素控制
// @Summary  根据用户权限查询ui元素
// @Description 根据用户权限查询ui元素
// @Description  返回数据格式举例：
// @Description    "parent_id": "0", //根，string 父元素id
// @Description    "authority": "[\"SYS_ADMIN\",\"TENANT_ADMIN\"]", //json 权限为users表中的枚举值:TENANT_ADMIN-租户管理员 TENANT_USER-租户用户 SYS_ADMIN-系统管理员
// @Description    "element_type": 1,//int 元素类型1-菜单 2-目录 3-按钮 4-路由
// @Description  	"orders": 1,//int
// @Description  	其他值
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/ui_elements/menu [get]
func (api *UiElementsApi) GetUiElementsListByAuthority(c *gin.Context) {
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	uiElementsList, err := service.GroupApp.UiElements.GetUiElementsListByAuthority(userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get UiElements list successfully", uiElementsList)
}

// 菜单权限配置表单
// /api/v1/ui_elements/select/form
func (api *UiElementsApi) GetUiElementsListByTenant(c *gin.Context) {
	uiElementsList, err := service.GroupApp.UiElements.GetTenantUiElementsList()
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get UiElements list successfully", uiElementsList)
}
