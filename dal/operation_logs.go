package dal

import (
	"context"
	"fmt"
	"time"

	model "project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
)

func CreateOperationLogs(OperationLog *model.OperationLog) error {
	return query.OperationLog.Create(OperationLog)
}

func GetListByPage(operationLog *model.GetOperationLogListByPageReq) (int64, interface{}, error) {
	q := query.OperationLog
	var count int64
	var operationLogList []model.GetOperationLogListByPageRsp
	queryBuilder := q.WithContext(context.Background()).Where(q.TenantID.Eq(operationLog.TenantID))

	if operationLog.IP != nil && *operationLog.IP != "" {
		queryBuilder = queryBuilder.Where(q.IP.Like(fmt.Sprintf("%%%s%%", *operationLog.IP)))
	}

	if operationLog.StartTime != nil && operationLog.EndTime != nil {
		queryBuilder = queryBuilder.Where(q.CreatedAt.Between(*operationLog.StartTime, *operationLog.EndTime))
	}

	u := query.User
	queryBuilder = queryBuilder.LeftJoin(u, u.ID.EqCol(q.UserID))
	if operationLog.UserName != nil && *operationLog.UserName != "" {
		queryBuilder = queryBuilder.Where(u.Name.Like(fmt.Sprintf("%%%s%%", *operationLog.UserName)))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, operationLogList, err
	}

	if operationLog.Page != 0 && operationLog.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(operationLog.PageSize)
		queryBuilder = queryBuilder.Offset((operationLog.Page - 1) * operationLog.PageSize)
	}

	err = queryBuilder.Select(q.ALL, u.Name.As("user_name")).
		Order(q.CreatedAt.Desc()).
		Scan(&operationLogList)
	if err != nil {
		logrus.Error(err)
		return count, operationLogList, err
	}

	return count, operationLogList, err
}

func DeleteOperationLogsByTime(t time.Time) error {
	_, err := query.OperationLog.Where(query.OperationLog.CreatedAt.Lte(t)).Delete()
	return err
}
