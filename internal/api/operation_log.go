package api

import (
	"net/http"

	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type OperationLogsApi struct{}

// GetListByPage 操作日志分页查询
// @Router   /api/v1/operation_logs [get]
func (api *OperationLogsApi) GetListByPage(c *gin.Context) {
	var req model.GetOperationLogListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	req.TenantID = userClaims.TenantID
	list, err := service.GroupApp.OperationLogs.GetListByPage(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get list successfully", list)
}
