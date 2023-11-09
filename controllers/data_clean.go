package controllers

import (
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	"encoding/json"

	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type DataCleanupController struct {
	beego.Controller
}

func (c *DataCleanupController) List() {
	var s services.TpDataCleanupService
	data, err := s.GetTpDataCleanupDetail()
	if err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	utils.SuccessWithDetailed(200, "success", data, map[string]string{}, (*context2.Context)(c.Ctx))
}

func (c *DataCleanupController) Edit() {
	var input struct {
		Id            string `json:"id" valid:"Required"`
		RetentionDays int    `json:"retention_days" valid:"Required"`
		Remark        string `json:"remark"`
	}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)
	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}
	var s services.TpDataCleanupService
	err = s.EditTpDataCleanup(input.Id, input.RetentionDays, input.Remark)
	if err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	response.Success(200, (*context2.Context)(c.Ctx))
}
