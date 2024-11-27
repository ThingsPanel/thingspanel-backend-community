package api

import (
	"net/http"

	model "project/internal/model"
	service "project/internal/service"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type DictApi struct{}

// CreateDictColumn 创建字典列
// @Router   /api/v1/dict/column [post]
func (*DictApi) CreateDictColumn(c *gin.Context) {

	var createDictReq model.CreateDictReq
	if !BindAndValidate(c, &createDictReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Dict.CreateDictColumn(&createDictReq, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Dict created successfully", nil)
}

// CreateDictColumn 创建字典多语言
// @Router   /api/v1/dict/language [post]
func (*DictApi) CreateDictLanguage(c *gin.Context) {

	var createDictLanguageReq model.CreateDictLanguageReq
	if !BindAndValidate(c, &createDictLanguageReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Dict.CreateDictLanguage(&createDictLanguageReq, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Dict language created successfully", nil)
}

// DeleteDictColumn 删除字典列
// @Router   /api/v1/dict/column/{id} [delete]
func (*DictApi) DeleteDictColumn(c *gin.Context) {
	id := c.Param("id")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Dict.DeleteDict(id, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete dict successfully", nil)
}

// DeleteDictLanguage 删除字典多语言Handle
// @Router   /api/v1/dict/language/{id} [delete]
func (*DictApi) DeleteDictLanguage(c *gin.Context) {
	id := c.Param("id")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Dict.DeleteDictLanguage(id, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete dict language successfully", nil)
}

// CreateDictColumn 枚举查询接口
// @Router   /api/v1/dict/enum [get]
func (*DictApi) HandleDict(c *gin.Context) {
	var dictEnum model.DictListReq
	if !BindAndValidate(c, &dictEnum) {
		return
	}
	list, err := service.GroupApp.Dict.GetDict(&dictEnum)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Dict get successfully", list)
}

// 协议服务下拉菜单查询接口
// /api/v1/dict/protocol/service
func (*DictApi) HandleProtocolAndService(c *gin.Context) {
	var protocolMenuReq model.ProtocolMenuReq
	if !BindAndValidate(c, &protocolMenuReq) {
		return
	}
	list, err := service.GroupApp.Dict.GetProtocolMenu(&protocolMenuReq)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Dict get successfully", list)
}

// GetDictLanguage 字典多语言查询
// @Router   /api/v1/dict/language/{id} [get]
func (*DictApi) HandleDictLanguage(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.Dict.GetDictLanguageListById(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	// {
	// 	"code": 200,
	// 	"message": "Get dict language successfully",
	// 	"data": [
	// 		{
	// 			"id": "1a773a0d-c06b-1f48-c236-4e2863c831d6",
	// 			"dict_id": "9bed09fa-b821-343d-2f41-6a91947d7132",
	// 			"language_code": "zh",
	// 			"translation": "男"
	// 		}
	// 	]
	// }

	SuccessHandler(c, "Get dict language successfully", data)
}

// GetDictLisyByPage 字典列表分页查询
// @Router   /api/v1/dict [get]
func (*DictApi) HandleDictLisyByPage(c *gin.Context) {
	var byList model.GetDictLisyByPageReq
	if !BindAndValidate(c, &byList) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)

	list, err := service.GroupApp.Dict.GetDictListByPage(&byList, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get dict list successfully", list)
}
