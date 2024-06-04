package dal

import (
	"errors"
	"project/common"
	model "project/model"
	query "project/query"
	"time"
)

func CreateOneTimeTask(d model.OneTimeTask, tx *query.QueryTx) error {
	if tx != nil {
		return tx.OneTimeTask.Create(&d)
	} else {
		return query.OneTimeTask.Create(&d)
	}
}

func SwitchOneTimeTask(sceneAutomationId, enabled string, tx *query.QueryTx) error {
	_, err := tx.OneTimeTask.
		Where(tx.OneTimeTask.SceneAutomationID.Eq(sceneAutomationId)).
		Update(tx.OneTimeTask.Enabled, enabled)
	return err
}

func GetOneTimeTask(sceneAutomationId string) ([]*model.OneTimeTask, error) {
	data, err := query.OneTimeTask.Where(query.OneTimeTask.SceneAutomationID.Eq(sceneAutomationId)).Find()
	return data, err
}

func DeleteOneTimeTask(sceneAutomationId string, tx *query.QueryTx) error {
	if tx != nil {
		_, err := tx.OneTimeTask.Where(tx.OneTimeTask.SceneAutomationID.Eq(sceneAutomationId)).Delete()
		return err
	} else {
		_, err := query.OneTimeTask.Where(query.OneTimeTask.SceneAutomationID.Eq(sceneAutomationId)).Delete()
		return err
	}
}

func GetOnceTaskListWithLock(limit int) ([]*model.OneTimeTask, error) {
	key := "irrigation-iot-platform:onceTask"
	if !common.AcquireLock(key, time.Second*5) {
		return nil, errors.New("未获取到锁")
	}
	defer common.ReleaseLock(key)
	q := query.OneTimeTask
	result, err := q.Where(q.ExecutionTime.Lte(time.Now()), q.Enabled.Eq("Y"), q.ExecutingState.Eq("NEX")).Order(q.ExecutionTime.Asc()).Limit(limit).Find()
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}
	var taskId []string
	for _, v := range result {
		taskId = append(taskId, v.ID)
	}
	_, err = q.Where(q.ID.In(taskId...)).UpdateColumn(q.ExecutingState, "EXE")
	if err != nil {
		return nil, err
	}
	return result, nil
}

func TaskExpirationSave(ids []string) error {
	_, err := query.OneTimeTask.Where(query.OneTimeTask.ID.In(ids...)).UpdateColumn(query.OneTimeTask.ExecutingState, "EXP")

	return err
}
