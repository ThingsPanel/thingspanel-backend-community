package api

import (
	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/errcode"

	"github.com/gin-gonic/gin"
)

type DataScriptApi struct{}

// CreateDataScript 创建数据处理脚本
// @Router   /api/v1/data_script [post]
func (*DataScriptApi) CreateDataScript(c *gin.Context) {
	var req model.CreateDataScriptReq
	if !BindAndValidate(c, &req) {
		return
	}
	data, err := service.GroupApp.DataScript.CreateDataScript(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}

// UpdateDataScript 更新数据处理脚本
// @Router   /api/v1/data_script [put]
func (*DataScriptApi) UpdateDataScript(c *gin.Context) {
	var req model.UpdateDataScriptReq
	if !BindAndValidate(c, &req) {
		return
	}

	if req.Description == nil && req.Name == "" {
		c.Error(errcode.WithData(errcode.CodeParamError, "description and name can not be empty at the same time"))
		return
	}

	err := service.GroupApp.DataScript.UpdateDataScript(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// DeleteDataScript 删除数据处理脚本
// @Router   /api/v1/data_script/{id} [delete]
func (*DataScriptApi) DeleteDataScript(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.DataScript.DeleteDataScript(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// GetDataScriptListByPage 数据处理脚本分页查询
// @Router   /api/v1/data_script [get]
func (*DataScriptApi) HandleDataScriptListByPage(c *gin.Context) {
	var req model.GetDataScriptListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	data_scriptList, err := service.GroupApp.DataScript.GetDataScriptListByPage(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data_scriptList)
}

// /api/v1/data_script/quiz
func (*DataScriptApi) QuizDataScript(c *gin.Context) {
	var req model.QuizDataScriptReq
	if !BindAndValidate(c, &req) {
		return
	}

	data, err := service.GroupApp.DataScript.QuizDataScript(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// /api/v1/data_script/enable [put]
func (*DataScriptApi) EnableDataScript(c *gin.Context) {
	var req model.EnableDataScriptReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.DataScript.EnableDataScript(&req)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}
