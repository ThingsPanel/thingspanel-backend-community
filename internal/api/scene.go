package api

import (
	"project/internal/model"
	"project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type SceneApi struct{}

// /api/v1/scene [post]
func (*SceneApi) CreateScene(c *gin.Context) {
	var req model.CreateSceneReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	id, err := service.GroupApp.Scene.CreateScene(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", map[string]interface{}{"scene_id": id})
}

// /api/v1/scene [delete]
func (*SceneApi) DeleteScene(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.Scene.DeleteScene(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/scene [put]
func (*SceneApi) UpdateScene(c *gin.Context) {
	var req model.UpdateSceneReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	id, err := service.GroupApp.Scene.UpdateScene(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", map[string]interface{}{"scene_id": id})
}

// /api/v1/scene/detail/{id} [get]
func (*SceneApi) HandleScene(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.Scene.GetScene(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/scene [get]
func (*SceneApi) HandleSceneByPage(c *gin.Context) {
	var req model.GetSceneListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Scene.GetSceneListByPage(req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/scene/active/{id} [post]
// todo 未完成
func (*SceneApi) ActiveScene(c *gin.Context) {
	id := c.Param("id")

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Scene.ActiveScene(id, userClaims.ID, userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/scene/log [get]
func (*SceneApi) HandleSceneLog(c *gin.Context) {
	var req model.GetSceneLogListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.Scene.GetSceneLog(req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}
