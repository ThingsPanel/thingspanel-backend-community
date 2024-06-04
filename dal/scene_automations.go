package dal

import (
	"context"
	"fmt"
	"project/common"
	"project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
)

func CreateSceneAutomation(d *model.SceneAutomation, tx *query.QueryTx) error {
	if tx != nil {
		return tx.SceneAutomation.Create(d)
	} else {
		return query.SceneAutomation.Create(d)
	}
}

func SaveSceneAutomation(d *model.SceneAutomation, tx *query.QueryTx) error {
	if tx != nil {
		return tx.SceneAutomation.Save(d)
	} else {
		return query.SceneAutomation.Save(d)
	}
}

func DeleteSceneAutomation(id string, tx *query.QueryTx) error {
	if tx != nil {
		_, err := tx.SceneAutomation.Where(tx.SceneAutomation.ID.Eq(id)).Delete()
		return err
	} else {
		_, err := query.SceneAutomation.Where(query.SceneAutomation.ID.Eq(id)).Delete()
		return err
	}

}

func GetSceneAutomation(id string, tx *query.QueryTx) (*model.SceneAutomation, error) {
	if tx != nil {
		data, err := tx.SceneAutomation.Where(tx.SceneAutomation.ID.Eq(id)).First()
		return data, err
	} else {
		data, err := query.SceneAutomation.Where(query.SceneAutomation.ID.Eq(id)).First()
		if err != nil {
			logrus.Error(err)
		}
		return data, err
	}
}

func SwitchSceneAutomation(id, enabled string, tx *query.QueryTx) error {
	_, err := tx.SceneAutomation.Where(tx.SceneAutomation.ID.Eq(id)).Update(tx.SceneAutomation.Enabled, enabled)
	return err
}

func GetSceneAutomationByPage(req *model.GetSceneAutomationByPageReq, tenant_id string) (int64, []*model.SceneAutomation, error) {
	q := query.SceneAutomation

	var count int64
	ctx := context.Background()
	queryBuilder := q.WithContext(ctx)
	if req.Name != nil && *req.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *req.Name)))
	}
	if req.DeviceId != nil && *req.DeviceId != "" {
		sceneIds, _ := getSceneAutomationIdByDeviceId(ctx, *req.DeviceId)
		//logrus.Warning(sceneIds)
		if len(sceneIds) == 0 {
			return count, nil, nil
		}
		queryBuilder = queryBuilder.Where(q.ID.In(sceneIds...))
	}
	if req.DeviceConfigId != nil && *req.DeviceConfigId != "" {
		sceneIds, _ := getSceneAutomationIdByDeviceConfigId(ctx, *req.DeviceConfigId)
		if len(sceneIds) == 0 {
			return count, nil, nil
		}
		queryBuilder = queryBuilder.Where(q.ID.In(sceneIds...))
	}

	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenant_id))

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}
	sceneList, err := queryBuilder.Order(q.CreatedAt.Desc()).Find()
	if err != nil {
		return count, sceneList, err
	}
	return count, sceneList, nil
}

func GetSceneAutomationWithAlarmByPageReq(req *model.GetSceneAutomationsWithAlarmByPageReq, tenant_id string) (int64, []*model.SceneAutomation, error) {
	q := query.SceneAutomation

	var (
		count    int64
		sceneIds []string
	)
	ctx := context.Background()
	queryBuilder := q.WithContext(ctx)
	if !common.IsStringEmpty(req.DeviceId) {
		sceneIds, _ = getSceneAutomationIdByDeviceId(ctx, *req.DeviceId)
		deviceConfig, err := GetDeviceByID(*req.DeviceId)
		if err != nil {
			return count, nil, err
		}
		if deviceConfig.DeviceConfigID != nil && *deviceConfig.DeviceConfigID != "" {
			sceneIds2, _ := getSceneAutomationIdByDeviceConfigId(ctx, *deviceConfig.DeviceConfigID)
			sceneIds = append(sceneIds, sceneIds2...)
		}
	} else {
		sceneIds, _ = getSceneAutomationIdByDeviceConfigId(ctx, *req.DeviceConfigId)
	}

	if len(sceneIds) == 0 {
		return count, nil, nil
	}
	//查询包含告警的场景
	sceneIds, err := GetSceneAutomationIdWithAlartBySceneID(sceneIds)
	if err != nil {
		return count, nil, err
	}
	if len(sceneIds) == 0 {
		return count, nil, nil
	}

	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenant_id), q.ID.In(sceneIds...))
	count, err = queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}
	sceneList, err := queryBuilder.Order(q.CreatedAt.Desc()).Find()
	if err != nil {
		return count, sceneList, err
	}
	return count, sceneList, nil
}

func getSceneAutomationIdByDeviceId(ctx context.Context, deviceId string) ([]string, error) {
	q := query.DeviceTriggerCondition
	var result []model.DeviceTriggerCondition
	var sceneIds []string
	err := q.WithContext(ctx).Where(q.TriggerConditionType.Eq(model.DEVICE_TRIGGER_CONDITION_TYPE_ONE), q.TriggerSource.Eq(deviceId)).Scan(&result)
	if err != nil {
		return sceneIds, err
	}

	for _, v := range result {
		logrus.Warning(v)
		sceneIds = append(sceneIds, v.SceneAutomationID)
	}
	var result2 []model.ActionInfo
	qa := query.ActionInfo
	err = qa.WithContext(ctx).Where(qa.ActionParamType.Eq(model.AUTOMATE_ACTION_TYPE_ONE), qa.ActionTarget.Eq(deviceId)).Scan(&result2)
	if err != nil {
		return sceneIds, err
	}
	for _, v := range result2 {
		sceneIds = append(sceneIds, v.SceneAutomationID)
	}
	return sceneIds, nil
}

func getSceneAutomationIdByDeviceConfigId(ctx context.Context, deviceConfigId string) ([]string, error) {
	q := query.DeviceTriggerCondition
	var result []model.DeviceTriggerCondition
	var sceneIds []string
	err := q.WithContext(ctx).Where(q.TriggerConditionType.Eq(model.DEVICE_TRIGGER_CONDITION_TYPE_MULTIPLE), q.TriggerSource.Eq(deviceConfigId)).Scan(&result)
	if err != nil {
		return sceneIds, err
	}
	for _, v := range result {
		sceneIds = append(sceneIds, v.SceneAutomationID)
	}
	var result2 []model.ActionInfo
	qa := query.ActionInfo
	err = qa.WithContext(ctx).Where(qa.ActionParamType.Eq(model.AUTOMATE_ACTION_TYPE_MULTIPLE), qa.ActionTarget.Eq(deviceConfigId)).Scan(&result2)
	if err != nil {
		return sceneIds, err
	}
	for _, v := range result {
		sceneIds = append(sceneIds, v.SceneAutomationID)
	}
	return sceneIds, nil
}

func GetSceneAutomationTenantID(ctx context.Context, scene_id string) string {
	//todo 增加缓存
	var tenantID string
	query.SceneAutomation.WithContext(ctx).Where(query.SceneAutomation.ID.Eq(scene_id)).Select(query.SceneAutomation.TenantID).Scan(&tenantID)
	return tenantID

}
