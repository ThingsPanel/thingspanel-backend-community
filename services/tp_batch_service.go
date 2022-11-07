package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
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
	sqlWhere := "1=1"
	if PaginationValidate.BatchNumber != "" {
		sqlWhere += " and batch_number like '" + PaginationValidate.BatchNumber + "'"
	}
	if PaginationValidate.Id != "" {
		sqlWhere += " and id = '" + PaginationValidate.Id + "'"
	}
	var count int64
	psql.Mydb.Model(&models.TpBatch{}).Where(sqlWhere).Count(&count)
	result := psql.Mydb.Model(&models.TpBatch{}).Where(sqlWhere).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_time desc").Find(&TpBatchs)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpBatchs, 0
	}
	return true, TpBatchs, count
}

// 新增数据
func (*TpBatchService) AddTpBatch(tp_batch models.TpBatch) (models.TpBatch, error) {
	var uuid = uuid.GetUuid()
	tp_batch.Id = uuid
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
	result := psql.Mydb.Raw("select * from tp_batch tb left join tp_product tp on  tb.product_id = tp.id where tb.id = ?", tp_batch_id).Scan(&pb)
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
		if tp_batch["protocol_type"] == "2" {
			uid = strings.Replace(uuid.GetUuid(), "-", "", -1)[0:9]
		}
		var TpGenerateDevice = models.TpGenerateDevice{
			BatchId:      tp_batch_id,
			Token:        uuid.GetUuid(),
			Password:     uid,
			ActivateFlag: "0",
			CreatedTime:  time.Now().Unix(),
			DeviceId:     uuid.GetUuid(),
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
