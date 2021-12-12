package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"github.com/mintance/go-uniqid"
	"github.com/xuri/excelize/v2"
)

type KvController struct {
	beego.Controller
}

type PaginateTSKV struct {
	CurrentPage int           `json:"current_page"`
	Data        []models.TSKV `json:"data"`
	Total       int64         `json:"total"`
	PerPage     int           `json:"per_page"`
}

// 获取KV
func (this *KvController) List() {
	var DeviceService services.DeviceService
	d, c := DeviceService.All()
	if c != 0 {
		response.SuccessWithDetailed(200, "获取成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "获取失败", (*context2.Context)(this.Ctx))
	return
}

func (this *KvController) Index() {
	kVIndexValidate := valid.KVIndexValidate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &kVIndexValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(kVIndexValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(kVIndexValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t, c := TSKVService.Paginate(kVIndexValidate.EntityID, kVIndexValidate.Type, kVIndexValidate.StartTime, kVIndexValidate.EndTime, kVIndexValidate.Limit, kVIndexValidate.Page-1)
	d := PaginateTSKV{
		CurrentPage: kVIndexValidate.Page,
		Data:        t,
		Total:       c,
		PerPage:     kVIndexValidate.Limit,
	}
	response.SuccessWithDetailed(200, "获取成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

func (this *KvController) Export() {
	kVExportValidate := valid.KVExportValidate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &kVExportValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(kVExportValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(kVExportValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t, c := TSKVService.GetAllByCondition(kVExportValidate.EntityID, kVExportValidate.Type, kVExportValidate.StartTime, kVExportValidate.EndTime)
	excel_file := excelize.NewFile()
	index := excel_file.NewSheet("Sheet1")
	excel_file.SetActiveSheet(index)
	excel_file.SetCellValue("Sheet1", "A1", "设备类型")
	excel_file.SetCellValue("Sheet1", "B1", "设备ID")
	excel_file.SetCellValue("Sheet1", "C1", "设备key")
	excel_file.SetCellValue("Sheet1", "D1", "时间")
	excel_file.SetCellValue("Sheet1", "E1", "设备值")
	var i int
	if c > 0 {
		i = 1
		for _, tv := range t {
			i++
			is := strconv.Itoa(i)
			excel_file.SetCellValue("Sheet1", "A"+is, tv.EntityType)
			excel_file.SetCellValue("Sheet1", "B"+is, tv.EntityID)
			excel_file.SetCellValue("Sheet1", "C"+is, tv.Key)
			excel_file.SetCellValue("Sheet1", "D"+is, tv.TS)
			excel_file.SetCellValue("Sheet1", "E"+is, tv.DblV)
		}
	}
	// 根据指定路径保存文件
	uniqid_str := uniqid.New(uniqid.Params{Prefix: "excel", MoreEntropy: true})
	if err := excel_file.SaveAs("excel/数据列表" + uniqid_str + ".xlsx"); err != nil {
		fmt.Println(err)
	}
	response.SuccessWithDetailed(200, "获取成功", "", map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
