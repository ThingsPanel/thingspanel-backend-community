package dal

import (
	"context"
	"fmt"

	model "project/model"
	query "project/query"

	"gorm.io/gen/field"
	"gorm.io/gorm"

	"gorm.io/gen"

	"github.com/sirupsen/logrus"
)

func CreateDevice(device *model.Device) error {
	return query.Device.Create(device)
}

// 批量创建设备
func CreateDeviceBatch(devices []*model.Device) error {
	return query.Device.Create(devices...)
}

func CreateDeviceBath(devices []*model.Device) error {
	return query.Device.Create(devices...)
}

func UpdateDevice(device *model.Device) (*model.Device, error) {
	info, err := query.Device.Where(query.Device.ID.Eq(device.ID)).Updates(device)
	if err != nil {
		logrus.Error(err)
		return nil, err
	} else if info.RowsAffected == 0 {
		return nil, fmt.Errorf("update device failed, no rows affected")
	}
	return device, err
}
func UpdateDeviceByMap(deviceID string, deviceMap map[string]interface{}) (*model.Device, error) {
	info, err := query.Device.Where(query.Device.ID.Eq(deviceID)).Updates(deviceMap)
	if err != nil {
		logrus.Error(err)
		return nil, err
	} else if info.RowsAffected == 0 {
		return nil, fmt.Errorf("update device failed, no rows affected")
	}
	// 查询更新后的数据
	device, err := query.Device.Where(query.Device.ID.Eq(deviceID)).First()
	if err != nil {
		logrus.Error(err)
	}
	return device, err
}

// 更新设备状态
func UpdateDeviceStatus(deviceId string, status int16) error {
	info, err := query.Device.Where(query.Device.ID.Eq(deviceId)).Update(query.Device.IsOnline, status)
	if err != nil {
		logrus.Error(err)
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("update device status failed, no rows affected")
	}
	return err
}

func DeleteDevice(id string, tenantID string) error {
	_, err := query.Device.Where(query.Device.ID.Eq(id), query.Device.TenantID.Eq(tenantID)).Delete()
	if err != nil {
		logrus.Error(err)
	}
	return err
}

// 根据子设备id获取父设备信息
func GetParentDeviceBySubDeviceID(subDeviceID string) (info *model.Device, err error) {
	var device = query.Device
	info, err = device.Where(device.ID.Eq(subDeviceID)).First()
	if err != nil {
		logrus.Error(err)
	}
	return
}

func GetDeviceByID(id string) (*model.Device, error) {
	device, err := query.Device.Where(query.Device.ID.Eq(id)).First()
	if err != nil {
		logrus.Error(err)
	} else if device == nil {
		return nil, fmt.Errorf("device not found")
	}
	return device, err
}

// 获取设备详情，关联设备配置
func GetDeviceDetail(id string) (map[string]interface{}, error) {
	var device = query.Device
	var deviceConfig = query.DeviceConfig
	var data = make(map[string]interface{})
	err := device.LeftJoin(deviceConfig, deviceConfig.ID.EqCol(device.DeviceConfigID)).
		Where(device.ID.Eq(id)).
		Select(device.ALL, deviceConfig.Name.As("device_config_name")).Scan(&data)
	if err != nil {
		logrus.Error(err)
	}
	if data["parent_id"] != nil {
		parentDevice, err := GetDeviceByID(data["parent_id"].(string))
		if err != nil {
			logrus.Error(err)
		}
		data["gateway_device_name"] = parentDevice.Name
	}
	return data, err
}

// 通过凭证获取设备信息
func GetDeviceByVoucher(voucher string) (*model.Device, error) {
	device, err := query.Device.Where(query.Device.Voucher.Eq(voucher)).First()
	if err != nil {
		logrus.Error(err)
	}
	return device, err
}

