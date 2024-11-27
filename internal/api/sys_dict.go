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
// @Tags     字典管理
// @Summary  创建字典列
// @Description 该接口用于创建字典列，涉及到sys_dict表
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateDictReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
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
// @Tags     字典管理
// @Summary  创建字典多语言
// @Description 该接口用于创建字典多语言，涉及到sys_dict_language表
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateDictLanguageReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
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
// @Tags     字典管理
// @Summary  删除字典列，关联删除sys_dict_language
// @Description 删除字典列，关联删除sys_dict_language
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "字典ID"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
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

// DeleteDictLanguage 删除字典多语言
// @Tags     字典管理
// @Summary  删除字典多语言，删除sys_dict_language
// @Description 删除字典多语言
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "字典ID"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
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
// @Tags     字典管理
// @Summary  枚举查询接口
// @Description 枚举查询接口
// @accept    application/json
// @Produce   application/json
// @Param   data query model.DictListReq  true "1"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/dict/enum [get]
func (*DictApi) GetDict(c *gin.Context) {
	var dictEnum model.DictListReq
	if !BindAndValidate(c, &dictEnum) {
		return
	}
	list, err := service.GroupApp.Dict.GetDict(&dictEnum)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	// {
	// 	"code": 200,
	// 	"message": "Dict get successfully",
	// 	"data": [
	// 		{
	// 			"dict_value": "1",
	// 			"translation": "男"
	// 		},
	// 		{
	// 			"dict_value": "2",
	// 			"translation": "女"
	// 		}
	// 	]
	// }

	SuccessHandler(c, "Dict get successfully", list)
}

// 协议服务下拉菜单查询接口
// /api/v1/dict/protocol/service
func (*DictApi) GetProtocolAndService(c *gin.Context) {
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
// @Tags     字典管理
// @Summary  枚举查询接口
// @Description 枚举查询接口
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "字典ID"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/dict/language/{id} [get]
func (*DictApi) GetDictLanguage(c *gin.Context) {
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
// @Tags     字典管理
// @Summary  字典列表分页查询
// @Description 字典列表分页查询
// @accept    application/json
// @Produce   application/json
// @Param     data  body model.GetDictLisyByPageReq  true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "字典列创建成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/dict [get]
func (*DictApi) GetDictLisyByPage(c *gin.Context) {
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

	// {
	// 	"code": 200,
	// 	"message": "Get dict list successfully",
	// 	"data": {
	// 		"list": [
	// 			{
	// 				"id": "e69adc65-d69e-5a6f-15ef-31a22b0f247a",
	// 				"dict_code": "sec",
	// 				"dict_value": "2",
	// 				"created_at": "2024-01-11T16:34:02.124316Z",
	// 				"remark": ""
	// 			},
	// 			{
	// 				"id": "9bed09fa-b821-343d-2f41-6a91947d7132",
	// 				"dict_code": "sec",
	// 				"dict_value": "1",
	// 				"created_at": "2024-01-11T16:33:54.476397Z",
	// 				"remark": ""
	// 			}
	// 		],
	// 		"total": 2
	// 	}
	// }

	SuccessHandler(c, "Get dict list successfully", list)
}
