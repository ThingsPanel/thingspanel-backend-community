package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type ConditionsLogService struct {
}

// 新增控制日志
func (*ConditionsLogService) Insert(conditionsLog *models.ConditionsLog) (*models.ConditionsLog, error) {
	conditionsLog.ID = uuid.GetUuid()
	conditionsLog.CteateTime = time.Now().Format("2006-01-02 15:04:05")
	err := psql.Mydb.Create(conditionsLog).Error
	if err != nil {
		logs.Error(err.Error())
		return nil, err
	} else {
		return conditionsLog, err
	}
}

// 新增控制日志
// Paginate 分页获取OperationLog数据
func (*ConditionsLogService) Paginate(conditionsLogListValidate valid.ConditionsLogListValidate) ([]map[string]interface{}, int64) {
	sqlWhere := `select cl.id ,cl.device_id ,cl.operation_type ,cl.instruct ,cl.sender ,cl.send_result ,cl.respond ,
	cl.cteate_time ,cl.remark ,cl.protocol_type ,d."name" as device_name ,a.id as asset_id ,a."name" as asset_name,
	 b.id as business_id ,b."name" as business_name from conditions_log cl left join device d on cl.device_id = d.id  
	 left join asset a on a.id = d.asset_id left join business b on b.id =a.business_id where 1=1`
	sqlWhereCount := `select count(1) from conditions_log cl left join device d on cl.device_id = d.id  
	 left join asset a on a.id = d.asset_id left join business b on b.id =a.business_id where 1=1`
	where := ""
	if conditionsLogListValidate.DeviceId != "" {
		where += " and cl.device_id = '" + conditionsLogListValidate.DeviceId + "'"
	}
	if conditionsLogListValidate.OperationType != "" {
		where += " and cl.operation_type = '" + conditionsLogListValidate.OperationType + "'"
	}
	if conditionsLogListValidate.SendResult != "" {
		where += " and cl.send_result = '" + conditionsLogListValidate.SendResult + "'"
	}
	if conditionsLogListValidate.BusinessId != "" {
		where += " and b.business_id = '" + conditionsLogListValidate.BusinessId + "'"
	}
	if conditionsLogListValidate.AssetId != "" {
		where += " and a.asset_id = '" + conditionsLogListValidate.AssetId + "'"
	}
	if conditionsLogListValidate.BusinessName != "" {
		where += " and b.name like '%" + conditionsLogListValidate.BusinessName + "%'"
	}
	if conditionsLogListValidate.AssetName != "" {
		where += " and a.name like '%" + conditionsLogListValidate.AssetName + "%'"
	}
	if conditionsLogListValidate.DeviceName != "" {
		where += " and d.name like '%" + conditionsLogListValidate.DeviceName + "%'"
	}
	sqlWhere += where
	var conditionsLogs []map[string]interface{}
	var values []interface{}
	var count int64
	countResult := psql.Mydb.Raw(sqlWhereCount).Count(&count)
	if countResult.Error != nil {
		errors.Is(countResult.Error, gorm.ErrRecordNotFound)
	}
	//计算分页
	offset := conditionsLogListValidate.Size * (conditionsLogListValidate.Current - 1)
	values = append(values, offset, conditionsLogListValidate.Size)
	sqlWhere += "order by cl.cteate_time desc offset ? limit ?"
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&conditionsLogs)
	if dataResult.Error != nil {
		errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
	}
	return conditionsLogs, count
}