// 更新设备在线状态
func UpdateDeviceOnlineStatus(deviceId string, status int16) error {
	_, err := query.Device.Where(query.Device.ID.Eq(deviceId)).Update(query.Device.IsOnline, status)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

// 通过设备编号获取设备信息
func GetDeviceByDeviceNumber(deviceNumber string) (*model.Device, error) {
	device, err := query.Device.Where(query.Device.DeviceNumber.Eq(deviceNumber)).First()
	if err != nil {
		logrus.Error(err)
	}
	return device, err
}

func GetDeviceBySubDeviceAddress(deviceAddress []string, parentId string) (map[string]*model.Device, error) {
	devices, err := query.Device.Where(query.Device.SubDeviceAddr.In(deviceAddress...)).
		Where(query.Device.ParentID.Eq(parentId)).Find()
	if err != nil {
		return nil, err
	}
	var result = make(map[string]*model.Device)
	for _, d := range devices {
		result[*d.SubDeviceAddr] = d
	}
	return result, err
}

// 移除子设备：将设备的parent_id置为空
func RemoveSubDevice(deviceId string, tenant_id string) error {
	info, err := query.Device.Where(query.Device.ID.Eq(deviceId), query.Device.TenantID.Eq(tenant_id)).UpdateSimple(query.Device.ParentID.Null(), query.Device.SubDeviceAddr.Null())
	if err != nil {
		logrus.Error(err)
	} else if info.RowsAffected == 0 {
		return fmt.Errorf("remove sub device failed, device not found")
	}
	return err
}

func GetDeviceListByPage(req *model.GetDeviceListByPageReq, tenant_id string) (int64, []model.GetDeviceListByPageRsp, error) {
	q := query.Device
	c := query.DeviceConfig
	var count int64
	var deviceList = []model.GetDeviceListByPageRsp{}
	queryBuilder := q.WithContext(context.Background())

	if req.GroupId != nil && *req.GroupId != "" {
		// 查询所有的组id
		groupIds, err := GetGroupChildrenIds(*req.GroupId)
		if err != nil {
			logrus.Error(err)
			return count, deviceList, err
		}
		ids, err2 := GetDeviceIdsByGroupIds(groupIds)
		if err2 != nil {
			logrus.Error(err2)
			return count, deviceList, err2
		}
		ids = append(ids, *req.GroupId)
		queryBuilder = queryBuilder.Where(q.ID.In(ids...))
	}
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenant_id))

	queryBuilder = queryBuilder.Where(q.ActivateFlag.Eq("active"))

	if req.IsEnabled != nil && *req.IsEnabled != "" {
		queryBuilder = queryBuilder.Where(q.IsEnabled.Eq(*req.IsEnabled))
	}

	if req.ProductID != nil && *req.ProductID != "" {
		queryBuilder = queryBuilder.Where(q.ProductID.Eq(*req.ProductID))
	}

	if req.ServiceIdentifier != nil && *req.ServiceIdentifier != "" {
		if *req.ServiceIdentifier == "mqtt" {
			queryBuilder = queryBuilder.Where(query.Device.Where(c.ProtocolType.Eq(*req.ServiceIdentifier)).Or(q.DeviceConfigID.IsNull()))
		} else {
			queryBuilder = queryBuilder.Where(c.ProtocolType.Eq(*req.ServiceIdentifier))
		}
	}
	if req.ServiceAccessID != nil && *req.ServiceAccessID != "" {
		queryBuilder = queryBuilder.Where(q.ServiceAccessID.Eq(*req.ServiceAccessID))
	}
	if req.DeviceType != nil && *req.DeviceType != "" {
		if *req.DeviceType == "1" {
			queryBuilder = queryBuilder.Where(query.Device.Where(q.DeviceConfigID.IsNull()).Or(c.DeviceType.Eq(*req.DeviceType)))
		} else {
			queryBuilder = queryBuilder.Where(c.DeviceType.Eq(*req.DeviceType))
		}
	}

	if req.AccessWay != nil && *req.AccessWay != "" {
		queryBuilder = queryBuilder.Where(q.AccessWay.Eq(*req.AccessWay))
	}

	// 模糊
	if req.Label != nil && *req.Label != "" {
		queryBuilder = queryBuilder.Where(q.Label.Like(fmt.Sprintf("%%%s%%", *req.Label)))
	}

	if req.Search != nil && *req.Search != "" {
		queryBuilder = queryBuilder.
			Where(q.Name.Like(fmt.Sprintf("%%%s%%", *req.Search))).
			Or(q.DeviceNumber.Like(fmt.Sprintf("%%%s%%", *req.Search)))
	}

	// 模糊
	if req.Name != nil && *req.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *req.Name)))
	}

	if req.CurrentVersion != nil && *req.CurrentVersion != "" {
		queryBuilder = queryBuilder.Where(q.CurrentVersion.Eq(*req.CurrentVersion))
	}

	if req.BatchNumber != nil && *req.BatchNumber != "" {
		queryBuilder = queryBuilder.Where(q.BatchNumber.Like(fmt.Sprintf("%%%s%%", *req.BatchNumber)))
	}

	if req.DeviceNumber != nil && *req.DeviceNumber != "" {
		queryBuilder = queryBuilder.Where(q.DeviceNumber.Like(fmt.Sprintf("%%%s%%", *req.DeviceNumber)))
	}
	if req.DeviceConfigId != nil && *req.DeviceConfigId != "" {
		queryBuilder = queryBuilder.Where(q.DeviceConfigID.Eq(*req.DeviceConfigId))
	}

	if req.IsOnline != nil {
		if *req.IsOnline == int(1) {
			queryBuilder = queryBuilder.Where(q.IsOnline.Eq(1))
		} else if *req.IsOnline == int(0) {
			queryBuilder = queryBuilder.Where(q.IsOnline.Neq(1))
		} else {
			return count, deviceList, fmt.Errorf("is_online param error")
		}
	}
	queryBuilder = queryBuilder.LeftJoin(c, c.ID.EqCol(q.DeviceConfigID))
	//count查询
	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, deviceList, err
	}

	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}

	t := query.TelemetryCurrentData
	t2 := query.TelemetryCurrentData.As("t2")
	//q.ID, q.DeviceNumber, q.Name, q.DeviceConfigID, q.ActivateFlag, q.ActivateAt, q.BatchNumber
	err = queryBuilder.Select(q.ID, q.DeviceNumber, q.Name, q.DeviceConfigID, q.ActivateFlag, q.ActivateAt, q.BatchNumber, q.Location, q.CurrentVersion, q.CreatedAt, q.IsOnline, q.AccessWay, c.ProtocolType, c.Name.As("DeviceConfigName"), t2.T).
		LeftJoin(t.Select(t.T.Max().As("ts"), t.DeviceID).Group(t.DeviceID).As("t2"), t2.DeviceID.EqCol(q.ID)).
		Order(q.CreatedAt.Desc()).
		Scan(&deviceList)
	if err != nil {
		logrus.Error(err)
		return count, deviceList, err
	}
	return count, deviceList, err
}

