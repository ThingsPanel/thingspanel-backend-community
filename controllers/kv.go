package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

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
	CurrentPage int               `json:"current_page"`
	Data        []models.TSKVDblV `json:"data"`
	Total       int64             `json:"total"`
	PerPage     int               `json:"per_page"`
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

// 升级
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
	t, c := TSKVService.Paginate(kVIndexValidate.BusinessId, kVIndexValidate.AssetId, kVIndexValidate.Token, kVIndexValidate.Type, kVIndexValidate.StartTime, kVIndexValidate.EndTime, kVIndexValidate.Limit, (kVIndexValidate.Page-1)*kVIndexValidate.Limit, kVIndexValidate.Key)
	d := PaginateTSKV{
		CurrentPage: kVIndexValidate.Page,
		Data:        t,
		Total:       c,
		PerPage:     kVIndexValidate.Limit,
	}
	response.SuccessWithDetailed(200, "获取成功", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

//导出升级
func (this *KvController) Export() {
	KVExcelValidate := valid.KVExcelValidate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &KVExcelValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(KVExcelValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(KVExcelValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t, c := TSKVService.Paginate(KVExcelValidate.BusinessId, KVExcelValidate.AssetId, KVExcelValidate.Token, KVExcelValidate.Type, KVExcelValidate.StartTime, KVExcelValidate.EndTime, KVExcelValidate.Limit, 0, KVExcelValidate.Key)
	excel_file := excelize.NewFile()
	index := excel_file.NewSheet("Sheet1")
	excel_file.SetActiveSheet(index)
	excel_file.SetCellValue("Sheet1", "A1", "业务名称")
	excel_file.SetCellValue("Sheet1", "B1", "资产名称")
	excel_file.SetCellValue("Sheet1", "C1", "token")
	excel_file.SetCellValue("Sheet1", "D1", "时间")
	excel_file.SetCellValue("Sheet1", "E1", "数据标签")
	excel_file.SetCellValue("Sheet1", "F1", "值")
	excel_file.SetCellValue("Sheet1", "G1", "设备插件")
	var i int
	if c > 0 {
		i = 1
		for _, tv := range t {
			i++
			is := strconv.Itoa(i)
			excel_file.SetCellValue("Sheet1", "A"+is, tv.Bname)
			excel_file.SetCellValue("Sheet1", "B"+is, tv.Name)
			excel_file.SetCellValue("Sheet1", "C"+is, tv.Token)
			tm := time.Unix(tv.TS/1000000, 0)
			excel_file.SetCellValue("Sheet1", "D"+is, tm.Format("2006/01/02 03:04:05"))
			excel_file.SetCellValue("Sheet1", "E"+is, tv.Key)
			excel_file.SetCellValue("Sheet1", "F"+is, tv.DblV)
			excel_file.SetCellValue("Sheet1", "G"+is, tv.EntityType)
		}
	}
	uploadDir := "./files/excel/"
	errs := os.MkdirAll(uploadDir, os.ModePerm)
	if errs != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(this.Ctx))
	}
	// 根据指定路径保存文件
	uniqid_str := uniqid.New(uniqid.Params{Prefix: "excel", MoreEntropy: true})
	excelName := "files/excel/数据列表" + uniqid_str + ".xlsx"
	if err := excel_file.SaveAs(excelName); err != nil {
		fmt.Println(err)
	}
	response.SuccessWithDetailed(200, "获取成功", excelName, map[string]string{}, (*context2.Context)(this.Ctx))
}

func (this *KvController) ExportOld() {
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
	excelName := "files/excel/数据列表" + uniqid_str + ".xlsx"
	if err := excel_file.SaveAs(excelName); err != nil {
		fmt.Println(err)
	}
	response.SuccessWithDetailed(200, "获取成功", "", map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 获取当前KV
func (this *KvController) CurrentData() {
	CurrentKV := valid.CurrentKV{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &CurrentKV)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(CurrentKV)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(CurrentKV, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t := TSKVService.GetCurrentData(CurrentKV.EntityID)
	log.Println(t)
	response.SuccessWithDetailed(200, "获取成功", t, map[string]string{}, (*context2.Context)(this.Ctx))
}

// 根据业务获取所有设备和设备当前KV
func (this *KvController) CurrentDataByBusiness() {
	CurrentKVByBusiness := valid.CurrentKVByBusiness{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &CurrentKVByBusiness)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(CurrentKVByBusiness)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(CurrentKVByBusiness, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t := TSKVService.GetCurrentDataByBusiness(CurrentKVByBusiness.BusinessiD)
	log.Println(t)
	response.SuccessWithDetailed(200, "获取成功", t, map[string]string{}, (*context2.Context)(this.Ctx))
}

// 根据设备分组获取所有设备和设备当前KV
func (this *KvController) CurrentDataByAsset() {
	CurrentKVByAsset := valid.CurrentKVByAsset{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &CurrentKVByAsset)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(CurrentKVByAsset)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(CurrentKVByAsset, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t := TSKVService.GetCurrentDataByAsset(CurrentKVByAsset.AssetId)
	log.Println(t)
	response.SuccessWithDetailed(200, "获取成功", t, map[string]string{}, (*context2.Context)(this.Ctx))
}

// 根据设备分组获取所有设备和设备当前KV app
func (this *KvController) CurrentDataByAssetA() {
	CurrentKVByAsset := valid.CurrentKVByAsset{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &CurrentKVByAsset)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(CurrentKVByAsset)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(CurrentKVByAsset, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t := TSKVService.GetCurrentDataByAssetA(CurrentKVByAsset.AssetId)
	log.Println(t)
	response.SuccessWithDetailed(200, "获取成功", t, map[string]string{}, (*context2.Context)(this.Ctx))
}

// 根据设备id分页查询当前kv
func (KvController *KvController) DeviceHistoryData() {
	DeviceHistoryDataValidate := valid.DeviceHistoryDataValidate{}
	err := json.Unmarshal(KvController.Ctx.Input.RequestBody, &DeviceHistoryDataValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(DeviceHistoryDataValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DeviceHistoryDataValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(KvController.Ctx))
			break
		}
		return
	}
	var TSKVService services.TSKVService
	t, count := TSKVService.DeviceHistoryData(DeviceHistoryDataValidate.DeviceId, DeviceHistoryDataValidate.Current, DeviceHistoryDataValidate.Size)
	var data = make(map[string]interface{})
	data["data"] = t
	data["count"] = count
	response.SuccessWithDetailed(200, "获取成功", data, map[string]string{}, (*context2.Context)(KvController.Ctx))
}
