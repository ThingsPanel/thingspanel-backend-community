package dal

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	model "project/internal/model"
	query "project/internal/query"
	utils "project/pkg/utils"

	"gorm.io/gen"

	"github.com/sirupsen/logrus"
)

func CreateDeviceConfig(deviceconfig *model.DeviceConfig) error {
	return query.DeviceConfig.Create(deviceconfig)
}

// 修改配置模板id
func UpdateDeviceConfigTemplateID(id string, templateID *string) error {
	// nil值也要更新
	_, err := query.DeviceConfig.Where(query.DeviceConfig.ID.Eq(id)).Update(query.DeviceConfig.DeviceTemplateID, templateID)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func UpdateDeviceConfig(id string, condsMap map[string]interface{}) error {
	p := query.DeviceConfig
	t := time.Now().UTC()
	condsMap["updated_at"] = &t
	info, err := query.DeviceConfig.Where(p.ID.Eq(id)).Updates(condsMap)
	if err != nil {
		logrus.Error(err)
		return err
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("update deviceconfig failed, no rows affected")
	}
	return err
}

func DeleteDeviceConfig(id string) error {
	_, err := query.DeviceConfig.Where(query.DeviceConfig.ID.Eq(id)).Delete()
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func GetDeviceConfigByID(id string) (*model.DeviceConfig, error) {
	deviceconfig, err := query.DeviceConfig.Where(query.DeviceConfig.ID.Eq(id)).First()
	if err != nil {
		logrus.Error(err)
	}
	if deviceconfig == nil {
		return nil, fmt.Errorf("deviceconfig not found: %s", id)
	}
	return deviceconfig, err
}

func GetDeviceConfigListByPage(deviceconfig *model.GetDeviceConfigListByPageReq, claims *utils.UserClaims) (int64, interface{}, error) {
	q := query.DeviceConfig
	var count int64
	var data []model.DeviceConfigRsp
	var deviceconfigList []*model.DeviceConfig
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(claims.TenantID))

	if deviceconfig.DeviceTemplateId != nil && *deviceconfig.DeviceTemplateId != "" {
		queryBuilder = queryBuilder.Where(q.DeviceTemplateID.Eq(*deviceconfig.DeviceTemplateId))
	}
	if deviceconfig.DeviceType != nil && *deviceconfig.DeviceType != "" {
		queryBuilder = queryBuilder.Where(q.DeviceType.Eq(*deviceconfig.DeviceType))
	}
	if deviceconfig.ProtocolType != nil && *deviceconfig.ProtocolType != "" {
		queryBuilder = queryBuilder.Where(q.ProtocolType.Eq(*deviceconfig.ProtocolType))
	}
	if deviceconfig.Name != nil && *deviceconfig.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *deviceconfig.Name)))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, deviceconfigList, err
	}

	if deviceconfig.Page != 0 && deviceconfig.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(deviceconfig.PageSize)
		queryBuilder = queryBuilder.Offset((deviceconfig.Page - 1) * deviceconfig.PageSize)
	}
	queryBuilder.Order(q.CreatedAt.Desc())
	deviceconfigList, err = queryBuilder.Select().Find()
	if err != nil {
		logrus.Error(err)
		return count, deviceconfigList, err
	}
	for i := range deviceconfigList {
		c, err := query.Device.
			Where(query.Device.ActivateFlag.Eq("active")).
			Where(query.Device.DeviceConfigID.Eq(deviceconfigList[i].ID)).Count()
		if err != nil {
			logrus.Error(err)
		}
		data = append(data, model.DeviceConfigRsp{DeviceConfig: deviceconfigList[i], DeviceCount: c})
	}

	return count, data, err
}

// 获取设备配置下拉菜单
func GetDeviceConfigSelectList(deviceConfigName *string, tenantID string, deviceType *string, protocolType *string) (any, error) {
	q := query.DeviceConfig
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantID))
	if deviceConfigName != nil {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *deviceConfigName)))
	}
	if deviceType != nil {
		queryBuilder = queryBuilder.Where(q.DeviceType.Eq(*deviceType))
	}
	if protocolType != nil {
		queryBuilder = queryBuilder.Where(q.ProtocolType.Eq(*protocolType))
	}
	var data []map[string]interface{}
	err := queryBuilder.Select(q.ID, q.Name).Scan(&data)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return data, err
}

func BatchUpdateDeviceConfig(req *model.BatchUpdateDeviceConfigReq) error {
	q := query.Device
	var devices []string
	err := q.Where(q.ID.In(req.DeviceIds...), q.DeviceConfigID.IsNotNull(), q.DeviceConfigID.Neq(req.DeviceConfigID)).Select(q.ID).Scan(&devices)
	if err != nil {
		return err
	}
	if len(devices) > 0 {
		return errors.Errorf("设备id:%s已绑定其他配置", devices)
	}
	t := time.Now().UTC()
	_, err = q.Where(q.ID.In(req.DeviceIds...)).Updates(&model.Device{DeviceConfigID: &req.DeviceConfigID, UpdateAt: &t})
	if err != nil {
		logrus.Error(err)
		return err
	}
	return err
}

type DeviceConfigQuery struct{}

