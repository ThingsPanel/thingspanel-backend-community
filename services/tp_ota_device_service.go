package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpOtaDeviceService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

//设备设计状态统计信息
type DeviceStatusCount struct {
	UpgradeStatus string `json:"upgrade_status,omitempty" alias:"状态"`
	Count         int    `json:"count" alias:"数量"`
}

//升级进度信息
type DeviceProgressMsg struct {
	UpgradeProgress  string `json:"step,omitempty" alias:"进度"`
	StatusDetail     string `json:"desc" alias:"描述"`
	Module           string `json:"module,omitempty" alias:"模块"`
	UpgradeStatus    string `json:"upgrade_status,omitempty"`
	StatusUpdateTime string `json:"status_update_time" alias:"升级更新时间"`
}

//升级失败详情
var upgreadFailure []string = []string{"-1", "-2", "-3", "-4"}

type OtaMsg struct {
	Id string `json:"id,omitempty" alias:"序号"`
	OtaModel
}
type OtaModel struct {
	PackageVersion string `json:"version,omitempty" alias:"进度"`
	PackageModule  string `json:"module,omitempty" alias:"描述"`
}

func (*TpOtaDeviceService) GetTpOtaDeviceList(PaginationValidate valid.TpOtaDevicePaginationValidate) (bool, []models.TpOtaDevice, int64) {
	var TpOtaDevices []models.TpOtaDevice
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpOtaDevice{})
	if PaginationValidate.OtaTaskId != "" {
		db.Where("ota_task_id =?", PaginationValidate.OtaTaskId)
	}
	if PaginationValidate.DeviceId != "" {
		db.Where("device_id like ?", "%"+PaginationValidate.DeviceId+"%")
	}
	if PaginationValidate.UpgradeStatus != "" {
		db.Where("upgrade_status =?", PaginationValidate.UpgradeStatus)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Find(&TpOtaDevices)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpOtaDevices, 0
	}
	return true, TpOtaDevices, count
}

func (*TpOtaDeviceService) GetTpOtaDeviceStatusCount(PaginationValidate valid.TpOtaDevicePaginationValidate) (bool, []DeviceStatusCount) {
	StatusCount := make([]DeviceStatusCount, 0)
	db := psql.Mydb.Model(&models.TpOtaDevice{})
	re := db.Select("upgrade_status as upgrade_status,count(*) as count").Where("remark = ? ", "ccc").Group("upgrade_status").Scan(&StatusCount)
	if re.Error != nil {
		return false, StatusCount
	}
	return true, StatusCount

}

// 新增数据
func (*TpOtaDeviceService) AddTpOtaDevice(tp_ota_device models.TpOtaDevice) (models.TpOtaDevice, error) {
	result := psql.Mydb.Create(&tp_ota_device)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_ota_device, result.Error
	}
	return tp_ota_device, nil
}

//批量插入数据
func (*TpOtaDeviceService) AddBathTpOtaDevice(tp_ota_device []models.TpOtaDevice) ([]models.TpOtaDevice, error) {
	result := psql.Mydb.Create(&tp_ota_device)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_ota_device, result.Error
	}
	return tp_ota_device, nil
}

//修改升级状态
//0-待推送 1-已推送 2-升级中 修改为已取消
//4-升级失败 修改为待推送
//3-升级成功 5-已取消 不修改
func (*TpOtaDeviceService) ModfiyUpdateDevice(tp_ota_device models.TpOtaDevice) error {
	var devices []models.TpOtaDevice
	var result *gorm.DB
	db := psql.Mydb.Model(&models.TpOtaDevice{})
	if tp_ota_device.OtaTaskId != "" {
		result = db.Where("ota_task_id=?", tp_ota_device.OtaTaskId).Find(&devices)

	} else {
		result = db.Where("id=?", tp_ota_device.Id).Find(&devices)
	}
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	for _, device := range devices {
		if device.UpgradeStatus == "0" || device.UpgradeStatus == "1" || device.UpgradeStatus == "2" {
			psql.Mydb.Model(&device).Update("upgrade_status", "5")
		}
		if device.UpgradeStatus == "4" {
			psql.Mydb.Model(&device).Update("upgrade_status", "0")
		}
	}
	return nil
}

