package service

import (
	"project/dal"
	model "project/internal/model"
	utils "project/utils"
)

type SceneAutomationLog struct{}

func (s *SceneAutomationLog) GetSceneAutomationLog(req *model.GetSceneAutomationLogReq, u *utils.UserClaims) (interface{}, error) {
	total, data, err := dal.GetSceneAutomationLog(req, u.TenantID)
	logList := make(map[string]interface{})
	logList["total"] = total
	logList["list"] = data

	return logList, err
}
