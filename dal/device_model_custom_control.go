package dal

import (
	"fmt"
	model "project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
)

func CreateDeviceModelCustomControl(data *model.DeviceModelCustomControl) error {
	return query.DeviceModelCustomControl.Create(data)
}

func DeleteDeviceModelCustomControlById(id string) error {
	info, err := query.DeviceModelCustomControl.Where(query.DeviceModelCustomControl.ID.Eq(id)).Delete()
	if err != nil {
		logrus.Error(err)
	}

	if info.RowsAffected == 0 {
		return fmt.Errorf("no data deleted")
	}

	return err

}

func UpdateDeviceModelCustomControl(data *model.DeviceModelCustomControl) (*model.DeviceModelCustomControl, error) {
	info, err := query.DeviceModelCustomControl.Where(query.DeviceModelCustomControl.ID.Eq(data.ID)).Updates(data)
	if err != nil {
		return nil, err
	} else if info.RowsAffected == 0 {
		return nil, fmt.Errorf("update device model custom control failed, no rows affected")
	}
	return data, err
}
