package service

import (
	dal "project/internal/dal"
	model "project/internal/model"
	utils "project/pkg/utils"
)

type EventData struct{}

func (*EventData) GetEventDatasListByPage(req *model.GetEventDatasListByPageReq, claims *utils.UserClaims) (interface{}, error) {
	count, data, err := dal.GetEventDatasListByPage(req)
	if err != nil {
		return nil, err
	}

	dataMap := make(map[string]interface{})
	dataMap["count"] = count
	dataMap["list"] = data

	return dataMap, nil
}
