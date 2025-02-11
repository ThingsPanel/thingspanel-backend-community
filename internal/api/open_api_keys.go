// internal/api/open_api_keys.go
package api

import (
	"project/internal/model"
	"project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type OpenAPIKeyApi struct{}

// CreateOpenAPIKey 创建API密钥
// @Router /api/v1/open/keys [post]
func (*OpenAPIKeyApi) CreateOpenAPIKey(c *gin.Context) {
	var createReq model.CreateOpenAPIKeyReq
	if !BindAndValidate(c, &createReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.OpenAPIKey.CreateOpenAPIKey(&createReq, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// GetOpenAPIKeyList 获取API密钥列表
// @Router /api/v1/open/keys [get]
func (*OpenAPIKeyApi) GetOpenAPIKeyList(c *gin.Context) {
	var listReq model.OpenAPIKeyListReq
	if !BindAndValidate(c, &listReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	list, err := service.GroupApp.OpenAPIKey.GetOpenAPIKeyList(&listReq, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", list)
}

// UpdateOpenAPIKey 更新API密钥
// @Router /api/v1/open/keys [put]
func (*OpenAPIKeyApi) UpdateOpenAPIKey(c *gin.Context) {
	var updateReq model.UpdateOpenAPIKeyReq
	if !BindAndValidate(c, &updateReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.OpenAPIKey.UpdateOpenAPIKey(&updateReq, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// DeleteOpenAPIKey 删除API密钥
// @Router /api/v1/open/keys/{id} [delete]
func (*OpenAPIKeyApi) DeleteOpenAPIKey(c *gin.Context) {
	id := c.Param("id")

	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	err := service.GroupApp.OpenAPIKey.DeleteOpenAPIKey(id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}
