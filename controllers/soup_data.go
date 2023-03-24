package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"strings"
)

type SoupDataController struct {
	beego.Controller
}

func (soup *SoupDataController) Index() {
	response.SuccessWithDetailed(200, "success", nil, map[string]string{}, (*context2.Context)(soup.Ctx))
	return
}

func (soup *SoupDataController) List() {
	PaginationValidate := valid.SoupDataPaginationValidate{}
	err := json.Unmarshal(soup.Ctx.Input.RequestBody, &PaginationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(PaginationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(PaginationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(soup.Ctx))
			break
		}
		return
	}
	var SoupDataService services.SoupDataService
	isSuccess, d, t := SoupDataService.GetList(PaginationValidate)

	if !isSuccess {
		response.SuccessWithMessage(1000, "查询失败", (*context2.Context)(soup.Ctx))
		return
	}
	dd := valid.RspSoupDataPaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(soup.Ctx))

}

//导出升级
//func (soup *SoupDataController) Export() {
//	SoupDataExcelValidate := valid.SoupDataPaginationValidate{}
//	err := json.Unmarshal(soup.Ctx.Input.RequestBody, &SoupDataExcelValidate)
//	if err != nil {
//		fmt.Println("参数解析失败", err.Error())
//	}
//	v := validation.Validation{}
//	status, _ := v.Valid(SoupDataExcelValidate)
//	if !status {
//		for _, err := range v.Errors {
//			// 获取字段别称
//			alias := gvalid.GetAlias(SoupDataExcelValidate, err.Field)
//			message := strings.Replace(err.Message, err.Field, alias, 1)
//			response.SuccessWithMessage(1000, message, (*context2.Context)(soup.Ctx))
//			break
//		}
//		return
//	}
//	var TSKVService services.TSKVService
//	//每次查10000条
//	num := SoupDataExcelValidate.Limit / 10000
//	excel_file := excelize.NewFile()
//	index := excel_file.NewSheet("Sheet1")
//	excel_file.SetActiveSheet(index)
//	excel_file.SetCellValue("Sheet1", "A1", "店名")
//	excel_file.SetCellValue("Sheet1", "B1", "订单号")
//	excel_file.SetCellValue("Sheet1", "C1", "锅底名称")
//	excel_file.SetCellValue("Sheet1", "D1", "桌号")
//	excel_file.SetCellValue("Sheet1", "E1", "数据标签")
//	excel_file.SetCellValue("Sheet1", "F1", "订单时间")
//	excel_file.SetCellValue("Sheet1", "G1", "开始加汤时间")
//	excel_file.SetCellValue("Sheet1", "H1", "加汤完毕时间")
//	excel_file.SetCellValue("Sheet1", "I1", "加料完成时间")
//	excel_file.SetCellValue("Sheet1", "J1", "转锅完成时间")
//	for i := 0; i <= num; i++ {
//		var t []models.TSKVDblV
//		var c int64
//		if (i+1)*10000 <= SoupDataExcelValidate.Limit {
//			t, c = TSKVService.Paginate(SoupDataExcelValidate.ShopName, SoupDataExcelValidate., SoupDataExcelValidate.Token, SoupDataExcelValidate.Type, SoupDataExcelValidate.StartTime, SoupDataExcelValidate.EndTime, (i+1)*10000, i*10000, SoupDataExcelValidate.Key, SoupDataExcelValidate.DeviceName)
//		} else {
//			t, c = TSKVService.Paginate(SoupDataExcelValidate.BusinessId, SoupDataExcelValidate.AssetId, SoupDataExcelValidate.Token, SoupDataExcelValidate.Type, SoupDataExcelValidate.StartTime, SoupDataExcelValidate.EndTime, SoupDataExcelValidate.Limit%10000, i*10000, SoupDataExcelValidate.Key, SoupDataExcelValidate.DeviceName)
//		}
//		var i int
//		if c > 0 {
//			i = 1
//			for _, tv := range t {
//				i++
//				is := strconv.Itoa(i)
//				excel_file.SetCellValue("Sheet1", "A"+is, tv.Bname)
//				excel_file.SetCellValue("Sheet1", "B"+is, tv.Name)
//				excel_file.SetCellValue("Sheet1", "C"+is, tv.Token)
//				tm := time.Unix(tv.TS/1000000, 0)
//				excel_file.SetCellValue("Sheet1", "D"+is, tm.Format("2006/01/02 03:04:05"))
//				excel_file.SetCellValue("Sheet1", "E"+is, tv.Key)
//				if tv.StrV == "" {
//					excel_file.SetCellValue("Sheet1", "F"+is, tv.DblV)
//				} else {
//					excel_file.SetCellValue("Sheet1", "F"+is, tv.StrV)
//				}
//				excel_file.SetCellValue("Sheet1", "G"+is, tv.EntityType)
//			}
//		}
//	}
//	//t, c := TSKVService.Paginate(KVExcelValidate.BusinessId, KVExcelValidate.AssetId, KVExcelValidate.Token, KVExcelValidate.Type, KVExcelValidate.StartTime, KVExcelValidate.EndTime, KVExcelValidate.Limit, 0, KVExcelValidate.Key, KVExcelValidate.DeviceName)
//
//	uploadDir := "./files/excel/"
//	errs := os.MkdirAll(uploadDir, os.ModePerm)
//	if errs != nil {
//		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(soup.Ctx))
//	}
//	// 根据指定路径保存文件
//	uniqid_str := uniqid.New(uniqid.Params{Prefix: "excel", MoreEntropy: true})
//	excelName := "files/excel/数据列表" + uniqid_str + ".xlsx"
//	if err := excel_file.SaveAs(excelName); err != nil {
//		fmt.Println(err)
//	}
//	response.SuccessWithDetailed(200, "获取成功", excelName, map[string]string{}, (*context2.Context)(soup.Ctx))
//}
