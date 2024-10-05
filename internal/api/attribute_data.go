package api

import (
	"net/http"
	"project/constant"
	"project/internal/model"
	"project/service"
	"project/utils"
	"strconv"

	"github.com/gin-gonic/gin"
)

type AttributeDataApi struct{}

// GetDataList 设备属性列表查询
// @Tags     属性数据
// @Summary  设备属性列表查询
// @Description 设备属性列表查询
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "ID"
// @Success  200  {object}  ApiResponse  "成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/attribute/datas/{id} [get]
func (a *AttributeDataApi) GetDataList(c *gin.Context) {
	id := c.Param("id")
	data, err := service.GroupApp.AttributeData.GetAttributeDataList(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get data successfully", data)
}

// 根据key查询设备属性
func (a *AttributeDataApi) GetAttributeDataByKey(c *gin.Context) {
	var req model.GetDataListByKeyReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.AttributeData.GetAttributeDataByKey(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get data successfully", data)
}

// DeleteData 删除数据
// @Tags     属性数据
// @Summary  删除数据
// @Description 删除数据
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "ID"
// @Success  200  {object}  ApiResponse  "删除成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/attribute/datas/{id} [delete]
func (a *AttributeDataApi) DeleteData(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.AttributeData.DeleteAttributeData(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete data successfully", nil)
}

// GetAttributeSetLogsDataListByPage 属性下发记录查询（分页）
// @Tags     属性数据
// @Summary  属性下发记录查询（分页）
// @Description 属性下发记录查询（分页）
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.GetAttributeSetLogsListByPageReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/attribute/datas/set/logs [get]
func (a *AttributeDataApi) GetAttributeSetLogsDataListByPage(c *gin.Context) {
	var req model.GetAttributeSetLogsListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.AttributeData.GetAttributeSetLogsDataListByPage(req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get data successfully", data)
}

// /api/v1/attribute/datas/pub
func (a *AttributeDataApi) AttributePutMessage(c *gin.Context) {
	var req model.AttributePutMessage
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.AttributeData.AttributePutMessage(c, userClaims.ID, &req, strconv.Itoa(constant.Manual))
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessOK(c)
}

// 发送获取属性请求
// /api/v1/attribute/datas/get
func (a *AttributeDataApi) AttributeGetMessage(c *gin.Context) {
	var req model.AttributeGetMessageReq
	if !BindAndValidate(c, &req) {
		return
	}
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.AttributeData.AttributeGetMessage(userClaims, &req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessOK(c)
}
