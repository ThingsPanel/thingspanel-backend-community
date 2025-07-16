package dal

import (
	"context"
	"encoding/json"
	"fmt"
	"project/pkg/constant"
	"strconv"
	"time"

	model "project/internal/model"
	query "project/internal/query"

	"github.com/sirupsen/logrus"
)

func GetCommandSetLogsDataListByPage(req model.GetCommandSetLogsListByPageReq) (int64, any, error) {

	var count int64
	q := query.CommandSetLog
	d := query.Device
	dc := query.DeviceConfig
	dmc := query.DeviceModelCommand
	u := query.User

	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.LeftJoin(d, d.ID.EqCol(q.DeviceID))
	queryBuilder = queryBuilder.LeftJoin(dc, dc.ID.EqCol(d.DeviceConfigID))
	queryBuilder = queryBuilder.LeftJoin(dmc, dmc.DeviceTemplateID.EqCol(dc.DeviceTemplateID), dmc.DataIdentifier.EqCol(q.Identify))
	queryBuilder = queryBuilder.LeftJoin(u, u.ID.EqCol(q.UserID))

	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(req.DeviceId))

	if req.Status != nil {
		queryBuilder = queryBuilder.Where(q.Status.Eq(*req.Status))
	}
	if req.OperationType != nil {
		queryBuilder = queryBuilder.Where(q.OperationType.Eq(*req.OperationType))
	}
	if req.IdentifyName != nil {
		queryBuilder = queryBuilder.Where(dmc.DataName.Like("%" + *req.IdentifyName + "%"))
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
	var list []map[string]interface{}

	err = queryBuilder.Select(q.ALL, dmc.DataName.As("identify_name"), u.Name.As("username")).Scan(&list)
	if err != nil {
		logrus.Error(err)
		return count, list, err
	}

	return count, list, nil

}

type CommandSetLogsQuery struct {
}

func (CommandSetLogsQuery) Create(ctx context.Context, info *model.CommandSetLog) (id string, err error) {
	command := query.CommandSetLog

	err = command.WithContext(ctx).Create(info)
	if err != nil {
		logrus.Error("[CommandSetLogsQuery]create failed:", err)
	}
	return info.ID, err
}

func (CommandSetLogsQuery) CommandResultUpdate(ctx context.Context, logId string, response model.MqttResponse) {
	command := query.CommandSetLog
	valueByte, _ := json.Marshal(response)
	values := string(valueByte)
	updates := model.CommandSetLog{
		RspDatum: &values,
	}
	if response.Result == 0 {
		status := strconv.Itoa(constant.ResponseStatusOk)
		updates.Status = &status
		updates.ErrorMessage = &response.Message
		//updates["status"] = constant.CommandStatusOk
	} else {
		//updates["status"] = constant.CommandStatusFailed
		//updates["error_message"] = response.Message
		status := strconv.Itoa(constant.ResponseSStatusFailed)
		updates.Status = &status
		updates.ErrorMessage = &response.Message
	}
	//updates["rsp_data"] = string(values)
	_, err := command.WithContext(ctx).Where(command.ID.Eq(logId)).Updates(updates)
	if err != nil {
		logrus.Error("[CommandSetLogsQuery]create failed:", err)
	}

}

func (CommandSetLogsQuery) Update(ctx context.Context, info *model.CommandSetLog) error {
	command := query.CommandSetLog

	result, err := command.WithContext(ctx).Where(command.MessageID.Eq(*info.MessageID)).Updates(info)
	if err != nil {
		logrus.Error("[CommandSetLogsQuery]update failed:", err)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("no data updated")
	}
	return err
}

func (CommandSetLogsQuery) FilterOneHourByMessageID(messageId string) (*model.CommandSetLog, error) {
	command := query.CommandSetLog
	nowTime := time.Now().UTC()

	log, err := command.Where(command.MessageID.Eq(messageId)).
		Where(command.CreatedAt.Gte(nowTime.Add(-time.Hour))).
		Select().
		First()
	if err != nil {
		logrus.Error("[CommandSetLogsQuery]FilterByMessageID failed:", err)
	}
	return log, err

}

// 删除命令历史数据，带事务
func DeleteCommandSetLogsByDeviceId(deviceId string, tx *query.QueryTx) error {
	_, err := tx.CommandSetLog.Where(query.CommandSetLog.DeviceID.Eq(deviceId)).Delete()
	return err
}
