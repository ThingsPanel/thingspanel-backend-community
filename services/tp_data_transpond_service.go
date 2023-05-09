package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	"ThingsPanel-Go/models"
	tphttp "ThingsPanel-Go/others/http"
	"encoding/json"
	"errors"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"strconv"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpDataTranspondService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

const DeviceMessageTypeAttributeReport = 1 // 属性上报
const DeviceMessageTypeAttributeSend = 2   // 属性下发
const DeviceMessageTypeEventReport = 3     // 事件上报
const DeviceMessageTypeCustomReport = 4    // 自定义上报

// DTI- device id
const DeviceTranspondInfoRedisKeyPrefix = "DTI2-%s" //缓存前缀

const DataTranspondDetailSwitchOpen = 1  // 开启转发
const DataTranspondDetailSwitchClose = 0 // 关闭转发

// 新建转发规则
func (*TpDataTranspondService) AddTpDataTranspond(
	dataTranspond models.TpDataTranspon,
	dataTranspondDetail []models.TpDataTransponDetail,
	dataTranspondTarget []models.TpDataTransponTarget,
) bool {

	err := psql.Mydb.Create(&dataTranspond)
	if err.Error != nil {
		errors.Is(err.Error, gorm.ErrRecordNotFound)
		return false
	}

	err = psql.Mydb.Create(&dataTranspondDetail)
	if err.Error != nil {
		errors.Is(err.Error, gorm.ErrRecordNotFound)
		return false
	}

	err = psql.Mydb.Create(&dataTranspondTarget)
	if err.Error != nil {
		errors.Is(err.Error, gorm.ErrRecordNotFound)
		return false
	}

	return true
}

func (*TpDataTranspondService) AddTpDataTranspondForEdit(
	dataTranspondDetail []models.TpDataTransponDetail,
	dataTranspondTarget []models.TpDataTransponTarget,
) bool {

	err := psql.Mydb.Create(&dataTranspondDetail)
	if err.Error != nil {
		errors.Is(err.Error, gorm.ErrRecordNotFound)
		return false
	}

	err = psql.Mydb.Create(&dataTranspondTarget)
	if err.Error != nil {
		errors.Is(err.Error, gorm.ErrRecordNotFound)
		return false
	}

	return true
}

func (*TpDataTranspondService) GetListByTenantId(
	offset int, pageSize int, tenantId string) ([]models.TpDataTranspon, int64) {

	var dataTranspon []models.TpDataTranspon
	var count int64

	tx := psql.Mydb.Model(&models.TpDataTranspon{})
	tx.Where("tenant_id = ?", tenantId)

	err := tx.Count(&count).Error
	if err != nil {
		logs.Error(err.Error())
		return dataTranspon, count
	}

	err = tx.Order("create_time desc").Limit(pageSize).Offset(offset).Find(&dataTranspon).Error
	if err != nil {
		logs.Error(err.Error())
		return dataTranspon, count
	}
	return dataTranspon, count
}

// 根据 dataTranspondId 查找 tp_data_transpond 表
func (*TpDataTranspondService) GetDataTranspondByDataTranspondId(dataTranspondId string) (models.TpDataTranspon, bool) {
	var dataTranspon models.TpDataTranspon
	tx := psql.Mydb.Model(&models.TpDataTranspon{})
	err := tx.Where("id = ?", dataTranspondId).Find(&dataTranspon).Error
	if err != nil {
		logs.Error(err.Error())
		return dataTranspon, false
	}
	return dataTranspon, true
}

// 根据 dataTranspondId 查找 tp_data_transpond_detail 表
func (*TpDataTranspondService) GetDataTranspondDetailByDataTranspondId(dataTranspondId string) ([]models.TpDataTransponDetail, bool) {
	var dataTranspondDetail []models.TpDataTransponDetail
	tx := psql.Mydb.Model(&models.TpDataTransponDetail{})
	err := tx.Where("data_transpond_id = ?", dataTranspondId).Omit("id").Find(&dataTranspondDetail).Error
	if err != nil {
		logs.Error(err.Error())
		return dataTranspondDetail, false
	}
	return dataTranspondDetail, true
}

