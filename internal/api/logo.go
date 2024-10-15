package api

import (
	"net/http"

	model "project/internal/model"
	service "project/internal/service"

	"github.com/gin-gonic/gin"
)

type LogoApi struct{}

// UpdateLogo 更新常规设置设置
// @Tags     常规设置
// @Summary  更新常规设置设置
// @Description 更新常规设置设置
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateLogoReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "更新常规设置设置成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/logo [put]
func (api *LogoApi) UpdateLogo(c *gin.Context) {
	var req model.UpdateLogoReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.Logo.UpdateLogo(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update logo successfully", nil)
}

// GetLogoListByPage 常规设置设置查询
// @Tags     常规设置
// @Summary  常规设置设置查询
// @Description 常规设置设置查询
// @accept    application/json
// @Produce   application/json
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/logo [get]
func (api *LogoApi) GetLogoList(c *gin.Context) {
	logoList, err := service.GroupApp.Logo.GetLogoList()
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get logo list successfully", logoList)
}
