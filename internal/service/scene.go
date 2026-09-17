package service

import (
	"project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
	utils "project/pkg/utils"
)

type Scene struct{}

func (*Scene) CreateScene(req model.CreateSceneReq, claims *utils.UserClaims) (string, error) {
	if err := ensureTenantDeviceAdministrator(claims); err != nil {
		return "", err
	}
	id, err := dal.CreateSceneInfo(req, claims)
	if err != nil {
		return "", errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return id, err
}

func (*Scene) UpdateScene(req model.UpdateSceneReq, claims *utils.UserClaims) (string, error) {
	if err := ensureTenantDeviceAdministrator(claims); err != nil {
		return "", err
	}
	id, err := dal.UpdateSceneInfo(req, claims)
	if err != nil {
		return "", errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return id, err
}

func (*Scene) DeleteScene(scene_id string, claims *utils.UserClaims) error {
	if err := ensureTenantDeviceAdministrator(claims); err != nil {
		return err
	}
	err := dal.DeleteSceneInfo(scene_id, claims.TenantID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return nil
}

func (*Scene) GetScene(scene_id string, claims *utils.UserClaims) (interface{}, error) {
	if err := ensureTenantDeviceAdministrator(claims); err != nil {
		return nil, err
	}
	sceneInfo, err := dal.GetSceneInfoByTenant(scene_id, claims.TenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	sceneActionsInfo, err := dal.GetSceneActionsInfo(scene_id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	res := make(map[string]interface{})
	res["info"] = sceneInfo
	res["actions"] = sceneActionsInfo
	return res, nil
}

func (*Scene) GetSceneListByPage(req model.GetSceneListByPageReq, claims *utils.UserClaims) (interface{}, error) {
	if err := ensureTenantDeviceAdministrator(claims); err != nil {
		return nil, err
	}
	total, sceneInfo, err := dal.GetSceneInfoByPage(&req, claims.TenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	sceneListMap := make(map[string]interface{})
	sceneListMap["total"] = total
	sceneListMap["list"] = sceneInfo
	return sceneListMap, nil
}

// TODO
func (*Scene) ActiveScene(scene_id, _ string, claims *utils.UserClaims) error {
	if err := ensureTenantDeviceAdministrator(claims); err != nil {
		return err
	}
	err := GroupApp.ActiveSceneExecute(scene_id, claims.TenantID)
	if err != nil {
		return err
	}
	return nil
}

func (*Scene) GetSceneLog(req model.GetSceneLogListByPageReq, claims *utils.UserClaims) (interface{}, error) {
	if err := ensureTenantDeviceAdministrator(claims); err != nil {
		return nil, err
	}
	total, data, err := dal.GetSceneLogByPage(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	sceneLogList := make(map[string]interface{})
	sceneLogList["total"] = total
	sceneLogList["list"] = data
	return sceneLogList, nil
}
