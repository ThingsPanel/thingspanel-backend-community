package service

import (
	"fmt"
	"project/internal/dal"
	model "project/internal/model"
)

type SysFunction struct{}

func (*SysFunction) GetSysFuncion() ([]*model.SysFunction, error) {
	data, err := dal.GetAllSysFunction()
	return data, err
}

func (*SysFunction) UpdateSysFuncion(function_id string) error {
	old, err := dal.GetSysFunctionById(function_id)
	if err != nil {
		return err
	}
	if old.ID == "" {
		return fmt.Errorf("not found")
	}

	var upTarget string

	if old.EnableFlag == "enable" {
		upTarget = "disable"
	} else {
		upTarget = "enable"
	}

	err = dal.UpdateSysFunction(function_id, upTarget)

	return err
}
