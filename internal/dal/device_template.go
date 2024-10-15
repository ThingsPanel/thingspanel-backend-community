package dal

import (
	"context"
	"fmt"

	model "project/internal/model"
	query "project/internal/query"
	utils "project/pkg/utils"

	"github.com/sirupsen/logrus"
)

const (
	DEVICE_TEMPLATE_PRIVATE = int16(1)
	DEVICE_TEMPLATE_PUBLIC  = int16(2)
)

func CreateDeviceTemplate(device *model.DeviceTemplate) (*model.DeviceTemplate, error) {

	return device, query.DeviceTemplate.Create(device)
}

func GetDeviceTemplateById(id string) (*model.DeviceTemplate, error) {
	template, err := query.DeviceTemplate.Where(query.DeviceTemplate.ID.Eq(id)).First()
	if err != nil {
		return template, err
	}
	if template == nil {
		return nil, fmt.Errorf("device template not found: id=%s", id)
	}
	return template, err
}

// GetDeviceTemplateByDeviceId 根据设备ID获取模板
func GetDeviceTemplateByDeviceId(deviceId string) (any, error) {
	var d = query.Device
	var t = query.DeviceTemplate
	var dc = query.DeviceConfig
	var rsp map[string]interface{}
	err := d.LeftJoin(dc, dc.ID.EqCol(d.DeviceConfigID)).LeftJoin(t, t.ID.EqCol(dc.DeviceTemplateID)).Where(d.ID.Eq(deviceId)).Select(t.ALL).Scan(&rsp)
	if err != nil {
		return nil, err
	}
	// 判断rsp是否有key为id
	if v, ok := rsp["id"]; ok {
		if v == nil {
			//返回{}，而不是nil
			return map[string]interface{}{}, nil
		}
	}
	return rsp, err
}

func UpdateDeviceTemplate(data *model.DeviceTemplate) (*model.DeviceTemplate, error) {
	info, err := query.DeviceTemplate.Where(query.DeviceTemplate.ID.Eq(data.ID)).Updates(data)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	if info.RowsAffected == 0 {
		return nil, fmt.Errorf("update device template failed, no rows affected")
	}
	return data, err
}

func DeleteDeviceTemplate(id string) error {
	_, err := query.DeviceTemplate.Where(query.DeviceTemplate.ID.Eq(id)).Delete()
	return err
}

func GetDeviceTemplateListByPage(req *model.GetDeviceTemplateListByPageReq, claims *utils.UserClaims) (int64, interface{}, error) {

	if req.Page <= 0 || req.PageSize <= 0 {
		return 0, nil, fmt.Errorf("page and pageSize must be greater than 0")
	}

	q := query.DeviceTemplate
	var count int64
	queryBuilder := q.WithContext(context.Background())
	if req.Name != nil {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *req.Name)))
	}
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(claims.TenantID))
	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	// toal_pages向上取整
	// total_pages := int64(math.Ceil(float64(count) / float64(req.PageSize)))

	queryBuilder = queryBuilder.Limit(req.PageSize)
	queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	queryBuilder = queryBuilder.Order(q.CreatedAt.Desc())
	datalist, err := queryBuilder.Find()
	if err != nil {
		logrus.Error("queryBuilder.Find error: ", err)
	}
	return count, datalist, err

}

func GetDeviceTemplateMenu(req *model.GetDeviceTemplateMenuReq, claims *utils.UserClaims) (interface{}, error) {

	q := query.DeviceTemplate
	queryBuilder := q.WithContext(context.Background())
	if req.Name != nil {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *req.Name)))
	}
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(claims.TenantID))
	var data []map[string]interface{}
	err := queryBuilder.Select(q.ID, q.Name).Scan(&data)
	if err != nil {
		logrus.Error("queryBuilder.Find error: ", err)
	}
	return data, err

}
