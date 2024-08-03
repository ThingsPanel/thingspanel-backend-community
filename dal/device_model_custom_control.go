package dal

import (
	model "project/model"
	query "project/query"
)

func CreateDeviceModelCustomControl(data *model.DeviceModelCustomControl) error {
	return query.DeviceModelCustomControl.Create(data)
}