func (DeviceConfigQuery) First(ctx context.Context, option ...gen.Condition) (info *model.DeviceConfig, err error) {
	info, err = query.DeviceConfig.WithContext(ctx).Where(option...).First()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (DeviceConfigQuery) Find(ctx context.Context, option ...gen.Condition) (list []*model.DeviceConfig, err error) {
	list, err = query.DeviceConfig.WithContext(ctx).Where(option...).Find()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

type DeviceConfigVo struct{}

func (DeviceConfigVo) PoToVo(deviceConfigInfo *model.DeviceConfig) (info *model.DeviceConfigsRes) {
	info = &model.DeviceConfigsRes{
		ID:         deviceConfigInfo.ID,
		Name:       deviceConfigInfo.Name,
		DeviceType: deviceConfigInfo.DeviceType,
		CreatedAt:  deviceConfigInfo.CreatedAt,
		UpdatedAt:  deviceConfigInfo.UpdatedAt,
	}
	if deviceConfigInfo.DeviceTemplateID != nil {
		info.DeviceTemplateID = *deviceConfigInfo.DeviceTemplateID
	}
	if deviceConfigInfo.ProtocolType != nil {
		info.ProtocolType = *deviceConfigInfo.ProtocolType
	}
	if deviceConfigInfo.VoucherType != nil {
		info.VoucherType = *deviceConfigInfo.VoucherType
	}
	if deviceConfigInfo.ProtocolConfig != nil {
		info.ProtocolConfig = *deviceConfigInfo.ProtocolConfig
	}
	if deviceConfigInfo.DeviceConnType != nil {
		info.DeviceConnType = *deviceConfigInfo.DeviceConnType
	}
	if deviceConfigInfo.AdditionalInfo != nil {
		info.VoucherType = *deviceConfigInfo.AdditionalInfo
	}
	if deviceConfigInfo.Description != nil {
		info.Description = *deviceConfigInfo.Description
	}
	if deviceConfigInfo.Remark != nil {
		info.Remark = *deviceConfigInfo.Remark
	}
	return
}

// func GetDeviceOnline(ctx context.Context, deviceOnlines []model.DeviceOnline) (map[string]int, error) {
// 	var (
// 		result               = make(map[string]int, 0)
// 		deviceConfigIds      []string
// 		deviveConfigOtherMap = make(map[string]model.DeviceConfigOtherConfig, 0)
// 		deviceIds            []string
// 		deviceMap            = make(map[string]string, 0)
// 	)

// 	if len(deviceOnlines) == 0 {
// 		return result, nil
// 	}
// 	for _, v := range deviceOnlines {
// 		if v.DeviceConfigId == nil || *v.DeviceConfigId == "" {
// 			continue
// 		}
// 		deviceIds = append(deviceIds, v.DeviceId)
// 		deviceConfigIds = append(deviceConfigIds, *v.DeviceConfigId)
// 		deviceMap[v.DeviceId] = *v.DeviceConfigId
// 	}
// 	list, err := query.DeviceConfig.WithContext(ctx).Where(query.DeviceConfig.ID.In(deviceConfigIds...)).Find()
// 	if err != nil {
// 		return result, nil
// 	}
// 	for _, v := range list {
// 		if v.OtherConfig == nil || *v.OtherConfig == "" {
// 			continue
// 		}
// 		var config model.DeviceConfigOtherConfig
// 		err = json.Unmarshal([]byte(*v.OtherConfig), &config)
// 		if err != nil {
// 			continue
// 		}
// 		deviveConfigOtherMap[v.ID] = config
// 	}
// 	t := query.TelemetryCurrentData
// 	rows, err := t.WithContext(ctx).Where(t.DeviceID.In(deviceIds...)).Group(t.DeviceID).Select(t.DeviceID, t.T.Max().As("ts")).Find()
// 	if err != nil {
// 		return result, nil
// 	}
// 	now := time.Now().UTC()
// 	for _, v := range rows {
// 		logrus.Warning(v.DeviceID)
// 		var (
// 			deviceConfigId string
// 			ok             bool
// 		)
// 		if deviceConfigId, ok = deviceMap[v.DeviceID]; !ok {
// 			continue
// 		}
// 		if config, ok := deviveConfigOtherMap[deviceConfigId]; ok {

// 			if config.Heartbeat > 0 {
// 				//当前时间-最近一次遥测时间 大于心跳秒数  表示离线
// 				if now.Sub(v.T).Seconds() > float64(config.Heartbeat) {
// 					result[v.DeviceID] = 0
// 				} else {
// 					result[v.DeviceID] = 1
// 				}
// 				continue
// 			}
// 			//设置了超时时间 当前时间-最近一次遥测时间 大于超时时间（分）  表示离线
// 			if config.OnlineTimeout > 0 {
// 				if now.Sub(v.T).Minutes() > float64(config.OnlineTimeout) {
// 					result[v.DeviceID] = 0
// 				} else {
// 					result[v.DeviceID] = 1
// 				}

// 			}
// 		}
// 	}
// 	return result, nil
// }

// 修改凭证类型
func UpdateDeviceConfigVoucherType(id string, voucherType *string) error {
	// nil值也要更新
	_, err := query.DeviceConfig.Where(query.DeviceConfig.ID.Eq(id)).Update(query.DeviceConfig.VoucherType, voucherType)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func GetDeviceConfigIdByName(name string) *string {
	var configId string
	err := query.DeviceConfig.Where(query.DeviceConfig.Name.Eq(name)).Select(query.DeviceConfig.ID).Limit(1).Scan(&configId)
	if err != nil {
		return nil
	}
	return &configId
}

// 根据功能模板ID查询想关联的配置模板数量
func GetDeviceConfigCountByFuncTemplateId(id string) (int64, error) {
	count, err := query.DeviceConfig.Where(query.DeviceConfig.DeviceTemplateID.Eq(id)).Count()
	if err != nil {
		logrus.Error(err)
	}
	return count, err
}
