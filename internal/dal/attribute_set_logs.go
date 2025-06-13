package dal

import (
	"context"
	"encoding/json"
	"project/pkg/constant"
	"strconv"

	model "project/internal/model"
	query "project/internal/query"

	"github.com/sirupsen/logrus"
)

func GetAttributeSetLogsDataListByPage(req model.GetAttributeSetLogsListByPageReq) (int64, []*model.AttributeSetLog, error) {

	var count int64
	q := query.AttributeSetLog
	u := query.User
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(req.DeviceId))
	if req.Status != nil {
		queryBuilder = queryBuilder.Where(q.Status.Eq(*req.Status))
	}
	if req.OperationType != nil {
		queryBuilder = queryBuilder.Where(q.OperationType.Eq(*req.OperationType))
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
	queryBuilder = queryBuilder.Order(q.CreatedAt.Desc())
	queryBuilder = queryBuilder.LeftJoin(u, u.ID.EqCol(q.UserID))
	list, err := queryBuilder.Select(q.ALL, u.Name.As("username")).Find()
	if err != nil {
		logrus.Error(err)
		return count, list, err
	}

	return count, list, nil

}

type AttributeSetLogsQuery struct {
}

func (AttributeSetLogsQuery) Create(ctx context.Context, info *model.AttributeSetLog) (id string, err error) {
	attribute := query.AttributeSetLog

	err = attribute.WithContext(ctx).Create(info)
	if err != nil {
		logrus.Error("[AttributeSetLogsQuery]create failed:", err)
	}
	return info.ID, err
}

func (AttributeSetLogsQuery) SetAttributeResultUpdate(ctx context.Context, logId string, response model.MqttResponse) {
	attribute := query.AttributeSetLog
	valueByte, _ := json.Marshal(response)
	values := string(valueByte)
	updates := model.AttributeSetLog{
		RspDatum: &values,
	}
	if response.Result == 0 {
		status := strconv.Itoa(constant.ResponseStatusOk)
		updates.Status = &status
	} else {
		status := strconv.Itoa(constant.ResponseSStatusFailed)
		updates.Status = &status
		updates.ErrorMessage = &response.Message
	}
	//updates["rsp_data"] = string(values)
	_, err := attribute.WithContext(ctx).Where(attribute.ID.Eq(logId)).Updates(updates)
	if err != nil {
		logrus.Error("[CommandSetLogsQuery]create failed:", err)
	}

}

// 根据key查询设备属性
func GetAttributeDataByKey(req model.GetDataListByKeyReq) (*model.AttributeData, error) {
	data, err := query.AttributeData.WithContext(context.Background()).Where(query.AttributeData.DeviceID.Eq(req.DeviceId), query.AttributeData.Key.Eq(req.Key)).First()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return data, nil
}

// 删除属性下发历史数据，带事务
func DeleteAttributeDataByDeviceIdTx(deviceId string, tx *query.QueryTx) error {
	_, err := tx.AttributeData.WithContext(context.Background()).Where(query.AttributeData.DeviceID.Eq(deviceId)).Delete()
	return err
}
