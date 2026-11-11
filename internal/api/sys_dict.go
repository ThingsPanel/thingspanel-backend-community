package api

import (
	"net/http"
	"os"
	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type DictApi struct{}

// Keep the established implementation available until common acceptance permits
// final cleanup. Selecting the native owner closes every old entry before it can
// parse input, consult claims or reach legacy SQL; failures never fall back.
func dictionaryLegacyAvailable(c *gin.Context) bool {
	switch os.Getenv("TP_PLATFORMCONFIG_OWNER") {
	case "", "disabled":
		return true
	default:
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": errcode.CodeServiceUnavailable, "message": "服务暂时不可用"})
		return false
	}
}

// CreateDictColumn 创建字典列
// @Router   /api/v1/dict/column [post]
func (*DictApi) CreateDictColumn(c *gin.Context) {
	if !dictionaryLegacyAvailable(c) {
		return
	}

	var createDictReq model.CreateDictReq
	if !BindAndValidate(c, &createDictReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Dict.CreateDictColumn(&createDictReq, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// CreateDictColumn 创建字典多语言
// @Router   /api/v1/dict/language [post]
func (*DictApi) CreateDictLanguage(c *gin.Context) {
	if !dictionaryLegacyAvailable(c) {
		return
	}

	var createDictLanguageReq model.CreateDictLanguageReq
	if !BindAndValidate(c, &createDictLanguageReq) {
		return
	}

	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Dict.CreateDictLanguage(&createDictLanguageReq, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// DeleteDictColumn 删除字典列
// @Router   /api/v1/dict/column/{id} [delete]
func (*DictApi) DeleteDictColumn(c *gin.Context) {
	if !dictionaryLegacyAvailable(c) {
		return
	}
	id := c.Param("id")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Dict.DeleteDict(id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// DeleteDictLanguage 删除字典多语言Handle
// @Router   /api/v1/dict/language/{id} [delete]
func (*DictApi) DeleteDictLanguage(c *gin.Context) {
	if !dictionaryLegacyAvailable(c) {
		return
	}
	id := c.Param("id")
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.Dict.DeleteDictLanguage(id, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// CreateDictColumn 枚举查询接口
// @Router   /api/v1/dict/enum [get]
func (*DictApi) HandleDict(c *gin.Context) {
	if !dictionaryLegacyAvailable(c) {
		return
	}
	var dictEnum model.DictListReq
	if !BindAndValidate(c, &dictEnum) {
		return
	}
	lang := c.GetHeader("Accept-Language")
	list, err := service.GroupApp.Dict.GetDict(&dictEnum, lang)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", list)
}

// 协议服务下拉菜单查询接口
// /api/v1/dict/protocol/service [get]
func (*DictApi) HandleProtocolAndService(c *gin.Context) {
	if !dictionaryLegacyAvailable(c) {
		return
	}
	var protocolMenuReq model.ProtocolMenuReq
	if !BindAndValidate(c, &protocolMenuReq) {
		return
	}
	list, err := service.GroupApp.Dict.GetProtocolMenu(&protocolMenuReq)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}

// GetDictLanguage 字典多语言查询
// @Router   /api/v1/dict/language/{id} [get]
func (*DictApi) HandleDictLanguage(c *gin.Context) {
	if !dictionaryLegacyAvailable(c) {
		return
	}
	id := c.Param("id")
	data, err := service.GroupApp.Dict.GetDictLanguageListById(id)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// GetDictLisyByPage 字典列表分页查询
// @Router   /api/v1/dict [get]
func (*DictApi) HandleDictLisyByPage(c *gin.Context) {
	if !dictionaryLegacyAvailable(c) {
		return
	}
	var byList model.GetDictLisyByPageReq
	if !BindAndValidate(c, &byList) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	logrus.Info("byList", byList)
	list, err := service.GroupApp.Dict.GetDictListByPage(&byList, userClaims)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", list)
}
