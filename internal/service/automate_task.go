package service

import (
	"errors"
	"project/internal/dal"
	model "project/internal/model"
	"time"

	"github.com/spf13/viper"
)

type AutomateTask struct {
}

var (
	errAutomationClaim         = errors.New("legacy automation task claim failed")
	errAutomationActionRead    = errors.New("legacy automation action lookup failed")
	errAutomationTargetRead    = errors.New("legacy automation target lookup failed")
	errAutomationActionExecute = errors.New("legacy automation action execution failed")
	errAutomationBatch         = errors.New("legacy automation task batch failed")
)

// OnceTaskExecute
// @description 单次任务执行
// @params t *AutomateTask
// @return error
func (t *AutomateTask) OnceTaskExecute() error {
	if err := requireLegacyAutomation(); err != nil {
		return err
	}
	limit := viper.GetInt("automation_task_confg.once_task_limit")
	result, err := dal.GetOnceTaskListWithLock(limit)
	if err != nil {
		return errAutomationClaim
	}
	if len(result) == 0 {
		return nil
	}
	return executeOnceBatch(result, t.TaskAutomationExecute, dal.TaskExpirationSave)
}

func executeOnceBatch(tasks []*model.OneTimeTask, execute func(string) error, saveExpired func([]string) error) error {
	var expIds []string
	failed := false
	for _, v := range tasks {
		if v.ExpirationTime > 0 && v.ExecutionTime.Add(time.Duration(v.ExpirationTime)*time.Minute).Before(time.Now()) {
			expIds = append(expIds, v.ID)
			continue
		}
		if execute(v.SceneAutomationID) != nil {
			failed = true
		}
	}
	if saveExpired(expIds) != nil {
		failed = true
	}
	if failed {
		return errAutomationBatch
	}
	return nil
}

// TaskAutomationExecute
// @description 单次任务执行
// @params t *AutomateTask
// @return error
func (*AutomateTask) TaskAutomationExecute(sceneAutomationId string) error {
	if err := requireLegacyAutomation(); err != nil {
		return err
	}
	//查询自动化是否关闭
	if GroupApp.CheckSceneAutomationHasClose(sceneAutomationId) {
		return nil
	}
	actions, err := dal.GetActionInfoListBySceneAutomationId([]string{sceneAutomationId})
	if err != nil {
		return errAutomationActionRead
	}
	var (
		deviceIds      []string
		deviceConfigId []string
	)
	for _, v := range actions {
		if v.ActionType == model.AUTOMATE_ACTION_TYPE_MULTIPLE && v.ActionTarget != nil {
			deviceConfigId = append(deviceConfigId, *v.ActionTarget)
		}
	}
	if len(deviceConfigId) > 0 {
		deviceIds, err = dal.GetDeviceIdsByDeviceConfigId(deviceConfigId)
		if err != nil {
			return errAutomationTargetRead
		}
	}
	if err := GroupApp.SceneAutomateExecute(sceneAutomationId, deviceIds, actions); err != nil {
		return errAutomationActionExecute
	}
	return nil
}

// PeriodicTaskExecute
// @description 循环任务执行
// @params t *AutomateTask
// @return error
func (t *AutomateTask) PeriodicTaskExecute() error {
	if err := requireLegacyAutomation(); err != nil {
		return err
	}

	limit := viper.GetInt("automation_task_confg.periodic_task_limit")
	result, err := dal.GetPeriodicTaskListWithLock(limit)
	if err != nil {
		return errAutomationClaim
	}
	if len(result) == 0 {
		return nil
	}
	return executePeriodicBatch(result, t.TaskAutomationExecute)
}

func executePeriodicBatch(tasks []*model.PeriodicTask, execute func(string) error) error {
	failed := false
	for _, v := range tasks {
		if v.ExpirationTime > 0 && v.ExecutionTime.Add(time.Duration(v.ExpirationTime)*time.Minute).Before(time.Now()) {
			continue
		}
		if execute(v.SceneAutomationID) != nil {
			failed = true
		}
	}
	if failed {
		return errAutomationBatch
	}
	return nil
}
