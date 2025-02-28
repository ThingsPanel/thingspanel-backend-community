package api

import (
	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type OperationLogsApi struct{}

// GetListByPage 操作日志分页查询
// @Router   /api/v1/operation_logs [get]
func (*OperationLogsApi) HandleListByPage(c *gin.Context) {
	var req model.GetOperationLogListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	list, err := service.GroupApp.OperationLogs.GetListByPage(&req, userClaims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", list)
}
