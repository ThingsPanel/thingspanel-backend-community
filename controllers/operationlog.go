package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type OperationlogController struct {
	beego.Controller
}

type PaginateOperationlog struct {
	CurrentPage int                   `json:"current_page"`
	Data        []models.OperationLog `json:"data"`
	Total       int64                 `json:"total"`
	PerPage     int                   `json:"per_page"`
}

// 获取操作日志
func (c *OperationlogController) Index() {
	var OperationLogService services.OperationLogService
	w, _ := OperationLogService.List(0, 100)
	response.SuccessWithDetailed(200, "success", w, map[string]string{}, (*context2.Context)(c.Ctx))
	return
}

// 分页获取告警日志
func (c *OperationlogController) List() {
	reqData := valid.OperationLogListValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var OperationLogService services.OperationLogService
	o, count := OperationLogService.Paginate(reqData.Page, reqData.Limit, reqData.Ip, reqData.Path, tenantId)
	d := PaginateOperationlog{
		CurrentPage: reqData.Page,
		Data:        o,
		Total:       count,
		PerPage:     reqData.Limit,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	return
}
