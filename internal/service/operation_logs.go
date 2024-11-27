package service

import (
	dal "project/internal/dal"
	model "project/internal/model"
	utils "project/pkg/utils"

	"github.com/sirupsen/logrus"
)

type OperationLogs struct{}

func (*OperationLogs) CreateOperationLogs(operationLog *model.OperationLog) error {
	err := dal.CreateOperationLogs(operationLog)

	if err != nil {
		logrus.Error(err)
	}

	return err
}

// 分页查询日志
func (*OperationLogs) GetListByPage(Params *model.GetOperationLogListByPageReq, userClaims *utils.UserClaims) (map[string]interface{}, error) {

	total, list, err := dal.GetListByPage(Params, userClaims)
	if err != nil {
		return nil, err
	}
	OperationLogsListRsp := make(map[string]interface{})
	OperationLogsListRsp["total"] = total
	OperationLogsListRsp["list"] = list

	return OperationLogsListRsp, err
}
