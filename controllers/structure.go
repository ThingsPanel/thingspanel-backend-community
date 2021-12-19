package controllers

import (
	"ThingsPanel-Go/initialize/psql"
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"github.com/bitly/go-simplejson"
	"gorm.io/gorm"
)

type StructureController struct {
	beego.Controller
}

type StructureW struct {
	Name  string           `json:"name"`
	Field []services.Field `json:"field"`
}

// 添加
func (this *StructureController) Add() {
	structureAddValidate := valid.StructureAdd{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &structureAddValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(structureAddValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(structureAddValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	res, err := simplejson.NewJson([]byte(structureAddValidate.Data))
	if err != nil {
		fmt.Println("解析出错", err)
	}
	rows, _ := res.Array()
	flag := psql.Mydb.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			if each_map, ok := row.(map[string]interface{}); ok {
				fm_id := uuid.GetUuid()
				fieldMapping := models.FieldMapping{
					ID:        fm_id,
					DeviceID:  fmt.Sprint(each_map["device_id"]),
					FieldFrom: fmt.Sprint(each_map["field_from"]),
					FieldTo:   fmt.Sprint(each_map["field_to"]),
				}
				if err := tx.Create(&fieldMapping).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if flag != nil {
		response.SuccessWithMessage(400, "插入失败", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(200, "插入成功", (*context2.Context)(this.Ctx))
	return
}

// 列表
func (this *StructureController) Index() {
	structureListValidate := valid.StructureList{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &structureListValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(structureListValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(structureListValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var FieldMappingService services.FieldMappingService
	fml, _ := FieldMappingService.GetByDeviceid(structureListValidate.ID)
	response.SuccessWithDetailed(200, "success", fml, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 编辑
func (this *StructureController) Edit() {
	structureUpdateValidate := valid.StructureUpdate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &structureUpdateValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(structureUpdateValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(structureUpdateValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	res, err := simplejson.NewJson([]byte(structureUpdateValidate.Data))
	if err != nil {
		fmt.Println("解析出错", err)
	}
	rows, _ := res.Array()
	flag := psql.Mydb.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			if each_map, ok := row.(map[string]interface{}); ok {
				if each_map["id"] != nil {
					result := tx.Model(&models.FieldMapping{}).Where("id = ?", fmt.Sprint(each_map["id"])).Updates(map[string]interface{}{
						"field_from": fmt.Sprint(each_map["field_from"]),
						"field_to":   fmt.Sprint(each_map["field_to"]),
					})
					if result.Error != nil {
						return err
					}
				} else {
					fm_id := uuid.GetUuid()
					fieldMapping := models.FieldMapping{
						ID:        fm_id,
						DeviceID:  fmt.Sprint(each_map["device_id"]),
						FieldFrom: fmt.Sprint(each_map["field_from"]),
						FieldTo:   fmt.Sprint(each_map["field_to"]),
					}
					if err := tx.Create(&fieldMapping).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if flag != nil {
		response.SuccessWithMessage(400, "插入失败", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(200, "修改成功", (*context2.Context)(this.Ctx))
	return
}

// 删除
func (this *StructureController) Delete() {
	structureDeleteValidate := valid.StructureDelete{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &structureDeleteValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(structureDeleteValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(structureDeleteValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var FieldMappingService services.FieldMappingService
	f := FieldMappingService.Delete(structureDeleteValidate.ID)
	if f {
		response.SuccessWithMessage(200, "删除失败", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "删除失败", (*context2.Context)(this.Ctx))
	return
}

func (this *StructureController) Field() {
	structureFieldValidate := valid.StructureField{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &structureFieldValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(structureFieldValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(structureFieldValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var AssetService services.AssetService
	el := AssetService.Extension()
	var wd []StructureW
	if len(el) > 0 {
		for _, ev := range el {
			wl := AssetService.Widget(ev.Key)
			if len(wl) > 0 {
				for _, wv := range wl {
					fl := AssetService.Field(ev.Key, wv.Key)
					if len(fl) > 0 {
						i := StructureW{
							Name:  wv.Name,
							Field: fl,
						}
						wd = append(wd, i)
					}
				}
			}
		}
	}
	if len(wd) == 0 {
		wd = []StructureW{}
	}
	response.SuccessWithDetailed(200, "success", wd, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
