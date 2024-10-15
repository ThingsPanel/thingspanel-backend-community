package dal

import (
	"project/internal/model"
	"project/internal/query"

	"github.com/sirupsen/logrus"
)

func GetAllSysFunction() ([]*model.SysFunction, error) {
	data, err := query.SysFunction.Find()
	return data, err
}

func GetSysFunctionById(functionId string) (*model.SysFunction, error) {
	data, err := query.SysFunction.Where(query.SysFunction.ID.Eq(functionId)).First()
	return data, err
}

func UpdateSysFunction(functionId, switchStatus string) error {
	_, err := query.SysFunction.
		Where(query.SysFunction.ID.Eq(functionId)).
		Update(query.SysFunction.EnableFlag, switchStatus)
	if err != nil {
		logrus.Error(err)
	}
	return err
}