func GetDevicePreRegisterListByPage(req *model.GetDevicePreRegisterListByPageReq, tenant_id string) (int64, []model.GetDevicePreRegisterListByPageRsp, error) {
	q := query.Device
	var count int64
	var deviceList = []model.GetDevicePreRegisterListByPageRsp{}
	queryBuilder := q.WithContext(context.Background())

	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenant_id))

	if req.ActivateFlag != nil && *req.ActivateFlag != "" {
		queryBuilder = queryBuilder.Where(q.ActivateFlag.Eq(*req.ActivateFlag))
	}

	if req.IsEnabled != nil && *req.IsEnabled != "" {
		queryBuilder = queryBuilder.Where(q.IsEnabled.Eq(*req.IsEnabled))
	}

	if req.ProductID != "" {
		queryBuilder = queryBuilder.Where(q.ProductID.Eq(req.ProductID))
	}

	if req.DeviceConfigID != nil && *req.DeviceConfigID != "" {
		queryBuilder = queryBuilder.Where(q.DeviceConfigID.Eq(*req.DeviceConfigID))
	}

	if req.BatchNumber != nil && *req.BatchNumber != "" {
		queryBuilder = queryBuilder.Where(q.BatchNumber.Like(fmt.Sprintf("%%%s%%", *req.BatchNumber)))
	}

	if req.DeviceNumber != nil && *req.DeviceNumber != "" {
		queryBuilder = queryBuilder.Where(q.DeviceNumber.Like(fmt.Sprintf("%%%s%%", *req.DeviceNumber)))
	}

	if req.Name != nil && *req.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *req.Name)))
	}

	//count查询
	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, deviceList, err
	}

	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}

	err = queryBuilder.Select(
		q.ID, q.Name, q.DeviceNumber, q.ActivateFlag, q.ActivateAt, q.BatchNumber, q.CurrentVersion).
		Order(q.CreatedAt.Desc()).
		Scan(&deviceList)
	if err != nil {
		logrus.Error(err)
		return count, deviceList, err
	}
	return count, deviceList, err
}

