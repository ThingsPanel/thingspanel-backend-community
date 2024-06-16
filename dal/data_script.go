package dal

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func CreateDataScript(data *model.DataScript) error {
	return query.DataScript.Create(data)
}

func UpdateDataScript(data *model.UpdateDataScriptReq) error {
	p := query.DataScript
	t := time.Now().UTC()
	data.UpdatedAt = &t
	_, err := query.DataScript.Where(p.ID.Eq(data.Id)).Updates(data)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func DeleteDataScript(id string) error {
	info, err := query.DataScript.Where(query.DataScript.ID.Eq(id)).Delete()
	if info.RowsAffected == 0 {
		return nil
	}
	return err
}

func GetDataScriptById(id string) (*model.DataScript, error) {
	data, err := query.DataScript.Where(query.DataScript.ID.Eq(id)).First()
	if err != nil {
		logrus.Error(err)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("data script not found")
	}
	return data, err
}

func GetDataScriptListByPage(data *model.GetDataScriptListByPageReq) (int64, interface{}, error) {
	q := query.DataScript
	var count int64
	var dataList interface{}
	queryBuilder := q.WithContext(context.Background())

	if data.DeviceConfigId != nil && *data.DeviceConfigId != "" {
		queryBuilder = queryBuilder.Where(q.DeviceConfigID.Eq(*data.DeviceConfigId))
	}

	if data.ScriptType != nil && *data.ScriptType != "" {
		queryBuilder = queryBuilder.Where(q.ScriptType.Eq(*data.ScriptType))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, dataList, err
	}

	if data.Page != 0 && data.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(data.PageSize)
		queryBuilder = queryBuilder.Offset((data.Page - 1) * data.PageSize)
	}

	dataList, err = queryBuilder.Select().Find()
	if err != nil {
		logrus.Error(err)
		return count, dataList, err
	}

	return count, dataList, err
}

func OnlyOneScriptTypeEnabled(id string) (enabled bool, err error) {
	q := query.DataScript
	var count int64

	data_script, err := GetDataScriptById(id)
	if err != nil {
		logrus.Error(err)
		return false, err
	}

	if data_script.EnableFlag == "Y" {
		return false, fmt.Errorf("the script has been enabled")
	}

	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Not(q.ID.Eq(data_script.ID))
	queryBuilder = queryBuilder.Where(q.DeviceConfigID.Eq(data_script.DeviceConfigID))
	queryBuilder = queryBuilder.Where(q.ScriptType.Eq(data_script.ScriptType))
	queryBuilder = queryBuilder.Where(q.EnableFlag.Eq("Y"))

	count, err = queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return false, err
	}

	if count > 0 {
		return false, fmt.Errorf("other script has been enabled")
	}

	return true, nil
}

func EnableDataScript(data *model.DataScript) error {
	p := query.DataScript
	t := time.Now().UTC()
	data.UpdatedAt = &t
	_, err := query.DataScript.Where(p.ID.Eq(data.ID)).Updates(data)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func GetDeviceIDsByDataScriptID(dataScriptID string) ([]string, error) {
	var deviceIDs []string
	dataScript, err := query.DataScript.Where(query.DataScript.ID.Eq(dataScriptID)).First()
	if err != nil {
		logrus.Error(err)
		return deviceIDs, err
	}
	devices, err := query.Device.Where(query.Device.DeviceConfigID.Eq(dataScript.DeviceConfigID)).Find()
	if err != nil {
		logrus.Error(err)
		return deviceIDs, err
	}
	for _, device := range devices {
		deviceIDs = append(deviceIDs, device.ID)
	}
	return deviceIDs, err
}

func GetDataScriptByDeviceConfigIdAndScriptType(deviceConfigId *string, scriptType string) (*model.DataScript, error) {
	if deviceConfigId == nil || *deviceConfigId == "" {
		return nil, nil
	}
	data, err := query.DataScript.
		Where(
			query.DataScript.DeviceConfigID.Eq(*deviceConfigId),
			query.DataScript.ScriptType.Eq(scriptType),
			query.DataScript.EnableFlag.Eq("Y")).
		First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}
