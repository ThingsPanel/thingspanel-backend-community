package dal

import (
	"errors"
	model "project/internal/model"
	query "project/internal/query"
	"project/pkg/common"
	"time"
)

func CreatePeriodicTask(d model.PeriodicTask, tx *query.QueryTx) error {
	if tx != nil {
		return tx.PeriodicTask.Create(&d)
	} else {
		return query.PeriodicTask.Create(&d)
	}
}

func SwitchPeriodicTask(sceneAutomationId, enabled string, tx *query.QueryTx) error {
	_, err := tx.PeriodicTask.
		Where(tx.PeriodicTask.ID.Eq(sceneAutomationId)).
		Update(tx.PeriodicTask.Enabled, enabled)
	return err
}

func GetPeriodicTask(sceneAutomationId string) ([]*model.PeriodicTask, error) {
	data, err := query.PeriodicTask.Where(query.PeriodicTask.SceneAutomationID.Eq(sceneAutomationId)).Find()
	return data, err
}

func DeletePeriodicTask(sceneAutomationId string, tx *query.QueryTx) error {
	if tx != nil {
		_, err := tx.PeriodicTask.Where(tx.PeriodicTask.SceneAutomationID.Eq(sceneAutomationId)).Delete()
		return err
	} else {
		_, err := query.PeriodicTask.Where(query.PeriodicTask.SceneAutomationID.Eq(sceneAutomationId)).Delete()
		return err
	}
}

func GetPeriodicTaskListWithLock(limit int) ([]*model.PeriodicTask, error) {
	key := "irrigation-iot-platform:periodicTask"
	if !common.AcquireLock(key, time.Second*5) {
		return nil, errors.New("未获取到锁")
	}
	defer common.ReleaseLock(key)
	q := query.PeriodicTask
	result, err := q.Where(q.ExecutionTime.Lte(time.Now()), q.Enabled.Eq("Y")).Order(q.ExecutionTime.Asc()).Limit(limit).Find()
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}
	var executeResult []*model.PeriodicTask
	for _, v := range result {
		nextExecuteTime, err := common.GetSceneExecuteTime(v.TaskType, v.Param)
		if err != nil {
			return result, err
		}
		if !v.ExecutionTime.IsZero() {
			executeResult = append(executeResult, v)
		}
		_, _ = q.Where(q.ID.Eq(v.ID)).UpdateColumn(q.ExecutionTime, nextExecuteTime.UTC())
	}

	return executeResult, nil
}
