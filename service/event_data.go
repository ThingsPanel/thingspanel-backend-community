package service

import (
	dal "project/dal"
	model "project/model"
	utils "project/utils"
)

type EventData struct{}

func (d *EventData) GetEventDatasListByPage(req *model.GetEventDatasListByPageReq, claims *utils.UserClaims) (interface{}, error) {
	count, data, err := dal.GetEventDatasListByPage(req)
	if err != nil {
		return nil, err
	}

	dataMap := make(map[string]interface{})
	dataMap["count"] = count
	dataMap["list"] = data

	return dataMap, nil
}