//接收升级进度信息
func (*TpOtaDeviceService) OtaProgressMsgProc(body []byte, topic string) bool {
	logs.Info("-------------------------------")
	logs.Info("来自直连设备/网关解析后的OTA升级消息：")
	logs.Info(utils.ReplaceUserInput(string(body)))
	logs.Info("-------------------------------")
	payload, err := verifyPayload(body)
	if err != nil {
		logs.Error(err.Error())
		return false
	}

	// 通过token获取设备信息
	var deviceid string
	result_a := psql.Mydb.Select("device_id").Where("token = ? and activate_flag = '1'", payload.Token).First(&deviceid)
	if result_a.Error != nil {
		logs.Error(result_a.Error, gorm.ErrRecordNotFound)
		return false
	} else if result_a.RowsAffected <= int64(0) {
		logs.Error("根据token没查找到设备")
		return false
	}
	//byte转map
	var progressMsg DeviceProgressMsg
	err_b := json.Unmarshal(payload.Values, &progressMsg)
	if err_b != nil {
		logs.Error(err_b.Error())
		return false
	}
	//升级进度上报失败判断
	intProgress, err := strconv.Atoi(progressMsg.UpgradeProgress)
	if err != nil || intProgress > 100 {
		logs.Error(fmt.Sprintf("设备id:%s 上报升级进度失败", deviceid))
		return false
	}
	//查询升级信息对应的设备
	progressMsg.StatusUpdateTime = fmt.Sprint(time.Now().Format("2006-01-02 15:04:05"))
	var otadevice []string
	psql.Mydb.Raw(`select d.id,d.ota_task_id from tp_ota o left join tp_ota_task t on t.ota_id=o.id left join tp_ota_device d on d.ota_task_id=t.id where o.package_module = ? and t.task_status !='2' 
	             and d.device_id=? andd.UpgradeStatus not in ('0','3','5') `, progressMsg.Module, deviceid).Scan(&otadevice)
	if otadevice[0] != "" && otadevice[1] != "" {
		//升级失败判断
		isUpgradeSuccess := utils.In(progressMsg.UpgradeProgress, upgreadFailure)
		if isUpgradeSuccess {
			progressMsg.UpgradeStatus = "4"
		}
		//升级成功判断
		if progressMsg.UpgradeProgress == "100" {
			progressMsg.UpgradeStatus = "5"
		}

		//修改升级信息
		psql.Mydb.Model(&models.TpOtaDevice{}).Where("id = ? and ota_task_id", otadevice[0], otadevice[1]).Updates(progressMsg)
		return true
	}
	return false
}

//接收固件版本信息
func (*TpOtaDeviceService) OtaToinfromMsgProcOther(body []byte, topic string) bool {
	logs.Info("-------------------------------")
	logs.Info("来自直连设备/网关解析后的子设备的消息：")
	logs.Info(utils.ReplaceUserInput(string(body)))
	logs.Info("-------------------------------")
	payload, err := verifyPayload(body)
	if err != nil {
		logs.Error(err.Error())
		return false
	}

	// 通过token获取设备信息
	var deviceid string
	result_a := psql.Mydb.Select("device_id").Where("token = ? and activate_flag = '1'", payload.Token).First(&deviceid)
	if result_a.Error != nil {
		logs.Error(result_a.Error, gorm.ErrRecordNotFound)
		return false
	} else if result_a.RowsAffected <= int64(0) {
		logs.Error("根据token没查找到设备")
		return false
	}

	//byte转map
	var otamsg OtaMsg
	err_b := json.Unmarshal(payload.Values, &otamsg)
	if err_b != nil {
		logs.Error(err_b.Error())
		return false
	}
	//查询升级信息对应的设备
	var otadevice []string
	psql.Mydb.Raw(`select d.id,d.ota_task_id from tp_ota o left join tp_ota_task t on t.ota_id=o.id left join tp_ota_device d on d.ota_task_id=t.id where o.package_module = ? and t.task_status !='2' and d.device_id =?`, otamsg.OtaModel.PackageModule, deviceid).Scan(&otadevice)
	if otadevice[0] != "" && otadevice[1] != "" {
		psql.Mydb.Model(&models.TpOtaDevice{}).Where("id = ? and ota_task_id", otadevice[0], otadevice[1]).Update("current_version", otamsg.OtaModel.PackageVersion)
		return true
	}

	return false

}
