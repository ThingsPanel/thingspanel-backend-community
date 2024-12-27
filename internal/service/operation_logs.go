package service

import (
	dal "project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/sirupsen/logrus"
)

type OperationLogs struct{}

func (*OperationLogs) CreateOperationLogs(operationLog *model.OperationLog) error {
	err := dal.CreateOperationLogs(operationLog)

	if err != nil {
		logrus.Error(err)
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	return err
}

// 分页查询日志
func (*OperationLogs) GetListByPage(Params *model.GetOperationLogListByPageReq, userClaims *utils.UserClaims) (map[string]interface{}, error) {

	total, list, err := dal.GetListByPage(Params, userClaims)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	OperationLogsListRsp := make(map[string]interface{})
	OperationLogsListRsp["total"] = total
	OperationLogsListRsp["list"] = list

	return OperationLogsListRsp, err
}
