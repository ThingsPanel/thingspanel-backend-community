package service

import (
	"project/internal/dal"
	model "project/internal/model"
	utils "project/pkg/utils"
)

type SceneAutomationLog struct{}

func (*SceneAutomationLog) GetSceneAutomationLog(req *model.GetSceneAutomationLogReq, u *utils.UserClaims) (interface{}, error) {
	if err := ensureTenantDeviceAdministrator(u); err != nil {
		return nil, err
	}
	total, data, err := dal.GetSceneAutomationLog(req, u.TenantID)
	logList := make(map[string]interface{})
	logList["total"] = total
	logList["list"] = data

	return logList, err
}
