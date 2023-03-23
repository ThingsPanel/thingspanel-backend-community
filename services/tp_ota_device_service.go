package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/modules/dataService/mqtt"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"errors"
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

func (*TpOtaDeviceService) GetTpOtaDeviceList(PaginationValidate valid.TpOtaDevicePaginationValidate) (bool, []map[string]interface{}, int64) {
	sqlWhere := `select od.*,d.name from tp_ota_device od left join device d on od.device_id=d.id where 1=1`
	sqlWhereCount := `select count(1) from tp_ota_device od left join device d on od.device_id=d.id where 1=1`
	var values []interface{}
	var where = ""
	if PaginationValidate.Name != "" {
		values = append(values, "%"+PaginationValidate.Name+"%")
		where += " and d.name like ?"
	}
	if PaginationValidate.DeviceId != "" {
		values = append(values, PaginationValidate.DeviceId)
		where += " and od.device_id = ?"
	}
	if PaginationValidate.OtaTaskId != "" {
		values = append(values, PaginationValidate.OtaTaskId)
		where += " and od.ota_task_id = ?"
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
	var deviceList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&deviceList)
	if dataResult.Error != nil {
		errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
	}
	return true, deviceList, count
}

func (*TpOtaDeviceService) GetTpOtaDeviceStatusCount(PaginationValidate valid.TpOtaDevicePaginationValidate) (bool, []DeviceStatusCount) {
	StatusCount := make([]DeviceStatusCount, 0)
	db := psql.Mydb.Model(&models.TpOtaDevice{})
	re := db.Select("upgrade_status as upgrade_status,count(*) as count").Where("ota_task_id = ? ", PaginationValidate.OtaTaskId).Group("upgrade_status").Scan(&StatusCount)
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
	if tp_ota_device.OtaTaskId != "" && tp_ota_device.Id != "" {
		//单个设备
		result = db.Where("ota_task_id=? and id=? ", tp_ota_device.OtaTaskId, tp_ota_device.Id).Find(&devices)

	} else {
		//任务下的所有设备
		result = db.Where("ota_task_id=?", tp_ota_device.OtaTaskId).Find(&devices)
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
	logs.Info("来自直连设备解析后的OTA升级消息：")
	logs.Info(utils.ReplaceUserInput(string(body)))
	logs.Info("-------------------------------")
	payload, err := verifyPayload(body)
	if err != nil {
		logs.Error(err.Error())
		return false
	}

	// 通过token获取设备信息
	var deviceid string
	result_a := psql.Mydb.Model(models.Device{}).Select("device_id").Where("token = ?", payload.Token).First(&deviceid)
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
	             and d.device_id=? and d.upgrade_status not in ('0','3','5') `, progressMsg.Module, deviceid).Scan(&otadevice)
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
	logs.Info("来自直连设备解析后的ota消息：")
	logs.Info(utils.ReplaceUserInput(string(body)))
	logs.Info("-------------------------------")
	payload, err := verifyPayload(body)
	if err != nil {
		logs.Error(err.Error())
		return false
	}

	// 通过token获取设备信息
	var deviceid string
	result_a := psql.Mydb.Model(models.Device{}).Select("device_id").Where("token = ?", payload.Token).First(&deviceid)
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
		psql.Mydb.Model(&models.Device{}).Where("id = ?", deviceid).Update("current_version", otamsg.OtaModel.PackageVersion)
		psql.Mydb.Model(&models.TpOtaDevice{}).Where("id = ? and ota_task_id", otadevice[0], otadevice[1]).Update("current_version", otamsg.OtaModel.PackageVersion)
		return true
	}

	return false

}

//推送升级包到设备
func (*TpOtaDeviceService) OtaToUpgradeMsg(devices []models.Device, otaid string) error {
	var ota models.TpOta
	if err := psql.Mydb.Where("id=?", otaid).Find(&ota).Error; err != nil {
		logs.Error("不存在该ota固件")
		return errors.New("无对应固件")
	}
	for _, device := range devices {
		var otamsg = make(map[string]interface{})
		otamsg["id"] = "123"
		otamsg["code"] = "200"
		var otamsgparams = make(map[string]interface{})
		otamsgparams["version"] = ota.PackageVersion
		otamsgparams["url"] = "http://dev.thingspanel.cn:8080" + ota.PackageUrl
		otamsgparams["signMethod"] = ota.SignatureAlgorithm
		otamsgparams["sign"] = ota.Sign
		otamsgparams["module"] = ota.PackageModule
		otamsgparams["extData"] = ota.AdditionalInfo
		otamsg["params"] = otamsgparams
		msgdata, json_err := json.Marshal(otamsg)
		if json_err != nil {
			logs.Error(json_err.Error())
		} else {
			go mqtt.SendOtaAdress(msgdata, device.Token)
		}
	}
	return nil
}
