package api

import (
	"net/http"

	model "project/internal/model"
	service "project/service"
	"project/utils"

	"github.com/gin-gonic/gin"
)

type OperationLogsApi struct{}

// GetListByPage 操作日志分页查询
// @Tags     操作日志
// @Summary  操作日志分页查询
// @Description 操作日志分页查询
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetOperationLogListByPageReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/operation_logs [get]
func (api *OperationLogsApi) GetListByPage(c *gin.Context) {
	var req model.GetOperationLogListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID
	list, err := service.GroupApp.OperationLogs.GetListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get list successfully", list)
}
