package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	sendmqtt "ThingsPanel-Go/modules/dataService/mqtt/sendMqtt"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
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

// 设备设计状态统计信息
type DeviceStatusCount struct {
	UpgradeStatus string `json:"upgrade_status,omitempty" alias:"状态"`
	Count         int    `json:"count" alias:"数量"`
}

// 升级进度信息
type DeviceProgressMsg struct {
	UpgradeProgress  interface{} `json:"step,omitempty" alias:"进度"`
	StatusDetail     string      `json:"desc" alias:"描述"`
	Module           string      `json:"module,omitempty" alias:"模块"`
	UpgradeStatus    string      `json:"upgrade_status,omitempty"`
	StatusUpdateTime string      `json:"status_update_time" alias:"升级更新时间"`
}

// 升级失败详情
var upgreadFailure []string = []string{"-1", "-2", "-3", "-4"}

type OtaMsg struct {
	Id     string   `json:"id,omitempty" alias:"序号"`
	Params OtaModel `json:"params,omitempty" alias:"参数"`
}
type OtaModel struct {
	PackageVersion string `json:"version,omitempty" alias:"版本号"` //版本号
	PackageModule  string `json:"module,omitempty" alias:"描述"`
}

// 列表
func (*TpOtaDeviceService) GetTpOtaDeviceList(PaginationValidate valid.TpOtaDevicePaginationValidate) (bool, []map[string]interface{}, int64) {
	sqlWhere := `select od.*,d.name,gd.device_code from tp_ota_device od left join device d on od.device_id=d.id left join tp_generate_device gd on od.device_id =gd.device_id where 1=1`
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
	if PaginationValidate.UpgradeStatus != "" {
		values = append(values, PaginationValidate.UpgradeStatus)
		where += " and od.upgrade_status = ?"
	}
	sqlWhere += where
	sqlWhereCount += where
	var count int64
	result := psql.Mydb.Raw(sqlWhereCount, values...).Scan(&count)
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

// 状态详情
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

// 批量插入数据
func (*TpOtaDeviceService) AddBathTpOtaDevice(tp_ota_device []models.TpOtaDevice) ([]models.TpOtaDevice, error) {
	result := psql.Mydb.Create(&tp_ota_device)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_ota_device, result.Error
	}
	return tp_ota_device, nil
}

// 设备状态修改
// 0-待推送 1-已推送 2-升级中 修改为已取消
// 4-升级失败 修改为待推送
// 3-升级成功 5-已取消 不修改
func (*TpOtaDeviceService) ModfiyUpdateDevice(tp_ota_modfiystatus valid.TpOtaDeviceIdValidate) error {
	var devices []models.TpOtaDevice
	db := psql.Mydb.Model(&models.TpOtaDevice{})
	status_detail := ""
	//OtaTaskId存在的时候，属于单个操作
	if tp_ota_modfiystatus.OtaTaskId != "" && tp_ota_modfiystatus.Id != "" {
		var tpOtaDevice = models.TpOtaDevice{UpgradeStatus: tp_ota_modfiystatus.UpgradeStatus, StatusUpdateTime: time.Now().Format("2006-01-02 15:04:05"), StatusDetail: status_detail}
		if tp_ota_modfiystatus.UpgradeStatus == "5" {
			status_detail = "已取消"
			tpOtaDevice.StatusDetail = "手动取消升级"
		} else if tp_ota_modfiystatus.UpgradeStatus == "0" {
			status_detail = "开始重新升级"
			// 重新升级需要把进度置为0
			tpOtaDevice.UpgradeProgress = "0"
			tpOtaDevice.StatusDetail = "手动开始重新升级"
		}
		if err := db.Where("ota_task_id=? and id=? ", tp_ota_modfiystatus.OtaTaskId, tp_ota_modfiystatus.Id).Updates(&tpOtaDevice).Error; err != nil {
			return err
		} else {
			// 重新升级需要推送升级包
			if tp_ota_modfiystatus.UpgradeStatus == "0" {
				logs.Info("重新升级需要推送升级包")
				// 查询待升级设备信息
				if err := db.Where("ota_task_id=? and id = ?", tp_ota_modfiystatus.OtaTaskId, tp_ota_modfiystatus.Id).Find(&devices).Error; err != nil {
					return err
				}
				// 查询设备信息
				var deviceList []models.Device
				if err := psql.Mydb.Model(&models.Device{}).Where("id=?", devices[0].DeviceId).Find(&deviceList).Error; err != nil {
					return err
				}
				var tpOtaTask models.TpOtaTask
				// 查询固件设计任务
				if err := psql.Mydb.Model(&models.TpOtaTask{}).Where("id=?", tp_ota_modfiystatus.OtaTaskId).First(&tpOtaTask).Error; err != nil {
					return err
				}
				var tpOtaDeviceService TpOtaDeviceService
				// 推送升级包
				if err := tpOtaDeviceService.OtaToUpgradeMsg(deviceList, tpOtaTask.OtaId, tp_ota_modfiystatus.OtaTaskId); err != nil {
					return err
				}
			}
		}
		return nil
	} else {
		//任务下所有设备操作
		if err := db.Where("ota_task_id=? ", tp_ota_modfiystatus.OtaTaskId).Find(&devices).Error; err != nil {
			return err
		}
		psql.Mydb.Model(&models.TpOtaTask{}).Where("ota_task_id=?", tp_ota_modfiystatus.OtaTaskId).Update("task_status", "2")
	}
	for _, device := range devices {
		if device.UpgradeStatus == "0" || device.UpgradeStatus == "1" || device.UpgradeStatus == "2" {
			psql.Mydb.Model(&device).Updates(&models.TpOtaDevice{UpgradeStatus: "5", StatusUpdateTime: time.Now().Format("2006-01-02 15:04:05"), StatusDetail: status_detail})
		}
	}
	return nil
}

