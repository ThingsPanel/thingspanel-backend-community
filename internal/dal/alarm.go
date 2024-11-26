package dal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	model "project/internal/model"
	query "project/internal/query"
	"project/pkg/global"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/datatypes"
	"gorm.io/gen"
	"gorm.io/gorm"
)

func CreateAlarmConfig(d *model.AlarmConfig) error {
	return query.AlarmConfig.Create(d)
}

func UpdateAlarmConfig(d *model.AlarmConfig) error {
	info, err := query.AlarmConfig.Updates(d)
	if err != nil {
		return err
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("no data updated")
	}
	return nil
}

func DeleteAlarmConfig(id string) error {
	info, err := query.AlarmConfig.Where(query.AlarmConfig.ID.Eq(id)).Delete()
	if err != nil {
		return err
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("no data deleted")
	}
	return nil
}

func GetAlarmByID(id string) (*model.AlarmConfig, error) {

	data, err := query.AlarmConfig.Where(query.AlarmConfig.ID.Eq(id)).First()
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetAlarmConfigListByPage(d *model.GetAlarmConfigListByPageReq) (int64, interface{}, error) {
	q := query.AlarmConfig
	var count int64
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(d.TenantID))
	if d.Name != nil && *d.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *d.Name)))
	}
	if d.AlarmLevel != nil && *d.AlarmLevel != "" {
		queryBuilder = queryBuilder.Where(q.AlarmLevel.Eq(*d.AlarmLevel))
	}
	if d.Enabled != "" {
		queryBuilder = queryBuilder.Where(q.Enabled.Eq(d.Enabled))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	queryBuilder = queryBuilder.Order(q.CreatedAt.Desc())
	queryBuilder = queryBuilder.LeftJoin(query.NotificationGroup, q.NotificationGroupID.EqCol(query.NotificationGroup.ID))

	if d.Page != 0 && d.PageSize != 0 {
		queryBuilder = queryBuilder.Offset((d.Page - 1) * d.PageSize).Limit(d.PageSize)
	}
	list := make([]map[string]interface{}, 0)
	err = queryBuilder.Select(
		q.ALL, query.NotificationGroup.Name.As("notification_group_name")).Scan(&list)

	if err != nil {
		return 0, nil, err
	}
	return count, list, nil
}

func CreateAlarmInfo(d *model.AlarmInfo) error {
	return query.AlarmInfo.Create(d)
}

func GetAlarmInfoByID(id string) (*model.AlarmInfo, error) {

	data, err := query.AlarmInfo.Where(query.AlarmInfo.ID.Eq(id)).First()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("no data found")
	}
	return data, nil
}

func UpdateAlarmInfo(d *model.AlarmInfo) error {
	info, err := query.AlarmInfo.Updates(d)
	if err != nil {
		return err
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("no data updated")
	}
	return nil
}

func UpdateAlarmInfoBatch(req *model.UpdateAlarmInfoBatchReq, userid string) error {
	info, err := query.AlarmInfo.Where(query.AlarmInfo.ID.In(req.Id...)).
		Updates(map[string]interface{}{
			"processing_result": req.ProcessingResult,
			"content":           req.ProcessingInstructions,
			"processor":         userid})
	if err != nil {
		return err
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("no data updated")
	}
	return nil
}

func GetAlarmInfoListByPage(d *model.GetAlarmInfoListByPageReq) (int64, interface{}, error) {
	q := query.AlarmInfo
	var count int64
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(d.TenantID))
	if d.StartTime != nil && d.EndTime != nil {
		queryBuilder = queryBuilder.Where(q.AlarmTime.Between(*d.StartTime, *d.EndTime))
	}

	if d.ProcessingResult != nil && *d.ProcessingResult != "" {
		queryBuilder = queryBuilder.Where(q.ProcessingResult.Eq(*d.ProcessingResult))
	}

	if d.AlarmLevel != nil && *d.AlarmLevel != "" {
		queryBuilder = queryBuilder.Where(q.AlarmLevel.Eq(*d.AlarmLevel))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	queryBuilder = queryBuilder.LeftJoin(
		query.AlarmConfig, q.AlarmConfigID.EqCol(query.AlarmConfig.ID)).
		LeftJoin(query.User, q.Processor.EqCol(query.User.ID))
	queryBuilder = queryBuilder.Order(q.AlarmTime.Desc())

	if d.Page != 0 && d.PageSize != 0 {
		queryBuilder = queryBuilder.Offset((d.Page - 1) * d.PageSize).Limit(d.PageSize)
	}
	list := make([]map[string]interface{}, 0)
	err = queryBuilder.Select(q.ALL,
		query.AlarmConfig.Name.As("alarm_config_name"),
		query.AlarmConfig.AlarmLevel.As("alarm_level"),
		query.User.Name.As("processor_name")).Scan(&list)
	if err != nil {
		return 0, nil, err
	}
	return count, list, nil
}

