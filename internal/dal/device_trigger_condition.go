package dal

import (
	"errors"
	"sort"
	"strings"

	model "project/internal/model"
	query "project/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInvalidDeviceTriggerSource = errors.New("direct-device trigger source is empty")

// LockTenantDeviceTriggerSources serializes device-trigger references with
// device deletion. Call it inside the same transaction as the scene mutation.
func LockTenantDeviceTriggerSources(tenantID string, sourceIDs []string, tx *query.QueryTx) error {
	if tx == nil {
		return gorm.ErrInvalidTransaction
	}
	ids := make([]string, 0, len(sourceIDs))
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		if strings.TrimSpace(id) == "" {
			return ErrInvalidDeviceTriggerSource
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		device := tx.Device
		if _, err := device.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(device.ID.Eq(id), device.TenantID.Eq(tenantID)).First(); err != nil {
			return err
		}
	}
	return nil
}

// ListDeviceTriggerSources returns the direct-device sources of one scene.
// The caller must first lock the scene row so concurrent updates cannot change
// this set while it is being reconciled with a replacement request.
func ListDeviceTriggerSources(sceneAutomationID string, tx *query.QueryTx) ([]string, error) {
	conditions := tx.DeviceTriggerCondition
	rows, err := conditions.Where(
		conditions.SceneAutomationID.Eq(sceneAutomationID),
		conditions.TriggerConditionType.Eq(model.DEVICE_TRIGGER_CONDITION_TYPE_ONE),
	).Find()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.TriggerSource == nil || strings.TrimSpace(*row.TriggerSource) == "" {
			return nil, ErrInvalidDeviceTriggerSource
		}
		ids = append(ids, *row.TriggerSource)
	}
	return ids, nil
}

func CreateDeviceTriggerCondition(d model.DeviceTriggerCondition, tx *query.QueryTx) error {
	if tx != nil {
		return tx.DeviceTriggerCondition.Create(&d)
	} else {
		return query.DeviceTriggerCondition.Create(&d)
	}
}

func SwitchDeviceTriggerCondition(sceneAutomationId, enabled string, tx *query.QueryTx) error {
	_, err := tx.DeviceTriggerCondition.
		Where(tx.DeviceTriggerCondition.SceneAutomationID.Eq(sceneAutomationId)).
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

func GetDeviceTriggerConditionListByDeviceId(deviceId string) ([]model.DeviceTriggerCondition, error) {
	var condtionds []model.DeviceTriggerCondition
	qd := query.DeviceTriggerCondition
	err := qd.Where(qd.TriggerSource.Eq(deviceId)).Scan(&condtionds)
	return condtionds, err
}

func GetDeviceTriggerConditionByGroupIds(groupIds []string) ([]model.DeviceTriggerCondition, error) {
	var condtionds []model.DeviceTriggerCondition
	qd := query.DeviceTriggerCondition
	err := qd.Where(qd.GroupID.In(groupIds...), qd.Enabled.Eq("Y")).Scan(&condtionds)
	return condtionds, err
}