// 接收升级进度信息
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
	result_a := psql.Mydb.Model(models.Device{}).Select("id").Where("token = ?", payload.Token).First(&deviceid)
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
	logs.Info("收到设备", deviceid, "升级进度信息:", progressMsg)
	logs.Info("-------------------------------")
	// 很多其他公司，可能只能int型 只能文本 不合适
	switch progressMsg.UpgradeProgress.(type) {
	case float64: // json转的数值 100%为 float64
		// 数值转文本
		progressMsg.UpgradeProgress = strconv.FormatInt(int64(progressMsg.UpgradeProgress.(float64)), 10) // 抛弃小数

	case string:
		// 直接越过
	default:
		logs.Error("不支持的数据类型")
		return false
	}

	//查询升级信息对应的设备
	progressMsg.StatusUpdateTime = fmt.Sprint(time.Now().Format("2006-01-02 15:04:05"))
	var otadevice models.TpOtaDevice
	psql.Mydb.Raw(`select d.* from tp_ota o left join tp_ota_task t on t.ota_id=o.id left join tp_ota_device d on d.ota_task_id=t.id where o.package_module = ? and t.task_status !='2' 
	             and d.device_id=? and d.upgrade_status not in ('0','3','5') `, progressMsg.Module, deviceid).Scan(&otadevice)
	if otadevice.Id != "" && otadevice.OtaTaskId != "" {
		if otadevice.UpgradeStatus == "4" {
			return false
		}
		//升级失败判断
		intProgress, err := strconv.Atoi(progressMsg.UpgradeProgress.(string))
		if err != nil {
			progressMsg.StatusDetail = progressMsg.UpgradeProgress.(string) + progressMsg.StatusDetail
		}
		isUpgradeSuccess := utils.In(progressMsg.UpgradeProgress.(string), upgreadFailure)
		if isUpgradeSuccess {
			logs.Info("设备%s升级失败", deviceid)
			progressMsg.UpgradeStatus = "4"
			progressMsg.StatusDetail = progressMsg.UpgradeProgress.(string) + progressMsg.StatusDetail
		} else if progressMsg.UpgradeProgress == "100" {
			//升级成功判断
			logs.Info("设备%s升级成功", deviceid)
			progressMsg.UpgradeStatus = "3"
			psql.Mydb.Model(&models.Device{}).Where("id = ?", deviceid).Update("current_version", progressMsg.Module)
		} else {
			// 判断升级步骤是不是数字字符串0-100之间
			if intProgress > 0 && intProgress < 100 {
				logs.Info("设备%s升级进度更新%d", deviceid, intProgress)
				progressMsg.UpgradeStatus = "2"
			} else {
				progressMsg.StatusDetail = progressMsg.UpgradeProgress.(string) + progressMsg.StatusDetail
			}

		}
		//修改升级信息
		psql.Mydb.Model(&models.TpOtaDevice{}).Where("id = ? and ota_task_id=?", otadevice.Id, otadevice.OtaTaskId).Updates(models.TpOtaDevice{UpgradeProgress: progressMsg.UpgradeProgress.(string), UpgradeStatus: progressMsg.UpgradeStatus, StatusUpdateTime: progressMsg.StatusUpdateTime, StatusDetail: progressMsg.StatusDetail})
		return true
	}
	return false
}

