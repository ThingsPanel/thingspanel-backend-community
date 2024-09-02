package dal

import (
	"context"
	model "project/internal/model"
	query "project/query"

	"github.com/sirupsen/logrus"
)

func BatchCreateRGroupDevice(r []*model.RGroupDevice) error {
	return query.RGroupDevice.CreateInBatches(r, len(r))
}

func DeleteRGroupDevice(group_id, device_id string) error {
	_, err := query.RGroupDevice.
		Where(query.RGroupDevice.GroupID.Eq(group_id)).
		Where(query.RGroupDevice.DeviceID.Eq(device_id)).
		Delete()
	return err
}

func GetRGroupDeviceByGroupId(req model.GetDeviceListByGroup) (int64, interface{}, error) {
	// 获取分组下设备,分页返回
	q := query.RGroupDevice
	var devicesList []model.GetDeviceListByGroupRsp
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.GroupID.Eq(req.GroupId))
	var count int64
	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, devicesList, err
	}

	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}
	d := query.Device
	c := query.DeviceConfig
	err = queryBuilder.Select(q.GroupID, d.ID, d.DeviceNumber, d.Name, c.Name.As("device_configs_name")).
		LeftJoin(d, d.ID.EqCol(q.DeviceID)).
		LeftJoin(c, c.ID.EqCol(d.DeviceConfigID)).
		Where(d.ActivateFlag.Eq("active")).
		Order(d.CreatedAt.Desc()).
		Scan(&devicesList)
	if err != nil {
		logrus.Error(err)
		return count, devicesList, err
	}

	return count, devicesList, err
}

// 获取分组下设备下拉菜单
// 返回设备id、设备名称、设备配置id、设备配置名称
func GetDeviceSelectByGroupId(tenantId string, group_id string, deviceName string, bindConfig int) ([]map[string]interface{}, error) {
	var data []map[string]interface{}
	rgd := query.RGroupDevice
	d := query.Device
	dc := query.DeviceConfig
	query := rgd.
		Select(rgd.DeviceID.As("id"), d.Name, d.DeviceConfigID, dc.Name.As("device_config_name")).
		Join(d, d.ID.EqCol(rgd.DeviceID)).
		Join(dc, d.DeviceConfigID.EqCol(dc.ID)).
		Where(rgd.GroupID.Eq(group_id)).
		Where(d.TenantID.Eq(tenantId)).
		Where(d.ActivateFlag.Eq("active")). // 激活状态
		Where(d.Name.Like("%" + deviceName + "%")).Order(d.CreatedAt.Desc())
	switch bindConfig {
	case 1:
		query = query.Where(d.DeviceConfigID.IsNotNull())
	case 2:
		query = query.Where(d.DeviceConfigID.IsNull())
	}
	return data, query.Scan(&data)
}

func GetRGroupDeviceByDeviceId(device_id string) ([]*model.RGroupDevice, error) {
	data, err := query.RGroupDevice.Where(query.RGroupDevice.DeviceID.Eq(device_id)).Find()
	return data, err
}

func GetDeviceIdsByGroupIds(group_ids []string) ([]string, error) {
	data, err := query.RGroupDevice.Where(query.RGroupDevice.GroupID.In(group_ids...)).Select(query.RGroupDevice.DeviceID).Find()
	var deviceIds []string
	for i := range data {
		deviceIds = append(deviceIds, data[i].DeviceID)
	}
	return deviceIds, err
}

func GetDeviceIdsByDeviceConfigId(deviceConfigIds []string) ([]string, error) {
	var result []string
	err := query.Device.Where(query.Device.DeviceConfigID.In(deviceConfigIds...)).Pluck(query.Device.ID, &result)

	return result, err
}
