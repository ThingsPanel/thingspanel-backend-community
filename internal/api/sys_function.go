package api

import (
	"fmt"
	"net/http"
	dal "project/dal"
	"project/service"
	"project/utils"

	"github.com/gin-gonic/gin"
)

type SysFunctionApi struct{}

// /api/v1/sys_function GET
func (s *SysFunctionApi) GetSysFcuntion(c *gin.Context) {
	// var userClaims = c.MustGet("claims").(*utils.UserClaims)
	// if userClaims.Authority != dal.SYS_ADMIN {
	// 	ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("权限不足,无法获取"))
	// 	return
	// }
	date, err := service.GroupApp.SysFunction.GetSysFuncion()
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get sys function successfully", date)
}

func (s *SysFunctionApi) UpdateSysFcuntion(c *gin.Context) {
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	if userClaims.Authority != dal.SYS_ADMIN {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("权限不足,无法获取"))
		return
	}
	id := c.Param("id")
	err := service.GroupApp.SysFunction.UpdateSysFuncion(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Update sys function successfully", nil)
}
