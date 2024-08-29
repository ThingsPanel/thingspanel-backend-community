package api

import (
	"net/http"

	model "project/internal/model"
	service "project/service"

	"github.com/gin-gonic/gin"
)

type DataScriptApi struct{}

// CreateDataScript 创建数据处理脚本
// @Tags     数据处理脚本
// @Summary  创建数据处理脚本
// @Description 创建数据处理脚本
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.CreateDataScriptReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "创建数据处理脚本成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/data_script [post]
func (api *DataScriptApi) CreateDataScript(c *gin.Context) {
	var req model.CreateDataScriptReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.DataScript.CreateDataScript(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Create data_script successfully", data)
}

// UpdateDataScript 更新数据处理脚本
// @Tags     数据处理脚本
// @Summary  更新数据处理脚本
// @Description 更新数据处理脚本
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateDataScriptReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "更新数据处理脚本成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/data_script [put]
func (api *DataScriptApi) UpdateDataScript(c *gin.Context) {
	var req model.UpdateDataScriptReq
	if !BindAndValidate(c, &req) {
		return
	}

	if req.Description == nil && req.Name == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "message": "修改内容不能为空"})
		return
	}

	err := service.GroupApp.DataScript.UpdateDataScript(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update data_script successfully", nil)
}

// DeleteDataScript 删除数据处理脚本
// @Tags     数据处理脚本
// @Summary  删除数据处理脚本
// @Description 删除数据处理脚本
// @accept    application/json
// @Produce   application/json
// @Param    id  path      string     true  "ID"
// @Success  200  {object}  ApiResponse  "更新数据处理脚本成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/data_script/{id} [delete]
func (api *DataScriptApi) DeleteDataScript(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.DataScript.DeleteDataScript(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete data_script successfully", nil)
}

// GetDataScriptListByPage 数据处理脚本分页查询
// @Tags     数据处理脚本
// @Summary  数据处理脚本分页查询
// @Description 数据处理脚本分页查询
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetDataScriptListByPageReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/data_script [get]
func (api *DataScriptApi) GetDataScriptListByPage(c *gin.Context) {
	var req model.GetDataScriptListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	data_scriptList, err := service.GroupApp.DataScript.GetDataScriptListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get data_script list successfully", data_scriptList)
}

// api/v1/data_script/quiz
func (api *DataScriptApi) QuizDataScript(c *gin.Context) {
	var req model.QuizDataScriptReq
	if !BindAndValidate(c, &req) {
		return
	}

	data, err := service.GroupApp.DataScript.QuizDataScript(&req)
	if err != nil {
		SuccessHandler(c, err.Error(), nil)
		return
	}
	SuccessHandler(c, data, nil)
}

// api/v1/data_script/enable [put]
func (api *DataScriptApi) EnableDataScript(c *gin.Context) {
	var req model.EnableDataScriptReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.DataScript.EnableDataScript(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Update data_script successfully", nil)
}
