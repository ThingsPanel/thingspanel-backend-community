package service

import (
	dal "project/dal"
	model "project/internal/model"

	"github.com/sirupsen/logrus"
)

type OperationLogs struct{}

func (p *OperationLogs) CreateOperationLogs(operationLog *model.OperationLog) error {
	err := dal.CreateOperationLogs(operationLog)

	if err != nil {
		logrus.Error(err)
	}

	return err
}

func (p *OperationLogs) GetListByPage(Params *model.GetOperationLogListByPageReq) (map[string]interface{}, error) {

	total, list, err := dal.GetListByPage(Params)
	if err != nil {
		return nil, err
	}
	OperationLogsListRsp := make(map[string]interface{})
	OperationLogsListRsp["total"] = total
	OperationLogsListRsp["list"] = list

	return OperationLogsListRsp, err
}
