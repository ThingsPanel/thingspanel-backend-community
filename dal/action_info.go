package dal

import (
	model "project/internal/model"
	query "project/query"
)

func CreateActionInfo(d model.ActionInfo, tx *query.QueryTx) error {
	if tx != nil {
		return tx.ActionInfo.Create(&d)
	} else {
		return query.ActionInfo.Create(&d)
	}
}

func GetActionInfo(sceneAutomationId string) ([]*model.ActionInfo, error) {
	data, err := query.ActionInfo.Where(query.ActionInfo.SceneAutomationID.Eq(sceneAutomationId)).Find()
	return data, err
}

func DeleteActionInfo(sceneAutomationId string, tx *query.QueryTx) error {
	if tx != nil {
		_, err := tx.ActionInfo.Where(tx.ActionInfo.SceneAutomationID.Eq(sceneAutomationId)).Delete()
		return err
	} else {
		_, err := query.ActionInfo.Where(query.ActionInfo.SceneAutomationID.Eq(sceneAutomationId)).Delete()
		return err
	}
}

func GetActionInfoListBySceneAutomationId(sceneAutomationIds []string) ([]model.ActionInfo, error) {
	var actionInfos []model.ActionInfo
	qa := query.ActionInfo
	return actionInfos, qa.Where(qa.SceneAutomationID.In(sceneAutomationIds...)).Scan(&actionInfos)
}

// 获取场景动作
func GetActionInfoListBySceneId(sceneIds []string) ([]model.ActionInfo, error) {
	var (
		result      []model.ActionInfo
		actionInfos []model.SceneActionInfo
	)
	qa := query.SceneActionInfo

	err := qa.Where(qa.SceneID.In(sceneIds...)).Scan(&actionInfos)
	if err != nil {
		return result, err
	}
	for _, v := range actionInfos {
		result = append(result, model.ActionInfo{
			SceneAutomationID: v.SceneID,
			ActionTarget:      &v.ActionTarget,
			ActionType:        v.ActionType,
			ActionParamType:   v.ActionParamType,
			ActionParam:       v.ActionParam,
			ActionValue:       v.ActionValue,
			Remark:            v.Remark,
		})
	}
	return result, nil
}

func GetSceneAutomationIdWithAlartBySceneID(sceneIds []string) ([]string, error) {
	var resultSceneIds []string
	result, err := query.ActionInfo.Where(query.ActionInfo.SceneAutomationID.In(sceneIds...), query.ActionInfo.ActionType.Eq(model.AUTOMATE_ACTION_TYPE_ALARM)).Distinct(query.ActionInfo.SceneAutomationID).Find()
	if err != nil {
		return resultSceneIds, nil
	}
	for _, v := range result {
		resultSceneIds = append(resultSceneIds, v.SceneAutomationID)
	}
	return resultSceneIds, nil
}
