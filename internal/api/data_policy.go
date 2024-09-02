package api

import (
	"net/http"

	model "project/internal/model"
	service "project/service"

	"github.com/gin-gonic/gin"
)

type DataPolicyApi struct{}

// UpdateDataPolicy 更新数据清理
// @Tags     数据清理
// @Summary  更新数据清理
// @Description 更新数据清理
// @accept    application/json
// @Produce   application/json
// @Param     data  body      model.UpdateDataPolicyReq   true  "见下方JSON"
// @Success  200  {object}  ApiResponse  "更新数据清理成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/datapolicy [put]
func (api *DataPolicyApi) UpdateDataPolicy(c *gin.Context) {
	var req model.UpdateDataPolicyReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.DataPolicy.UpdateDataPolicy(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Update datapolicy successfully", nil)
}

// GetDataPolicyListByPage 数据清理分页查询
// @Tags     数据清理
// @Summary  数据清理分页查询
// @Description 数据清理分页查询
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetDataPolicyListByPageReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/datapolicy [get]
func (api *DataPolicyApi) GetDataPolicyListByPage(c *gin.Context) {
	var req model.GetDataPolicyListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	datapolicyList, err := service.GroupApp.DataPolicy.GetDataPolicyListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get datapolicy list successfully", datapolicyList)
}
