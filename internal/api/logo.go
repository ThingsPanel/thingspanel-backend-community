package api

import (
	model "project/internal/model"
	service "project/internal/service"

	"github.com/gin-gonic/gin"
)

type LogoApi struct{}

// UpdateLogo 更新常规设置设置
// @Router   /api/v1/logo [put]
func (LogoApi) UpdateLogo(c *gin.Context) {
	var req model.UpdateLogoReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.Logo.UpdateLogo(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// GetLogoListByPage 常规设置设置查询
// @Router   /api/v1/logo [get]
func (LogoApi) HandleLogoList(c *gin.Context) {
	logoList, err := service.GroupApp.Logo.GetLogoList()
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", logoList)
}