// 根据 dataTranspondId 查找 tp_data_transpond_target 表
func (*TpDataTranspondService) GetDataTranspondTargetByDataTranspondId(dataTranspondId string) ([]models.TpDataTransponTarget, bool) {
	var dataTranspondTarget []models.TpDataTransponTarget
	tx := psql.Mydb.Model(&models.TpDataTransponTarget{})
	err := tx.Where("data_transpond_id = ?", dataTranspondId).Omit("id").Find(&dataTranspondTarget).Error
	if err != nil {
		logs.Error(err.Error())
		return dataTranspondTarget, false
	}
	return dataTranspondTarget, true
}

func (*TpDataTranspondService) UpdateDataTranspondStatusByDataTranspondId(dataTranspondId string, swtich int) bool {
	tx := psql.Mydb.Model(&models.TpDataTranspon{})
	err := tx.Where("id = ?", dataTranspondId).Update("status", swtich).Error
	if err != nil {
		logs.Error(err.Error())
		return false
	}
	return true
}

func (*TpDataTranspondService) DeletaByDataTranspondId(dataTranspondId string) bool {

	result := psql.Mydb.Where("id = ?", dataTranspondId).Delete(&models.TpDataTranspon{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	result = psql.Mydb.Where("data_transpond_id = ?", dataTranspondId).Delete(&models.TpDataTransponDetail{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	result = psql.Mydb.Where("data_transpond_id = ?", dataTranspondId).Delete(&models.TpDataTransponTarget{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	return true
}

func (*TpDataTranspondService) DeletaDeviceTargetByDataTranspondId(dataTranspondId string) bool {

	result := psql.Mydb.Where("data_transpond_id = ?", dataTranspondId).Delete(&models.TpDataTransponDetail{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	result = psql.Mydb.Where("data_transpond_id = ?", dataTranspondId).Delete(&models.TpDataTransponTarget{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	return true
}

func (*TpDataTranspondService) UpdateDataTranspond(input models.TpDataTranspon) bool {
	result := psql.Mydb.Where("id = ?", input.Id).Save(&input)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}
	return true
}

// 删除/编辑规则，和修改开关，直接删除缓存
func (*TpDataTranspondService) DeleteCacheByDataTranspondId(dataTranspondId string) bool {
	var m []models.TpDataTransponDetail
	result := psql.Mydb.Where("data_transpond_id = ?", dataTranspondId).Find(&m)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}
	for _, v := range m {
		key := fmt.Sprintf(DeviceTranspondInfoRedisKeyPrefix, v.DeviceId)
		redis.DelKey(key)
	}
	return true
}

// 通过设备ID，查询是否需要转发，以及转发的地址
// 会设置缓存
func GetDeviceDataTranspondInfo(deviceId string) (bool, DataTranspondCache) {

	var data DataTranspondCache
	// 从redis中获取数据
	redisKey := fmt.Sprintf(DeviceTranspondInfoRedisKeyPrefix, deviceId)
	redisInfo := redis.GetStr(redisKey)
	fmt.Println("key", redisKey)
	fmt.Println(redisInfo)
	// 如果是 {} 证明为空缓存，不继续查询数据库，直接返回
	if redisInfo == "{}" {
		return false, data
	}

	if len(redisInfo) > 10 {
		// 尝试解析json
		err := json.Unmarshal([]byte(redisInfo), &data)
		if err != nil {
			return false, data
		}
	}

	// 如果json有数据，证明开关为开，直接返回，不查询数据库
	if len(data.DeviceId) > 1 {
		return true, data
	}

	// 查询 tp_data_transpond_detail 中是否有配置该设备的转发信息
	e, resTpDataTransponDetail := getDataTranspondDetailByDeviceId(deviceId)
	if !e {
		return false, data
	}

	// 配置为空，这是空缓存，返回
	if resTpDataTransponDetail.DataTranspondId == "" {
		_ = setEmptyCache(redisKey)
		return false, data
	}

	e, resTpDataTranspon := getDataTranspondSwitch(resTpDataTransponDetail.DataTranspondId)
	if !e {
		return false, data
	}

	// 如果关闭，设置空缓存
	if resTpDataTranspon.Status == DataTranspondDetailSwitchClose {
		_ = setEmptyCache(redisKey)
		return false, data
	}

	// 如果开启，查询转发目标
	e, resTpDataTransponTarget := getDataTranspondTarget(resTpDataTransponDetail.DataTranspondId)
	if !e {
		return false, data
	}

	// 组装信息，设置缓存
	data.DeviceId = deviceId
	data.Script = resTpDataTranspon.Script
	data.MessageType = resTpDataTransponDetail.MessageType

	var targetInfo dataTranspondTargetCache
	switch resTpDataTransponTarget.DataType {
	case models.DataTypeURL:
		targetInfo.URL = resTpDataTransponTarget.Target
	case models.DataTypeMQTT:
		var d dataTransponTargetInfoMQTTCache
		err := json.Unmarshal([]byte(resTpDataTransponTarget.Target), &d)
		if err != nil {
			return false, data
		}
		targetInfo.MQTT = d
	}

	data.TargetInfo = targetInfo
	// 设置缓存
	_ = setCache(redisKey, data)
	return true, data
}

// 用于验证是否需要转发，以及转发数据
func CheckAndTranspondData(deviceId string, msg []byte, messageType int) {
	fmt.Println("deviceId", deviceId)
	ok, data := GetDeviceDataTranspondInfo(deviceId)
	// 无转发配置或messageType不符
	if !ok || data.MessageType != messageType {
		fmt.Println("无转发配置或messageType不符")
		return
	}
	// 转发到mqtt或http接口
	if len(data.TargetInfo.URL) > 1 {
		// send post
		_, _ = tphttp.Post(data.TargetInfo.URL, string(msg))
	}

	if len(data.TargetInfo.MQTT.Host) > 1 {
		// send mqtt
		ConnectAndSend(data, msg)
	}
}

func ConnectAndSend(t DataTranspondCache, msg []byte) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%s", t.TargetInfo.MQTT.Host, strconv.Itoa(t.TargetInfo.MQTT.Port)))
	fmt.Println(fmt.Sprintf("tcp://%s:%s", t.TargetInfo.MQTT.Host, strconv.Itoa(t.TargetInfo.MQTT.Port)))
	opts.SetClientID(t.TargetInfo.MQTT.ClientId)
	opts.SetUsername(t.TargetInfo.MQTT.UserName)
	opts.SetPassword(t.TargetInfo.MQTT.Password)
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		return
	}
	m := client.Publish(t.TargetInfo.MQTT.Topic, 0, false, msg)
	if m.Error() != nil {
		fmt.Println(m.Error())
	}
	defer client.Disconnect(1)
}

// 查询 tp_data_transpond_detail 中是否有配置该设备的转发信息
func getDataTranspondDetailByDeviceId(deviceId string) (e bool, data models.TpDataTransponDetail) {
	resultA := psql.Mydb.Where("device_id = ?", deviceId).First(&data)
	// 出错或未配置
	if resultA.Error != nil {
		logs.Error(resultA.Error.Error())
		return false, data
	}
	return true, data
}

// 查询 tp_data_transpond 查看转发是否启用
func getDataTranspondSwitch(dataTranspondId string) (e bool, data models.TpDataTranspon) {
	resultB := psql.Mydb.Where("id = ?", dataTranspondId).First(&data)
	if resultB.Error != nil {
		logs.Error(resultB.Error.Error())
		return false, data
	}
	return true, data
}

// 查询 tp_data_transpond_target 查询转发目标
func getDataTranspondTarget(dataTranspondId string) (e bool, data models.TpDataTransponTarget) {
	resultB := psql.Mydb.Where("data_transpond_id = ?", dataTranspondId).First(&data)
	if resultB.Error != nil {
		logs.Error(resultB.Error.Error())
		return false, data
	}
	return true, data
}

func setEmptyCache(key string) error {
	e := redis.SetStr(key, "{}", 1*time.Hour)
	return e

}

func setCache(key string, data DataTranspondCache) error {
	str, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return redis.SetStr(key, string(str), 1*time.Hour)
}

type DataTranspondCache struct {
	DeviceId    string                   `json:"device_id"`
	MessageType int                      `json:"message_type"`
	Script      string                   `json:"script"`
	TargetInfo  dataTranspondTargetCache `json:"target_info"`
}

type dataTranspondTargetCache struct {
	URL  string                          `json:"url"`
	MQTT dataTransponTargetInfoMQTTCache `json:"mqtt"`
}

type dataTransponTargetInfoMQTTCache struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	UserName string `json:"username"`
	Password string `json:"password"`
	ClientId string `json:"client_id"`
	Topic    string `json:"topic"`
}
