package service

import (
	"context"
	dal "project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
	utils "project/pkg/utils"
)

type EventData struct{}

func (*EventData) GetEventDatasListByPage(req *model.GetEventDatasListByPageReq, _ *utils.UserClaims, contexts ...context.Context) (interface{}, error) {
	count, data, err := dal.GetEventDatasListByPage(req, contexts...)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	dataMap := make(map[string]interface{})
	dataMap["count"] = count
	dataMap["list"] = data

	return dataMap, nil
}
