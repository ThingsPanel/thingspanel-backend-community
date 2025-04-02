package api

import (
	"context"
	"project/internal/model"
	service "project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type ExpectedDataApi struct{}

// 预期数据列表查询
// /api/v1/expected/data/list
func (*ExpectedDataApi) HandleExpectedDataList(c *gin.Context) {
	var req model.GetExpectedDataPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ExpectedData.PageList(context.Background(), &req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// 新增预期数据
// /api/v1/expected/data POST
func (*ExpectedDataApi) CreateExpectedData(c *gin.Context) {
	var req model.CreateExpectedDataReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	resp, err := service.GroupApp.ExpectedData.Create(c, &req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", resp)
}

// 删除预期数据
// /api/v1/expected/data/{id} DELETE
func (*ExpectedDataApi) DeleteExpectedData(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.ExpectedData.Delete(c, id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", map[string]interface{}{})
}
