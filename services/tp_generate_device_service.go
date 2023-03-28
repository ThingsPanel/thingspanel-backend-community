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

type TpGenerateDeviceService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpGenerateDeviceService) GetTpGenerateDeviceDetail(tp_generate_device_id string) models.TpGenerateDevice {
	var tp_generate_device models.TpGenerateDevice
	psql.Mydb.First(&tp_generate_device, "id = ?", tp_generate_device_id)
	return tp_generate_device
}

// 获取列表
func (*TpGenerateDeviceService) GetTpGenerateDeviceList(PaginationValidate valid.TpGenerateDevicePaginationValidate) (bool, []valid.TpGenerateDeviceListRsp, int64) {
	var TpGenerateDevices []valid.TpGenerateDeviceListRsp
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpGenerateDevice{})
	if PaginationValidate.AddFlag != "" {
		db.Where("tp_generate_device.add_flag=?", PaginationValidate.AddFlag)
	}
	if PaginationValidate.DeviceCode != "" {
		db.Where("tp_generate_device.device_code like ?", "%"+PaginationValidate.DeviceCode+"%")
	}
	var count int64
	db.Where("batch_id=?", PaginationValidate.BatchId).Count(&count)
	result := db.Model(&models.TpGenerateDevice{}).
		Select("tp_generate_device.id,tp_generate_device.add_flag,tp_generate_device.add_date,tp_generate_device.device_code,CASE WHEN ts_kv_latest.key='SYS_ONLINE' THEN '1' ELSE '0' END AS activate_flag").
		Joins("left join ts_kv_latest on ts_kv_latest.entity_id = tp_generate_device.device_id  and ts_kv_latest.key='SYS_ONLINE' ").
		Where("tp_generate_device.batch_id=?", PaginationValidate.BatchId).Limit(PaginationValidate.PerPage).Offset(offset).Find(&TpGenerateDevices)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpGenerateDevices, 0
	}
	return true, TpGenerateDevices, count
}

// 新增数据
func (*TpGenerateDeviceService) AddTpGenerateDevice(tp_generate_device models.TpGenerateDevice) (models.TpGenerateDevice, error) {
	var uuid = uuid.GetUuid()
	tp_generate_device.Id = uuid
	result := psql.Mydb.Create(&tp_generate_device)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_generate_device, result.Error
	}
	return tp_generate_device, nil
}

// 修改数据
func (*TpGenerateDeviceService) EditTpGenerateDevice(tp_generate_device valid.TpGenerateDeviceValidate) bool {
	result := psql.Mydb.Model(&models.TpGenerateDevice{}).Where("id = ?", tp_generate_device.Id).Updates(&tp_generate_device)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpGenerateDeviceService) DeleteTpGenerateDevice(tp_generate_device models.TpGenerateDevice) error {
	var activatflag string
	psql.Mydb.Select("add_flag").Where("id=?", tp_generate_device.Id).Find(&activatflag)
	if activatflag == "1" {
		return errors.New("已激活不能删除")
	}
	result := psql.Mydb.Delete(&tp_generate_device)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}
	return nil
}

//生成设备表-批次表-产品表关联查询
func (*TpGenerateDeviceService) generateDeviceProductBatch(id string) (map[string]interface{}, error) {
	var gpb map[string]interface{}
	sql := `select tgd.device_id as device_id,tgd.add_flag as add_flag ,tgd.token as token ,tgd.password as password ,tp.protocol_type as protocol_type ,tp.plugin as plugin, 
	tb.access_address as access_address ,tp.serial_number as serial_number,tb.batch_number as serial_number,tp.device_model_id as device_model_id
	from tp_generate_device tgd left join tp_batch tb on tgd.batch_id = tb.id left join tp_product tp on  tb.product_id = tp.id where tgd.id = ?`
	result := psql.Mydb.Raw(sql, id).Scan(&gpb)
	if result.Error == nil {
		if result.RowsAffected == int64(0) {
			return gpb, errors.New("激活码错误！")
		}
	}
	return gpb, result.Error
}

// 设备激活
func (*TpGenerateDeviceService) ActivateDevice(generate_device_id string, asset_id string, device_name string) error {
	var TpGenerateDeviceService TpGenerateDeviceService
	gpb, err := TpGenerateDeviceService.generateDeviceProductBatch(generate_device_id)
	if err != nil {
		logs.Error(err.Error())
		return err
	}
	if gpb["add_flag"] == "1" {
		return errors.New("设备已添加，不能再次添加！")
	}
	var password = ""
	if gpb["password"] != nil {
		password = gpb["password"].(string)
	}
	if gpb["device_model_id"] == nil {
		gpb["device_model_id"] = ""
	}
	device := models.Device{
		ID:             gpb["device_id"].(string),
		AssetID:        asset_id,
		Token:          gpb["token"].(string),
		Password:       password,
		Name:           device_name,
		Protocol:       gpb["protocol_type"].(string),
		Type:           gpb["device_model_id"].(string),
		DeviceType:     "1",
		ProtocolConfig: "{}",
		ChartOption:    "{}",
		CreatedAt:      time.Now().Unix(),
	}
	var DeviceService DeviceService
	_, rsp_err := DeviceService.Add1(device)
	if rsp_err != nil {
		return rsp_err
	}
	//更新激活
	var tp_generate_device = valid.TpGenerateDeviceValidate{
		Id:      generate_device_id,
		AddFlag: "1",
	}
	TpGenerateDeviceService.EditTpGenerateDevice(tp_generate_device)
	return nil
}
