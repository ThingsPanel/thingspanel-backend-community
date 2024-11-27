package api

import (
	"net/http"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type SceneApi struct{}

func (*SceneApi) CreateScene(c *gin.Context) {
	var req model.CreateSceneReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	id, err := service.GroupApp.Scene.CreateScene(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "create scene successfully", map[string]interface{}{"scene_id": id})
}

func (*SceneApi) DeleteScene(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.Scene.DeleteScene(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "delete scene successfully", nil)
}

func (*SceneApi) UpdateScene(c *gin.Context) {
	var req model.UpdateSceneReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	id, err := service.GroupApp.Scene.UpdateScene(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "update scene successfully", map[string]interface{}{"scene_id": id})
}

func (*SceneApi) HandleScene(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.Scene.GetScene(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get scene successfully", data)
}

func (*SceneApi) HandleSceneByPage(c *gin.Context) {
	var req model.GetSceneListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Scene.GetSceneListByPage(req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get scene list successfully", data)
}

// todo 未完成
func (*SceneApi) ActiveScene(c *gin.Context) {
	id := c.Param("id")

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Scene.ActiveScene(id, userClaims.ID, userClaims.TenantID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "active scene successfully", nil)
}

func (*SceneApi) HandleSceneLog(c *gin.Context) {
	var req model.GetSceneLogListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.Scene.GetSceneLog(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get scene log successfully", data)
}
