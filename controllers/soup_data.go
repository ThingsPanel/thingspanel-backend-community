package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"github.com/mintance/go-uniqid"
	"github.com/xuri/excelize/v2"
	"os"
	"strconv"
	"strings"
	"time"
)

type SoupDataController struct {
	beego.Controller
}

func (soup *SoupDataController) Index() {
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

func (soup *SoupDataController) Export() {
	SoupDataExcelValidate := valid.SoupDataPaginationValidate{}
	err := json.Unmarshal(soup.Ctx.Input.RequestBody, &SoupDataExcelValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(SoupDataExcelValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(SoupDataExcelValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(soup.Ctx))
			break
		}
		return
	}
	var TSKVService services.SoupDataService
	//每次查10000条
	num := SoupDataExcelValidate.Limit / 10000
	excel_file := excelize.NewFile()
	index := excel_file.NewSheet("Sheet1")
	excel_file.SetActiveSheet(index)
	excel_file.SetCellValue("Sheet1", "A1", "店名")
	excel_file.SetCellValue("Sheet1", "B1", "订单号")
	excel_file.SetCellValue("Sheet1", "C1", "锅底名称")
	excel_file.SetCellValue("Sheet1", "D1", "桌号")
	excel_file.SetCellValue("Sheet1", "E1", "订单时间")
	excel_file.SetCellValue("Sheet1", "F1", "开始加汤时间")
	excel_file.SetCellValue("Sheet1", "G1", "加汤完毕时间")
	excel_file.SetCellValue("Sheet1", "H1", "加料完成时间")
	excel_file.SetCellValue("Sheet1", "I1", "转锅完成时间")
	for i := 0; i <= num; i++ {
		var t []models.AddSoupDataValue
		var c int64
		if (i+1)*10000 <= SoupDataExcelValidate.Limit {
			t, c = TSKVService.Paginate(SoupDataExcelValidate.ShopName, (i+1)*10000, i*10000)
		} else {
			t, c = TSKVService.Paginate(SoupDataExcelValidate.ShopName, SoupDataExcelValidate.Limit%10000, i*10000)
		}
		var i int
		if c > 0 {
			i = 1
			for _, tv := range t {
				i++
				is := strconv.Itoa(i)
				excel_file.SetCellValue("Sheet1", "A"+is, tv.ShopName)
				excel_file.SetCellValue("Sheet1", "B"+is, tv.OrderSn)
				excel_file.SetCellValue("Sheet1", "C"+is, tv.BottomPot)
				excel_file.SetCellValue("Sheet1", "D"+is, tv.TableNumber)
				excel_file.SetCellValue("Sheet1", "E"+is, time.Unix(tv.OrderTime/1000, 0).Format("2006/01/02 15:04:05"))
				excel_file.SetCellValue("Sheet1", "F"+is, time.Unix(tv.SoupStartTime/1000, 0).Format("2006/01/02 15:04:05"))
				excel_file.SetCellValue("Sheet1", "G"+is, time.Unix(tv.SoupEndTime/1000, 0).Format("2006/01/02 15:04:05"))
				excel_file.SetCellValue("Sheet1", "H"+is, time.Unix(tv.FeedingEndTime/1000, 0).Format("2006/01/02 15:04:05"))
				excel_file.SetCellValue("Sheet1", "I"+is, time.Unix(tv.TurningPotEnd/1000, 0).Format("2006/01/02 15:04:05"))
			}
		}
	}

	uploadDir := "./files/excel/"
	errs := os.MkdirAll(uploadDir, os.ModePerm)
	if errs != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(soup.Ctx))
	}
	// 根据指定路径保存文件
	uniqid_str := uniqid.New(uniqid.Params{Prefix: "excel", MoreEntropy: true})
	excelName := "files/excel/数据列表" + uniqid_str + ".xlsx"
	if err := excel_file.SaveAs(excelName); err != nil {
		fmt.Println(err)
	}
	response.SuccessWithDetailed(200, "获取成功", excelName, map[string]string{}, (*context2.Context)(soup.Ctx))
}
