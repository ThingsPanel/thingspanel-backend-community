package dal

import (
	"context"

	model "project/internal/model"
	query "project/query"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func GetEventDatasListByPage(req *model.GetEventDatasListByPageReq) (int64, []map[string]interface{}, error) {

	var count int64
	q := query.EventData
	d := query.Device
	dc := query.DeviceConfig
	dme := query.DeviceModelEvent

	queryBuilder := q.WithContext(context.Background())

	queryBuilder = queryBuilder.LeftJoin(d, q.DeviceID.EqCol(d.ID)).
		LeftJoin(dc, d.DeviceConfigID.EqCol(dc.ID)).
		LeftJoin(dme, dc.DeviceTemplateID.EqCol(dme.DeviceTemplateID), dme.DataIdentifier.EqCol(q.Identify)).
		Where(q.DeviceID.Eq(req.DeviceId))

	if req.Identify != nil && *req.Identify != "" {
		queryBuilder = queryBuilder.Where(q.Identify.Eq(*req.Identify))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}
	queryBuilder = queryBuilder.Order(q.T.Desc())
	var list []map[string]interface{}
	err = queryBuilder.Select(q.ALL, dme.DataName).Scan(&list)
	if err != nil {
		logrus.Error(err)
		return count, list, err
	}

	return count, list, nil

}

// CreateEventData 创建事件数据
func CreateEventData(data *model.EventData) error {
	return query.EventData.Create(data)
}

func GetDeviceEventOneKeys(deviceId string, keys string) (string, error) {
	data, err := query.EventData.Where(query.EventData.DeviceID.Eq(deviceId), query.EventData.Identify.Eq(keys)).Order(query.EventData.T.Desc()).First()
	var result string
	if err != nil {
		return result, err
	} else if err == gorm.ErrRecordNotFound {
		return result, nil
	}

	if data.Datum != nil {
		result = *data.Datum
	}
	return result, nil
}
