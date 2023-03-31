package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/skip2/go-qrcode"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

type TpBatchService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpBatchService) GetTpBatchDetail(tp_batch_id string) models.TpBatch {
	var tp_batch models.TpBatch
	psql.Mydb.First(&tp_batch, "id = ?", tp_batch_id)
	return tp_batch
}

// 获取列表
func (*TpBatchService) GetTpBatchList(PaginationValidate valid.TpBatchPaginationValidate) (bool, []models.TpBatch, int64) {
	var TpBatchs []models.TpBatch
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpBatch{})
	if PaginationValidate.BatchNumber != "" {
		db.Where("batch_number like ?", "%"+PaginationValidate.BatchNumber+"%")
	}
	if PaginationValidate.Id != "" {
		db.Where("id = ?", PaginationValidate.Id)
	}
	if PaginationValidate.ProductId != "" {
		db.Where("product_id = ?", PaginationValidate.ProductId)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("created_time desc").Find(&TpBatchs)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpBatchs, 0
	}
	return true, TpBatchs, count
}

// 新增数据
func (*TpBatchService) AddTpBatch(tp_batch models.TpBatch) (models.TpBatch, error) {
	result := psql.Mydb.Create(&tp_batch)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_batch, result.Error
	}
	return tp_batch, nil
}

// 修改数据
func (*TpBatchService) EditTpBatch(tp_batch valid.TpBatchValidate) bool {
	result := psql.Mydb.Model(&models.TpBatch{}).Where("id = ?", tp_batch.Id).Updates(&tp_batch)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpBatchService) DeleteTpBatch(tp_batch models.TpBatch) error {
	sql := `select count(*) from tp_generate_device where add_flag = '1' and batch_id= ?`
	var count int64
	if resultcount := psql.Mydb.Raw(sql, tp_batch.Id).Scan(&count); resultcount.Error != nil {
		logs.Error(resultcount.Error)
		return resultcount.Error
	}
	if count > 0 {
		return errors.New("有设备已激活,不能删除")
	}
	result := psql.Mydb.Delete(&tp_batch)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}

//批次表-产品表关联查询
func (*TpBatchService) productBatch(tp_batch_id string) (map[string]interface{}, error) {
	var pb map[string]interface{}
	result := psql.Mydb.Raw("select tb.*,tp.serial_number from tp_batch tb left join tp_product tp on  tb.product_id = tp.id where tb.id = ?", tp_batch_id).Scan(&pb)
	if result.RowsAffected == int64(0) {
		return pb, errors.New("没有这个批次信息！")
	}
	return pb, result.Error
}

// 批次生成
func (*TpBatchService) GenerateBatch(tp_batch_id string) error {
	var TpBatchService TpBatchService
	var TpGenerateDeviceService TpGenerateDeviceService
	tp_batch, err := TpBatchService.productBatch(tp_batch_id)
	if err != nil {
		logs.Error(err.Error())
		return err
	}
	if tp_batch["generate_flag"].(string) == "1" {
		return errors.New("已生成的批次，不能再次生成")
	}
	for i := 0; i < int(tp_batch["device_number"].(int32)); i++ {
		var uid string = ""
		if tp_batch["auth_type"] == "2" {
			uid = strings.Replace(uuid.GetUuid(), "-", "", -1)[0:9]
		}
		device_code := tp_batch["serial_number"].(string) + "-" + tp_batch["batch_number"].(string) + "-" + fmt.Sprintf("%04d", i+1)
		var TpGenerateDevice = models.TpGenerateDevice{
			BatchId:     tp_batch_id,
			Token:       uuid.GetUuid(),
			Password:    uid,
			AddFlag:     "0",
			CreatedTime: time.Now().UnixMicro(),
			DeviceId:    uuid.GetUuid(),
			DeviceCode:  device_code,
		}
		// 插入数据
		TpGenerateDeviceService.AddTpGenerateDevice(TpGenerateDevice)
		var u = valid.TpBatchValidate{
			Id:           tp_batch_id,
			GenerateFlag: "1",
		}
		TpBatchService.EditTpBatch(u)
	}
	return nil
}

