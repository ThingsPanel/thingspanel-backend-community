package dal

import (
	"context"
	model "project/internal/model"
	"project/query"

	"github.com/sirupsen/logrus"
)

func GetSceneAutomationLog(req *model.GetSceneAutomationLogReq, tenantId string) (int64, []*model.SceneAutomationLog, error) {
	var count int64
	q := query.SceneAutomationLog
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.SceneAutomationID.Eq(req.SceneAutomationId))
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantId))

	if req.ExecutionResult != nil {
		queryBuilder = queryBuilder.Where(q.ExecutionResult.Eq(*req.ExecutionResult))
	}

	if req.ExecutionStartTime != nil && req.ExecutionEndTime != nil {
		queryBuilder = queryBuilder.Where(q.ExecutedAt.Between(*req.ExecutionStartTime, *req.ExecutionEndTime))
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

	logList, err := queryBuilder.Order(q.ExecutedAt.Desc()).Find()
	if err != nil {
		return count, logList, err
	}
	return count, logList, err

}

func SceneAutomationLogInsert(data *model.SceneAutomationLog) error {
	err := query.SceneAutomationLog.Create(data)
	if err != nil {
		return err
	}
	return err
}
