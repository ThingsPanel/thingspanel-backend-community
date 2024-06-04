package dal

import (
	model "project/model"
	query "project/query"
)

func CreateDeviceTriggerCondition(d model.DeviceTriggerCondition, tx *query.QueryTx) error {
	if tx != nil {
		return tx.DeviceTriggerCondition.Create(&d)
	} else {
		return query.DeviceTriggerCondition.Create(&d)
	}
}

func SwitchDeviceTriggerCondition(sceneAutomationId, enabled string, tx *query.QueryTx) error {
	_, err := tx.DeviceTriggerCondition.
		Where(tx.DeviceTriggerCondition.ID.Eq(sceneAutomationId)).
		Update(tx.DeviceTriggerCondition.Enabled, enabled)
	return err
}

func GetDeviceTriggerCondition(sceneAutomationId string) ([]*model.DeviceTriggerCondition, error) {
	data, err := query.DeviceTriggerCondition.
		Where(query.DeviceTriggerCondition.SceneAutomationID.Eq(sceneAutomationId)).
		Find()
	return data, err
}

func DeleteDeviceTriggerCondition(sceneAutomationId string, tx *query.QueryTx) error {
	if tx != nil {
		_, err := tx.DeviceTriggerCondition.Where(tx.DeviceTriggerCondition.SceneAutomationID.Eq(sceneAutomationId)).Delete()
		return err
	} else {
		_, err := query.DeviceTriggerCondition.Where(query.DeviceTriggerCondition.SceneAutomationID.Eq(sceneAutomationId)).Delete()
		return err
	}
}

func GetDeviceTriggerConditionByDeviceId(deviceId string, conditionType string) ([]model.DeviceTriggerCondition, error) {
	var condtionds []model.DeviceTriggerCondition
	qd := query.DeviceTriggerCondition
	err := qd.Where(qd.TriggerConditionType.Eq(conditionType), qd.TriggerSource.Eq(deviceId), qd.Enabled.Eq("Y")).Scan(&condtionds)
	return condtionds, err
}

func GetDeviceTriggerConditionByGroupIds(groupIds []string) ([]model.DeviceTriggerCondition, error) {
	var condtionds []model.DeviceTriggerCondition
	qd := query.DeviceTriggerCondition
	err := qd.Where(qd.GroupID.In(groupIds...), qd.Enabled.Eq("Y")).Scan(&condtionds)
	return condtionds, err
}
