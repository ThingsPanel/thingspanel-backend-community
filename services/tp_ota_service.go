package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"
	"errors"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpOtaService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

//获取列表
func (*TpOtaService) GetTpOtaList(PaginationValidate valid.TpOtaPaginationValidate) (bool, []map[string]interface{}, int64) {
	sqlWhere := `select o.*,p.name as product_name from tp_ota o left join tp_product p on o.product_id=p.id where 1=1 `
	sqlWhereCount := `select count(1) from tp_ota o left join tp_product p on o.product_id=p.id where 1=1`
	var values []interface{}
	var where = ""
	if PaginationValidate.PackageName != "" {
		values = append(values, "%"+PaginationValidate.PackageName+"%")
		where += " and o.package_name like ?"
	}
	if PaginationValidate.Id != "" {
		values = append(values, PaginationValidate.Id)
		where += " and o.id = ?"
	}
	if PaginationValidate.ProductId != "" {
		values = append(values, PaginationValidate.ProductId)
		where += " and o.product_id = ?"
	}
	sqlWhere += where
	sqlWhereCount += where
	var count int64
	result := psql.Mydb.Raw(sqlWhereCount, values...).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	var offset int = (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	var limit int = PaginationValidate.PerPage
	sqlWhere += " offset ? limit ?"
	values = append(values, offset, limit)
	var otaList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&otaList)
	// 判断升级状态
	for i := 0; i < len(otaList); i++ {
		// 获取当前时间并格式化为2006-01-02 15:04:05
		nowTime := time.Now().Format("2006-01-02 15:04:05")
		// 如果升级时间类型是1-定时升级
		if otaList[i]["upgrade_time_type"] == "1" {
			// 如果开始时间大于当前时间，说明还未开始升级
			if otaList[i]["start_time"].(time.Time).Format("2006-01-02 15:04:05") > nowTime {
				otaList[i]["task_status"] = "0"
				continue
			} else if otaList[i]["end_time"].(time.Time).Format("2006-01-02 15:04:05") < nowTime {
				otaList[i]["task_status"] = "2"
				continue
			}
		}
		// 查询升级任务下的设备不是升级成功或者失败或已取消状态的数量
		var count int64
		if err := psql.Mydb.Model(&models.TpOtaDevice{}).Where("ota_task_id = ? and upgrade_status not in ('3','4','5')", otaList[i]["id"]).Count(&count).Error; err != nil {
			logs.Error(err)
			// 设置升级状态为空
			otaList[i]["task_status"] = ""
			continue
		} else {
			// 如果count大于0，说明还有设备没有升级完成
			if count == 0 {
				otaList[i]["task_status"] = "2"
			} else {
				otaList[i]["task_status"] = "1"
			}
		}

	}
	if dataResult.Error != nil {
		errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
	}
	return true, otaList, count
}

//根据id获取升级包信息
func (*TpOtaService) GetTpOtaVersionById(otaid string) (bool, models.TpOta) {
	var TpOtas models.TpOta
	result := psql.Mydb.Model(&models.TpOta{}).Where("id=?", otaid).Find(&TpOtas)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpOtas
	}
	return true, TpOtas
}

// 新增数据
func (*TpOtaService) AddTpOta(tp_ota models.TpOta) (map[string]interface{}, error) {
	var data map[string]interface{}
	result := psql.Mydb.Create(&tp_ota)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return data, result.Error
	}
	if err := psql.Mydb.Raw(`select o.*,p.name as product_name from tp_ota o left join tp_product p on o.product_id=p.id where o.id = ?`, tp_ota.Id).Scan(&data).Error; err != nil {
		return data, err
	}
	return data, nil
}
func (*TpOtaService) DeleteTpOta(tp_ota models.TpOta) error {
	var count int64
	if err := psql.Mydb.Model(&models.TpOtaTask{}).Where("ota_id = ?", tp_ota.Id).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return errors.New("存在升级任务不能删除固件")
	}
	result := psql.Mydb.Delete(&tp_ota)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}