// 通过设备id获取设备信息
func GetDeviceById(deviceId string) (*model.Device, error) {
	device, err := query.Device.Where(query.Device.ID.Eq(deviceId)).First()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return device, nil
}

type DeviceQuery struct {
}

func (d DeviceQuery) Count(ctx context.Context) (count int64, err error) {
	count, err = query.Device.Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (d DeviceQuery) CountByTenantID(ctx context.Context, TenantID string) (count int64, err error) {
	var device = query.Device
	count, err = device.Where(device.TenantID.Eq(TenantID)).Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

// 获取网关未关联网关设备的子设备列表,并做关联查询设备配置表
func (d DeviceQuery) GetGatewayUnrelatedDeviceList(ctx context.Context, tenantId string) (list []map[string]interface{}, err error) {
	var device = query.Device
	var deviceConfig = query.DeviceConfig
	// 条件：device-父设备为空，设备配置不为空
	// 条件：device_config_id-设备类型为3-子设备
	err = device.
		WithContext(ctx).
		Select(device.ID, device.Name, device.DeviceConfigID.As("device_config_id"), deviceConfig.Name.As("device_config_name")).
		Where(device.TenantID.Eq(tenantId)).
		Where(device.DeviceConfigID.IsNotNull()).
		Where(device.ParentID.IsNull()). // 父设备为空
		LeftJoin(deviceConfig, deviceConfig.ID.EqCol(device.DeviceConfigID)).
		Where(deviceConfig.DeviceType.Eq("3")). // 设备类型为网关
		Scan(&list)
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (d DeviceQuery) CountByWhere(ctx context.Context, option ...gen.Condition) (count int64, err error) {
	var device = query.Device
	count, err = device.Where(option...).Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (d DeviceQuery) First(ctx context.Context, option ...gen.Condition) (info *model.Device, err error) {
	info, err = query.Device.WithContext(ctx).Where(option...).First()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (d DeviceQuery) Find(ctx context.Context, option ...gen.Condition) (list []*model.Device, err error) {
	list, err = query.Device.WithContext(ctx).Where(option...).Find()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

// 获取设备下拉列表
// 返回设备id、设备名称、设备配置id、设备配置名称
func (d DeviceQuery) GetDeviceSelect(tenantId string, deviceName string, bindConfig int) (list []map[string]interface{}, err error) {

	var device = query.Device
	var deviceConfig = query.DeviceConfig
	query := device.
		WithContext(context.Background()).
		Select(device.ID, device.Name, device.DeviceConfigID.As("device_config_id"), deviceConfig.Name.As("device_config_name")).
		Where(device.TenantID.Eq(tenantId)).
		Where(device.ActivateFlag.Eq("active")). // 激活状态
		Where(device.Name.Like(fmt.Sprintf("%%%s%%", deviceName))).
		LeftJoin(deviceConfig, deviceConfig.ID.EqCol(device.DeviceConfigID)).Order(device.CreatedAt.Desc())
	switch bindConfig {
	case 1:
		query = query.Where(device.DeviceConfigID.IsNotNull())
	case 2:
		query = query.Where(device.DeviceConfigID.IsNull())
	}
	err = query.Scan(&list)
	if err != nil {
		logrus.Error(err)
	}
	return
}

// 更新指定字段
func (d DeviceQuery) Update(ctx context.Context, info *model.Device, option ...field.Expr) error {
	device := query.Device
	_, err := query.Device.WithContext(ctx).Where(device.ID.Eq(info.ID)).Select(option...).UpdateColumns(info)
	if err != nil {
		logrus.Error(ctx, err)
	}
	return err
}

// 更新设备配置
func (d DeviceQuery) ChangeDeviceConfig(deviceID string, deviceConfigID *string) error {
	device := query.Device
	info, err := device.Where(device.ID.Eq(deviceID)).Update(device.DeviceConfigID, deviceConfigID)
	if err != nil {
		logrus.Error(err)
		return err
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("update device config failed, no rows affected")
	}
	return err
}

func (d DeviceQuery) GetSubList(ctx context.Context, parent_id string, pageSize, page int64, tenantID string) ([]model.GetSubListResp, int64, error) {

	var (
		q     = query.Device
		count int64
		resp  []model.GetSubListResp
	)
	query := q.WithContext(ctx).Where(q.ParentID.Eq(parent_id), q.TenantID.Eq(tenantID))
	count, err := query.Count()
	if err != nil {
		return resp, count, err
	}
	err = query.Offset(int(page-1) * int(pageSize)).Limit(int(pageSize)).Order(q.CreatedAt.Desc()).Scan(&resp)
	if err != nil {
		return resp, count, err
	}
	return resp, count, nil
}

// 获取子设备列表
func GetSubDeviceListByParentID(parentId string) ([]*model.Device, error) {
	var device = query.Device
	list, err := device.Where(device.ParentID.Eq(parentId)).Find()
	if err != nil {
		logrus.Error(err)
	}
	return list, err
}

func GetDeviceTemplateChartSelect(tenantId string) (any, error) {
	var data = []map[string]interface{}{}
	var d = query.Device
	var dc = query.DeviceConfig
	var dm = query.DeviceTemplate
	err := d.LeftJoin(dc, dc.ID.EqCol(d.DeviceConfigID)).
		LeftJoin(dm, dm.ID.EqCol(dc.DeviceTemplateID)).
		Where(d.TenantID.Eq(tenantId)).
		Where(d.ActivateFlag.Eq("active")).
		Where(d.DeviceConfigID.IsNotNull()).
		Where(dc.DeviceTemplateID.IsNotNull()).
		Where(dm.WebChartConfig.IsNotNull()).
		Select(d.ID.As("device_id"), d.Name.As("device_name"), dm.WebChartConfig).Scan(&data)
	if err != nil {
		logrus.Error(err)
	}
	return data, nil
}

func GetDeviceCurrentStatus(deviceId string) (string, error) {
	data, err := query.Device.Where(query.Device.ID.Eq(deviceId)).First()
	var result string = "OFF-LINE"
	if err != nil {
		return result, err
	} else if err == gorm.ErrRecordNotFound {
		return result, nil
	}
	if data.IsOnline == 1 {
		result = "ON-LINE"
	}
	return result, nil
}

func GetDeviceTemplateIdByDeviceId(deviceId string) (string, error) {
	var result model.DeviceConfig
	query.Device.LeftJoin(query.DeviceConfig, query.Device.DeviceConfigID.EqCol(query.DeviceConfig.ID)).
		Where(query.Device.ID.Eq(deviceId)).Select(query.DeviceConfig.DeviceTemplateID).Scan(&result)
	if result.DeviceTemplateID != nil {
		return *result.DeviceTemplateID, nil
	}
	return "", nil
}

// 通过设备配置id获取设备列表
func GetDevicesByDeviceConfigID(deviceConfigID string) ([]*model.Device, error) {
	var device = query.Device
	list, err := device.Where(device.DeviceConfigID.Eq(deviceConfigID)).Find()
	if err != nil {
		logrus.Error(err)
	}
	return list, err
}

// 通过子设备配置id获取已绑定网关的子设备列表
// func GetDevicesBySubDeviceConfigID(deviceConfigID string) ([]*model.Device, error) {
// 	var device = query.Device
// 	list, err := device.Where(device.DeviceConfigID.Eq(deviceConfigID), device.ParentID.IsNotNull()).Find()
// 	if err != nil {
// 		logrus.Error(err)
// 	}
// 	return list, err
// }

// 通过子设配置ID查询所有关联这个配置的子设备的网关设备列表
func GetGatewayDevicesBySubDeviceConfigID(deviceConfigID string) ([]string, error) {
	var device = query.Device
	var deviceIDList []string
	err := device.Where(device.DeviceConfigID.Eq(deviceConfigID), device.ParentID.IsNotNull()).Select(device.ParentID.Distinct()).Scan(&deviceIDList)
	if err != nil {
		logrus.Error(err)
	}
	return deviceIDList, err
}

// 查询服务接入点关联的设备
func GetServiceDeviceList(serviceAccessId string) ([]model.Device, error) {
	var devices []model.Device
	err := query.Device.Where(query.Device.ServiceAccessID.Eq(serviceAccessId)).Scan(&devices)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return devices, err
}