// 导出
func (*TpBatchService) Export(batch_id string) (string, error) {
	excel_file := excelize.NewFile()
	index := excel_file.NewSheet("Sheet1")
	excel_file.SetActiveSheet(index)
	excel_file.SetCellValue("Sheet1", "A1", "产品编号")
	excel_file.SetCellValue("Sheet1", "B1", "批号")
	excel_file.SetCellValue("Sheet1", "C1", "协议类型")
	excel_file.SetCellValue("Sheet1", "D1", "接入地址")
	excel_file.SetCellValue("Sheet1", "E1", "用户名")
	excel_file.SetCellValue("Sheet1", "F1", "密码")
	excel_file.SetCellValue("Sheet1", "G1", "二维码bash64")
	var gpb []map[string]interface{}
	sql := `select tgd.device_id as device_id,tgd.add_flag as add_flag ,tgd.token as token ,tgd.password as password ,tp.protocol_type as protocol_type ,tp.plugin as plugin, 
	tb.access_address as access_address ,tp.serial_number as serial_number,tb.batch_number as batch_number,tgd.id as generate_device_id
	from tp_generate_device tgd left join tp_batch tb on tgd.batch_id = tb.id left join tp_product tp on  tb.product_id = tp.id where tb.id = ?`
	result := psql.Mydb.Raw(sql, batch_id).Scan(&gpb)
	if result.Error == nil {
		if result.RowsAffected == int64(0) {
			return "", errors.New("查询不到批次下已生成的设备")
		}
	}
	uploadDir := "./files/QR/"
	errs := os.MkdirAll(uploadDir, os.ModePerm)
	if errs != nil {
		return "", errs
	}
	var i int = 1
	for _, tv := range gpb {
		i++
		is := strconv.Itoa(i)
		if tv["access_address"] == nil {
			tv["access_address"] = ""
		}
		if tv["password"] == nil {
			tv["password"] = ""
		}
		filepath := "./files/QR/" + tv["generate_device_id"].(string) + ".png"
		qrcode.WriteFile(tv["generate_device_id"].(string), qrcode.Medium, 256, filepath)
		srcByte, err := ioutil.ReadFile(filepath)
		if err != nil {
			logs.Error(err.Error())
		}
		res := base64.StdEncoding.EncodeToString(srcByte)
		excel_file.SetCellValue("Sheet1", "A"+is, tv["serial_number"].(string))
		excel_file.SetCellValue("Sheet1", "B"+is, tv["batch_number"].(string))
		excel_file.SetCellValue("Sheet1", "C"+is, tv["protocol_type"].(string))
		excel_file.SetCellValue("Sheet1", "D"+is, tv["access_address"].(string))
		excel_file.SetCellValue("Sheet1", "E"+is, tv["token"].(string))
		excel_file.SetCellValue("Sheet1", "F"+is, tv["password"].(string))
		excel_file.SetCellValue("Sheet1", "G"+is, res)
	}
	uploadDir1 := "./files/excel/"
	errs1 := os.MkdirAll(uploadDir1, os.ModePerm)
	if errs1 != nil {
		return "", errs1
	}
	// 根据指定路径保存文件
	excelName := "files/excel/产品数据" + time.Now().Format("20060102150405") + ".xlsx"
	if err := excel_file.SaveAs(excelName); err != nil {
		logs.Error(err.Error())
	}
	return excelName, nil
}

//导入
func (*TpBatchService) Import(bath_id, batch_number, product_id, file string) ([]models.TpGenerateDevice, error) {
	var authtype string
	if err := psql.Mydb.Model(&models.TpProduct{}).Select("auth_type").Where("id=?", product_id).Find(&authtype).Error; err != nil {
		return nil, errors.New("产品查询失败")
	}
	var product_serial_number string
	if err := psql.Mydb.Model(&models.TpProduct{}).Select("serial_number").Where("id=?", product_id).Find(&product_serial_number).Error; err != nil {
		return nil, errors.New("产品编号查询失败")
	}
	f, err := excelize.OpenFile(file)
	if err != nil {
		logs.Error("打开文件失败")
		return nil, errors.New("打开文件失败")
	}
	fl := f.GetSheetList()
	if len(fl) < 0 {
		return nil, errors.New("无sheet处理")
	}
	rows, err1 := f.GetRows(fl[0])
	if err1 != nil {
		return nil, errors.New("获取行失败")
	}
	devicenumber := len(rows)
	if len(rows) <= 1 {
		return nil, errors.New("无设备数据不能创建批次")
	}
	if len(rows) > 10000 {
		return nil, errors.New("设备数量超上限")
	}
	var generatedevices []models.TpGenerateDevice
	passwd := ""
	for i := 1; i < devicenumber; i++ {
		if authtype == "2" && len(rows[i]) <= 2 {
			return nil, errors.New("MQTTBasic认证方式必须有密码")
		}
		if rows[i][1] == "" {
			return nil, errors.New("必须有token")
		}
		if len(rows[i]) == 3 {
			passwd = rows[i][2]
		}
		generatedevices = append(generatedevices, models.TpGenerateDevice{
			Id:          utils.GetUuid(),
			BatchId:     bath_id,
			Token:       rows[i][1],
			Password:    passwd,
			AddFlag:     "0",
			CreatedTime: time.Now().UnixMicro(),
			DeviceCode:  product_serial_number + "-" + batch_number + "-" + fmt.Sprintf("%04d", i),
		})
	}
	return generatedevices, nil

}