// 接收固件版本信息
func (*TpOtaDeviceService) OtaToinformMsgProcOther(body []byte, topic string) bool {
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
	result_a := psql.Mydb.Model(models.Device{}).Select("id").Where("token = ?", payload.Token).First(&deviceid)
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
	//修改设备当前版本
	logs.Info(otamsg)
	result := psql.Mydb.Model(&models.Device{}).Where("id = ?", deviceid).Update("current_version", otamsg.Params.PackageVersion)
	if result.Error != nil {
		logs.Error(result.Error)
		return false
	}
	// //查询升级信息对应的设备
	// var otadevice models.TpOtaDevice
	// result = psql.Mydb.Raw(`select d.* from tp_ota o left join tp_ota_task t on t.ota_id=o.id left join tp_ota_device d on d.ota_task_id=t.id where o.package_module = ? and t.task_status !='2' and d.device_id =?`, otamsg.OtaModel.PackageModule, deviceid).Scan(&otadevice)
	// if result.Error != nil {
	// 	logs.Error(result.Error)
	// 	return false
	// }
	// if otadevice.DeviceId != "" && otadevice.OtaTaskId != "" {

	// 	psql.Mydb.Model(&models.TpOtaDevice{}).Where("id = ? and ota_task_id=?", otadevice.DeviceId, otadevice.OtaTaskId).Update("current_version", otamsg.OtaModel.PackageVersion)
	// 	return true
	// }
	logs.Info("更新版本成功")
	return true

}

// 推送升级包到设备
func (*TpOtaDeviceService) OtaToUpgradeMsg(devices []models.Device, otaid string, otataskid string) error {
	var ota models.TpOta
	//查询ota信息
	if err := psql.Mydb.Where("id=?", otaid).Find(&ota).Error; err != nil {
		logs.Error("不存在该ota固件")
		return errors.New("无对应固件")
	}
	for _, device := range devices {
		//设备是否在线
		var deviceService DeviceService
		if idOnline, err := deviceService.IsDeviceOnline(device.ID); err != nil {
			logs.Error(err.Error())
		} else if !idOnline {
			psql.Mydb.Model(&models.TpOtaDevice{}).Where("device_id = ? and ota_task_id = ?", device.ID, otataskid).Updates(map[string]interface{}{"upgrade_status": "4", "status_detail": "设备不在线，升级失败", "status_update_time": time.Now().Format("2006-01-02 15:04:05")})
			logs.Info("OTA任务id为%s,设备id为%s,设备不在线", otataskid, device.ID)
			continue
		}
		//检查这个设备在其他任务中是否正在升级中
		var count int64
		//状态 0-待推送 1-已推送 2-升级中 3-升级成功 4-升级失败 5-已取消
		psql.Mydb.Where("device_id = ? and ota_task_id != ? and upgrade_status  in ('0','1','2')", device.ID, otataskid).Count(&count)
		if count != 0 {
			psql.Mydb.Model(&models.TpOtaDevice{}).Where("device_id = ? and ota_task_id = ?", device.ID, otataskid).Updates(map[string]interface{}{"upgrade_status": "4", "status_detail": "上次升级未完成", "status_update_time": time.Now().Format("2006-01-02 15:04:05")})
			logs.Info("OTA任务id为%s,设备id为%s,有任务进行中", otataskid, device.ID)
			continue
		}
		//检查升级包
		var otamsg = make(map[string]interface{})
		// 获取随机九位数字并转换为字符串
		rand.Seed(time.Now().UnixNano())
		randNum := rand.Intn(999999999)
		otamsg["id"] = strconv.Itoa(randNum)
		otamsg["code"] = "200"
		var otamsgparams = make(map[string]interface{})
		otamsgparams["version"] = ota.PackageVersion
		otamsgparams["url"] = ota.PackageUrl
		otamsgparams["signMethod"] = ota.SignatureAlgorithm
		otamsgparams["sign"] = ota.Sign
		otamsgparams["module"] = ota.PackageModule
		//其他配置格式成map
		var m map[string]interface{}
		err := json.Unmarshal([]byte(ota.AdditionalInfo), &m)
		if err != nil {
			return err
		}
		otamsgparams["extData"] = m
		otamsg["params"] = otamsgparams
		msgdata, json_err := json.Marshal(otamsg)
		if json_err != nil {
			logs.Error(json_err.Error())
		} else {
			psql.Mydb.Model(&models.TpOtaDevice{}).Where("device_id = ? and ota_task_id = ?", device.ID, otataskid).Updates(map[string]interface{}{"upgrade_status": "1", "status_detail": "已通知设备", "status_update_time": time.Now().Format("2006-01-02 15:04:05")})
			go sendmqtt.SendOtaAdress(msgdata, device.Token)
		}
	}
	return nil
}
