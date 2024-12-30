package service

import (
	"project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
)

type SysFunction struct{}

func (*SysFunction) GetSysFuncion() ([]*model.SysFunction, error) {
	data, err := dal.GetAllSysFunction()
	return data, err
}

func (*SysFunction) UpdateSysFuncion(function_id string) error {
	old, err := dal.GetSysFunctionById(function_id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if old.ID == "" {
		return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"msg": "id is nil",
		})
	}

	var upTarget string

	if old.EnableFlag == "enable" {
		upTarget = "disable"
	} else {
		upTarget = "enable"
	}

	err = dal.UpdateSysFunction(function_id, upTarget)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return err
}