func GetAlarmHistoryListByPage(d *model.GetAlarmHisttoryListByPage, tenantID string) (int64, interface{}, error) {
	q := query.AlarmHistory
	var count int64
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantID))

	if d.StartTime != nil && d.EndTime != nil && !d.StartTime.IsZero() && !d.EndTime.IsZero() {
		queryBuilder = queryBuilder.Where(q.CreateAt.Between(*d.StartTime, *d.EndTime))
	}

	if d.AlarmStatus != nil && *d.AlarmStatus != "" {
		queryBuilder = queryBuilder.Where(q.AlarmStatus.Eq(*d.AlarmStatus))
	}

	if d.DeviceId != nil && *d.DeviceId != "" {
		//queryBuilder = queryBuilder.Where(q.AlarmDeviceList.Like(fmt.Sprintf("%%%s%%", *d.DeviceId)))
		queryBuilder = queryBuilder.Where(gen.Cond(datatypes.JSONQuery("alarm_device_list").HasKey(*d.DeviceId))...)
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	queryBuilder = queryBuilder.LeftJoin(
		query.AlarmConfig, q.AlarmConfigID.EqCol(query.AlarmConfig.ID))

	queryBuilder = queryBuilder.Order(q.CreateAt.Desc())

	if d.Page != 0 && d.PageSize != 0 {
		queryBuilder = queryBuilder.Offset((d.Page - 1) * d.PageSize).Limit(d.PageSize)
	}
	list := make([]map[string]interface{}, 0)
	err = queryBuilder.Select(q.ALL,
		query.AlarmConfig.Name.As("alarm_config_name"),
		query.AlarmConfig.AlarmLevel.As("alarm_level"),
	).Scan(&list)
	if err != nil {
		return 0, nil, err
	}
	for i, v := range list {
		var (
			deviceIds  []string
			deviceList = make([]map[string]interface{}, 0)
		)
		_ = json.Unmarshal([]byte(v["alarm_device_list"].(string)), &deviceIds)
		_ = query.Device.Where(query.Device.ID.In(deviceIds...)).Select(query.Device.ID, query.Device.Name).Scan(&deviceList)
		list[i]["alarm_device_list"] = deviceList
	}
	//查询设备命令
	return count, list, nil
}

func AlarmHistorySave(history *model.AlarmHistory) error {

	return query.AlarmHistory.Save(history)
}
func AlarmHistoryDescUpdate(req *model.AlarmHistoryDescUpdateReq, tenantID string) error {
	result, err := query.AlarmHistory.Where(query.AlarmHistory.ID.Eq(req.AlarmHistoryId), query.AlarmHistory.TenantID.Eq(tenantID)).UpdateColumn(query.AlarmHistory.Description, req.Description)
	if err != nil {
		return err
	}
	if result.RowsAffected == 0 {
		return errors.New("设置告警描述失败")
	}
	return nil
}

func GetDeviceAlarmStatus(req *model.GetDeviceAlarmStatusReq) bool {
	result, err := query.AlarmHistory.Where(gen.Cond(datatypes.JSONQuery("alarm_device_list").HasKey(req.DeviceId))...).Order(query.AlarmHistory.CreateAt.Desc()).First()
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		return false
	}
	if result.AlarmStatus == "N" {
		return false
	}
	return true
}

func GetConfigByDevice(req *model.GetDeviceAlarmStatusReq) ([]model.AlarmConfig, error) {
	var result []map[string]interface{}
	err := query.AlarmHistory.Where(gen.Cond(datatypes.JSONQuery("alarm_device_list").HasKey(req.DeviceId))...).
		Select(query.AlarmHistory.AlarmConfigID, query.AlarmHistory.AlarmConfigID.Count()).Group(query.AlarmHistory.AlarmConfigID).Scan(&result)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}

	var (
		configId []string
		config   []model.AlarmConfig
	)
	for _, v := range result {
		configId = append(configId, v["alarm_config_id"].(string))
	}
	return config, query.AlarmConfig.Where(query.AlarmConfig.ID.In(configId...)).Scan(&config)
}

func GetAlarmNameWithCache(alarmId string) string {
	redis := global.REDIS
	cacheKey := fmt.Sprintf("GetAlarmNameWithCache:alarmId:%s", alarmId)
	var result string
	err := redis.Get(cacheKey).Scan(&result)
	if err == nil && result != "" {
		return result
	}
	alarmConfig, err := query.AlarmConfig.Where(query.AlarmConfig.ID.Eq(alarmId)).Select(query.AlarmConfig.Name).First()
	if err != nil {
		return ""
	}
	redis.Set(cacheKey, alarmConfig.Name, time.Hour)
	return alarmConfig.Name
}
