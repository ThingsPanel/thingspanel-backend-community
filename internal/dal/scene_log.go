package dal

import (
	"context"
	model "project/internal/model"
	query "project/internal/query"

	"github.com/sirupsen/logrus"
)

func GetSceneLogByPage(req model.GetSceneLogListByPageReq) (int64, []*model.SceneLog, error) {

	var count int64
	q := query.SceneLog
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.SceneID.Eq(req.ID))

	if req.ExecutionResult != nil && *req.ExecutionResult != "" {
		queryBuilder = queryBuilder.Where(q.ExecutionResult.Eq(*req.ExecutionResult))
	}

	if req.ExecutionStartTime != nil && req.ExecutionEndTime != nil && !req.ExecutionStartTime.IsZero() && !req.ExecutionEndTime.IsZero() {
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

func SceneLogInsert(data *model.SceneLog) error {
	err := query.SceneLog.Create(data)
	if err != nil {
		return err
	}
	return err
}
