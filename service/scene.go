package service

import (
	"project/dal"
	model "project/model"
	utils "project/utils"
)

type Scene struct{}

func (s *Scene) CreateScene(req model.CreateSceneReq, claims *utils.UserClaims) (string, error) {
	id, err := dal.CreateSceneInfo(req, claims)
	return id, err
}

func (s *Scene) UpdateScene(req model.UpdateSceneReq, claims *utils.UserClaims) (string, error) {
	id, err := dal.UpdateSceneInfo(req, claims)
	return id, err
}

func (s *Scene) DeleteScene(scene_id string) error {
	return dal.DeleteSceneInfo(scene_id)
}

func (s *Scene) GetScene(scene_id string) (interface{}, error) {
	sceneInfo, err := dal.GetSceneInfo(scene_id)
	if err != nil {
		return nil, err
	}

	sceneActionsInfo, err := dal.GetSceneActionsInfo(scene_id)
	if err != nil {
		return nil, err
	}

	res := make(map[string]interface{})
	res["info"] = sceneInfo
	res["actions"] = sceneActionsInfo
	return res, nil
}

func (s *Scene) GetSceneListByPage(req model.GetSceneListByPageReq, claims *utils.UserClaims) (interface{}, error) {
	total, sceneInfo, err := dal.GetSceneInfoByPage(&req, claims.TenantID)
	if err != nil {
		return nil, err
	}
	sceneListMap := make(map[string]interface{})
	sceneListMap["total"] = total
	sceneListMap["list"] = sceneInfo
	return sceneListMap, nil
}

// TODO
func (s *Scene) ActiveScene(scene_id, userId, tenantID string) error {

	return GroupApp.ActiveSceneExecute(scene_id, tenantID)
	// actions, err := dal.GetActionInfoListBySceneAutomationId([]string{scene_id})
	// if err != nil {
	// 	return nil
	// }
	// var (
	// 	deviceIds      []string
	// 	deviceConfigId []string
	// )
	// for _, v := range actions {
	// 	if v.ActionType == model.AUTOMATE_ACTION_TYPE_MULTIPLE && v.ActionTarget != nil {
	// 		deviceConfigId = append(deviceConfigId, *v.ActionTarget)
	// 	}
	// }
	// if len(deviceConfigId) > 0 {
	// 	deviceIds, err = dal.GetDeviceIdsByDeviceConfigId(deviceConfigId)
	// 	if err != nil {
	// 		return err
	// 	}
	// }
	// details, err := GroupApp.AutomateActionExecute(scene_id, deviceIds, actions)
	// var exeResult string
	// if err == nil {
	// 	exeResult = "S"
	// } else {
	// 	exeResult = "F"
	// }
	// logrus.Debug(details)
	// return dal.SceneLogInsert(&model.SceneLog{
	// 	ID:              uuid.New(),
	// 	SceneID:         scene_id,
	// 	ExecutedAt:      time.Now().UTC(),
	// 	Detail:          details,
	// 	ExecutionResult: exeResult,
	// 	TenantID:        tenantID,
	// })
}

func (s *Scene) GetSceneLog(req model.GetSceneLogListByPageReq) (interface{}, error) {
	total, data, err := dal.GetSceneLogByPage(req)
	if err != nil {
		return nil, err
	}
	sceneLogList := make(map[string]interface{})
	sceneLogList["total"] = total
	sceneLogList["list"] = data
	return sceneLogList, nil
}
