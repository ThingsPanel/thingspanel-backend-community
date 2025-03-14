package api

import (
	dal "project/internal/dal"
	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type SysFunctionApi struct{}

// /api/v1/sys_function GET
func (*SysFunctionApi) HandleSysFcuntion(c *gin.Context) {
	lang := c.GetHeader("Accept-Language")
	date, err := service.GroupApp.SysFunction.GetSysFuncion(lang)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", date)
}

// /api/v1/sys_function/{function_id} PUT
func (*SysFunctionApi) UpdateSysFcuntion(c *gin.Context) {
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	if userClaims.Authority != dal.SYS_ADMIN {
		c.Error(errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"authority": "authority is not sys admin",
		}))
		return
	}
	id := c.Param("id")
	err := service.GroupApp.SysFunction.UpdateSysFuncion(id)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}
