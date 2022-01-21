package controllers

import (
	"ThingsPanel-Go/initialize/psql"
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	response "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"gorm.io/gorm"
)

type FieldmappingController struct {
	beego.Controller
}

func (reqDate *FieldmappingController) AddOnly() {
	addFieldMappingValidate := valid.FieldMapping{}
	err := json.Unmarshal(reqDate.Ctx.Input.RequestBody, &addFieldMappingValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(addFieldMappingValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(addFieldMappingValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(reqDate.Ctx))
			break
		}
		return
	}
	var fieldMappingList []models.FieldMapping
	for _, row := range addFieldMappingValidate.Data {
		if row.ID == "" {
			var uuid = uuid.GetUuid()
			fieldMapping := models.FieldMapping{
				ID:        uuid,
				DeviceID:  row.DeviceID,
				FieldFrom: row.FieldFrom,
				FieldTo:   row.FieldTo,
			}
			result := psql.Mydb.Create(&fieldMapping)
			if result.Error != nil {
				errors.Is(result.Error, gorm.ErrRecordNotFound)
				response.SuccessWithMessage(400, "添加失败", (*context2.Context)(reqDate.Ctx))
			} else {
				fieldMappingList = append(fieldMappingList, fieldMapping)
			}

		} else {
			fieldMapping := models.FieldMapping{
				ID:        row.ID,
				DeviceID:  row.DeviceID,
				FieldFrom: row.FieldFrom,
				FieldTo:   row.FieldTo,
			}
			result := psql.Mydb.Updates(&fieldMapping)
			if result.Error != nil {
				errors.Is(result.Error, gorm.ErrRecordNotFound)
				response.SuccessWithMessage(400, "修改失败", (*context2.Context)(reqDate.Ctx))
			} else {
				fieldMappingList = append(fieldMappingList, fieldMapping)
			}
		}

	}

	response.SuccessWithDetailed(200, "success", fieldMappingList, map[string]string{}, (*context2.Context)(reqDate.Ctx))

}

func (reqDate *FieldmappingController) UpdateOnly() {
	updateFieldMappingValidate := valid.UpdateFieldMapping{}
	err := json.Unmarshal(reqDate.Ctx.Input.RequestBody, &updateFieldMappingValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(updateFieldMappingValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(updateFieldMappingValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(reqDate.Ctx))
			break
		}
		return
	}
	for _, row := range updateFieldMappingValidate.Data {
		fieldMapping := models.FieldMapping{
			ID:        row.ID,
			DeviceID:  row.DeviceID,
			FieldFrom: row.FieldFrom,
			FieldTo:   row.FieldTo,
		}
		result := psql.Mydb.Updates(&fieldMapping)
		if result.Error != nil {
			errors.Is(result.Error, gorm.ErrRecordNotFound)
			response.SuccessWithMessage(400, "修改失败", (*context2.Context)(reqDate.Ctx))
		}
	}

	response.SuccessWithMessage(200, "success", (*context2.Context)(reqDate.Ctx))
}
