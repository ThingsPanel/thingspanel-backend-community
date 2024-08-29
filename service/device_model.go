package service

import (
	"context"
	"encoding/json"
	"fmt"
	"project/constant"
	"project/dal"
	model "project/internal/model"
	"project/query"
	utils "project/utils"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/go-basic/uuid"
)

type DeviceModel struct{}

// 物模型通用-创建
func (d *DeviceModel) CreateDeviceModelGeneral(req model.CreateDeviceModelReq, what string, claims *utils.UserClaims) (interface{}, error) {

	if req.AdditionalInfo != nil && !IsJSON(*req.AdditionalInfo) {
		return nil, fmt.Errorf("additional_info is not a valid JSON")
	}

	t := time.Now().UTC()
	switch what {
	case model.DEVICE_MODEL_TELEMETRY:
		var deviceModel model.DeviceModelTelemetry
		deviceModel.ID = uuid.New()
		deviceModel.DeviceTemplateID = req.DeviceTemplateId
		deviceModel.DataName = req.DataName
		deviceModel.DataIdentifier = req.DataIdentifier
		deviceModel.ReadWriteFlag = req.ReadWriteFlag
		deviceModel.DataType = req.DataType
		deviceModel.Unit = req.Unit
		deviceModel.Description = req.Description
		deviceModel.AdditionalInfo = req.AdditionalInfo
		deviceModel.CreatedAt = t
		deviceModel.UpdatedAt = t
		deviceModel.Remark = req.Remark
		deviceModel.TenantID = claims.TenantID
		err := dal.CreateDeviceModelTelemetry(&deviceModel)
		if err != nil {
			return nil, err
		} else {
			return deviceModel, nil
		}

	case model.DEVICE_MODEL_ATTRIBUTES:
		var deviceModel model.DeviceModelAttribute
		deviceModel.ID = uuid.New()
		deviceModel.DeviceTemplateID = req.DeviceTemplateId
		deviceModel.DataName = req.DataName
		deviceModel.DataIdentifier = req.DataIdentifier
		deviceModel.ReadWriteFlag = req.ReadWriteFlag
		deviceModel.DataType = req.DataType
		deviceModel.Unit = req.Unit
		deviceModel.Description = req.Description
		deviceModel.AdditionalInfo = req.AdditionalInfo
		deviceModel.CreatedAt = t
		deviceModel.UpdatedAt = t
		deviceModel.Remark = req.Remark
		deviceModel.TenantID = claims.TenantID
		err := dal.CreateDeviceModelAttribute(&deviceModel)
		if err != nil {
			return nil, err
		} else {
			return deviceModel, nil
		}
	case model.DEVICE_MODEL_EVENTS:
		var deviceModel model.DeviceModelEvent
		deviceModel.ID = uuid.New()
		deviceModel.DeviceTemplateID = req.DeviceTemplateId
		deviceModel.DataName = req.DataName
		deviceModel.DataIdentifier = req.DataIdentifier

		deviceModel.Description = req.Description
		deviceModel.AdditionalInfo = req.AdditionalInfo
		deviceModel.CreatedAt = t
		deviceModel.UpdatedAt = t
		deviceModel.Remark = req.Remark
		deviceModel.TenantID = claims.TenantID
		err := dal.CreateDeviceModelEvent(&deviceModel)
		if err != nil {
			return nil, err
		} else {
			return deviceModel, nil
		}
	case model.DEVICE_MODEL_COMMANDS:
		var deviceModel model.DeviceModelCommand
		deviceModel.ID = uuid.New()
		deviceModel.DeviceTemplateID = req.DeviceTemplateId
		deviceModel.DataName = req.DataName
		deviceModel.DataIdentifier = req.DataIdentifier

		deviceModel.Description = req.Description
		deviceModel.AdditionalInfo = req.AdditionalInfo
		deviceModel.CreatedAt = t
		deviceModel.UpdatedAt = t
		deviceModel.Remark = req.Remark
		deviceModel.TenantID = claims.TenantID
		err := dal.CreateDeviceModelCommand(&deviceModel)
		if err != nil {
			return nil, err
		} else {
			return deviceModel, nil
		}
	default:
		return nil, fmt.Errorf("不支持的创建类型")
	}

}

func (d *DeviceModel) CreateDeviceModelGeneralV2(req model.CreateDeviceModelV2Req, what string, claims *utils.UserClaims) (interface{}, error) {

	if req.AdditionalInfo != nil && !IsJSON(*req.AdditionalInfo) {
		return nil, fmt.Errorf("additional_info is not a valid JSON")
	}

	if req.Params != nil && !IsJSON(*req.Params) {
		return nil, fmt.Errorf("params is not a valid JSON")
	}

	t := time.Now().UTC()
	switch what {
	case model.DEVICE_MODEL_EVENTS:
		var deviceModel model.DeviceModelEvent
		deviceModel.ID = uuid.New()
		deviceModel.DeviceTemplateID = req.DeviceTemplateId
		deviceModel.DataName = req.DataName
		deviceModel.DataIdentifier = req.DataIdentifier
		deviceModel.Param = req.Params
		deviceModel.Description = req.Description
		deviceModel.AdditionalInfo = req.AdditionalInfo
		deviceModel.CreatedAt = t
		deviceModel.UpdatedAt = t
		deviceModel.Remark = req.Remark
		deviceModel.TenantID = claims.TenantID
		err := dal.CreateDeviceModelEvent(&deviceModel)
		if err != nil {
			return nil, err
		} else {
			return deviceModel, nil
		}
	case model.DEVICE_MODEL_COMMANDS:
		var deviceModel model.DeviceModelCommand
		deviceModel.ID = uuid.New()
		deviceModel.DeviceTemplateID = req.DeviceTemplateId
		deviceModel.DataName = req.DataName
		deviceModel.DataIdentifier = req.DataIdentifier
		deviceModel.Param = req.Params
		deviceModel.Description = req.Description
		deviceModel.AdditionalInfo = req.AdditionalInfo
		deviceModel.CreatedAt = t
		deviceModel.UpdatedAt = t
		deviceModel.Remark = req.Remark
		deviceModel.TenantID = claims.TenantID
		err := dal.CreateDeviceModelCommand(&deviceModel)
		if err != nil {
			return nil, err
		} else {
			return deviceModel, nil
		}
	default:
		return nil, fmt.Errorf("不支持的创建类型")
	}

}

func (d *DeviceModel) DeleteDeviceModelGeneral(id string, what string, claims *utils.UserClaims) (err error) {
	switch what {
	case model.DEVICE_MODEL_TELEMETRY:
		err = dal.DeleteDeviceModelTelemetry(id)
	case model.DEVICE_MODEL_ATTRIBUTES:
		err = dal.DeleteDeviceModelAttribute(id)
	case model.DEVICE_MODEL_EVENTS:
		err = dal.DeleteDeviceModelEvent(id)
	case model.DEVICE_MODEL_COMMANDS:
		err = dal.DeleteDeviceModelCommand(id)
	default:
		return fmt.Errorf("不支持的删除类型")
	}
	return err
}

func (d *DeviceModel) UpdateDeviceModelGeneral(req model.UpdateDeviceModelReq, what string, claims *utils.UserClaims) (interface{}, error) {

	if req.AdditionalInfo != nil && !IsJSON(*req.AdditionalInfo) {
		return nil, fmt.Errorf("additional_info is not a valid JSON")
	}

	t := time.Now().UTC()

	switch what {
	case model.DEVICE_MODEL_TELEMETRY:
		var deviceModel model.DeviceModelTelemetry
		deviceModel.ID = req.ID
		deviceModel.DataName = req.DataName
		deviceModel.DataIdentifier = req.DataIdentifier
		deviceModel.ReadWriteFlag = req.ReadWriteFlag
		deviceModel.DataType = req.DataType
		deviceModel.Unit = req.Unit
		deviceModel.Description = req.Description
		deviceModel.AdditionalInfo = req.AdditionalInfo
		deviceModel.UpdatedAt = t
		deviceModel.Remark = req.Remark
		deviceModel.TenantID = claims.TenantID
		err := dal.UpdateDeviceModelTelemetry(&deviceModel)
		if err != nil {
			return nil, err
		} else {
			return deviceModel, nil
		}

	case model.DEVICE_MODEL_ATTRIBUTES:
		var deviceModel model.DeviceModelAttribute
		deviceModel.ID = req.ID
		deviceModel.DataName = req.DataName
		deviceModel.DataIdentifier = req.DataIdentifier
		deviceModel.ReadWriteFlag = req.ReadWriteFlag
		deviceModel.DataType = req.DataType
		deviceModel.Unit = req.Unit
		deviceModel.Description = req.Description
		deviceModel.AdditionalInfo = req.AdditionalInfo
		deviceModel.UpdatedAt = t
		deviceModel.Remark = req.Remark
		deviceModel.TenantID = claims.TenantID
		err := dal.UpdateDeviceModelAttribute(&deviceModel)
		if err != nil {
			return nil, err
		} else {
			return deviceModel, nil
		}
	default:
		return nil, fmt.Errorf("不支持的删除类型")
	}
}

func (d *DeviceModel) UpdateDeviceModelGeneralV2(req model.UpdateDeviceModelV2Req, what string, claims *utils.UserClaims) (interface{}, error) {
	if req.AdditionalInfo != nil && !IsJSON(*req.AdditionalInfo) {
		return nil, fmt.Errorf("additional_info is not a valid JSON")
	}

	if req.Params != nil && !IsJSON(*req.Params) {
		return nil, fmt.Errorf("params is not a valid JSON")
	}

	t := time.Now().UTC()

	switch what {
	case model.DEVICE_MODEL_EVENTS:
		var deviceModel model.DeviceModelEvent
		deviceModel.ID = req.ID
		deviceModel.DataName = req.DataName
		deviceModel.DataIdentifier = req.DataIdentifier
		deviceModel.Param = req.Params
		deviceModel.Description = req.Description
		deviceModel.AdditionalInfo = req.AdditionalInfo
		deviceModel.UpdatedAt = t
		deviceModel.Remark = req.Remark
		deviceModel.TenantID = claims.TenantID
		err := dal.UpdateDeviceModelEvent(&deviceModel)
		if err != nil {
			return nil, err
		} else {
			return deviceModel, nil
		}
	case model.DEVICE_MODEL_COMMANDS:
		var deviceModel model.DeviceModelCommand
		deviceModel.ID = req.ID
		deviceModel.DataName = req.DataName
		deviceModel.DataIdentifier = req.DataIdentifier
		deviceModel.Param = req.Params
		deviceModel.Description = req.Description
		deviceModel.AdditionalInfo = req.AdditionalInfo
		deviceModel.UpdatedAt = t
		deviceModel.Remark = req.Remark
		deviceModel.TenantID = claims.TenantID
		err := dal.UpdateDeviceModelCommand(&deviceModel)
		if err != nil {
			return nil, err
		} else {
			return deviceModel, nil
		}
	default:
		return nil, fmt.Errorf("不支持的删除类型")
	}
}

func (d *DeviceModel) GetDeviceModelListByPageGeneral(req model.GetDeviceModelListByPageReq, what string, claims *utils.UserClaims) (interface{}, error) {

	listRsp := make(map[string]interface{})
	switch what {
	case model.DEVICE_MODEL_TELEMETRY:
		count, data, err := dal.GetDeviceModelTelemetryListByPage(req, claims.TenantID)
		if err != nil {
			return nil, err
		}
		listRsp["total"] = count
		listRsp["list"] = data
		return listRsp, nil
	case model.DEVICE_MODEL_ATTRIBUTES:
		count, data, err := dal.GetDeviceModelAttributesListByPage(req, claims.TenantID)
		if err != nil {
			return nil, err
		}
		listRsp["total"] = count
		listRsp["list"] = data
		return listRsp, nil
	case model.DEVICE_MODEL_EVENTS:
		count, data, err := dal.GetDeviceModelEventsListByPage(req, claims.TenantID)
		if err != nil {
			return nil, err
		}
		listRsp["total"] = count
		listRsp["list"] = data
		return listRsp, nil
	case model.DEVICE_MODEL_COMMANDS:
		count, data, err := dal.GetDeviceModelCommandsListByPage(req, claims.TenantID)
		if err != nil {
			return nil, err
		}
		listRsp["total"] = count
		listRsp["list"] = data
		return listRsp, nil
	default:
		return nil, fmt.Errorf("不支持的删除类型")
	}
}

func (d *DeviceModel) GetModelSourceAT(ctx context.Context, param *model.ParamID) ([]model.GetModelSourceATRes, error) {
	var (
		res = make([]model.GetModelSourceATRes, 0)
	)

	resInfo := model.GetModelSourceATRes{
		DataSourceTypeRes: string(constant.TelemetrySource),
		Options:           make([]*model.Options, 0),
	}

	// telemetryList
	telemetryList, err := dal.DeviceModelTelemetryQuery{}.Find(ctx, query.DeviceModelTelemetry.DeviceTemplateID.Eq(param.ID))
	if err != nil {
		logrus.Error(ctx, "[GetModelSourceAT]telemetryList failed:", err)
	}

	for _, telemetry := range telemetryList {
		info := &model.Options{
			Key:      telemetry.DataIdentifier,
			Label:    telemetry.DataName,
			DataType: telemetry.DataType,
		}
		if info.DataType != nil && *info.DataType == "Enum" {
			json.Unmarshal([]byte(*telemetry.AdditionalInfo), &info.Enum)
		}
		resInfo.Options = append(resInfo.Options, info)
	}
	res = append(res, resInfo)

	// attributeList
	resInfo = model.GetModelSourceATRes{
		DataSourceTypeRes: string(constant.AttributeSource),
		Options:           make([]*model.Options, 0),
	}
	attributeList, err := dal.DeviceModelAttributeQuery{}.Find(ctx, query.DeviceModelAttribute.DeviceTemplateID.Eq(param.ID))
	if err != nil {
		logrus.Error(ctx, "[GetModelSourceAT]attributeList failed:", err)
	}

	for _, attribute := range attributeList {
		info := &model.Options{
			Key:      attribute.DataIdentifier,
			Label:    attribute.DataName,
			DataType: attribute.DataType,
		}
		if info.DataType != nil && *info.DataType == "Enum" {
			json.Unmarshal([]byte(*attribute.AdditionalInfo), &info.Enum)
		}
		resInfo.Options = append(resInfo.Options, info)
	}

	res = append(res, resInfo)
	return res, err
}

func (d *DeviceModel) CreateDeviceModelCustomCommands(req model.CreateDeviceModelCustomCommandReq, claims *utils.UserClaims) error {

	if req.EnableStatus != "enable" && req.EnableStatus != "disable" {
		return fmt.Errorf("enable status error")
	}

	var deviceModelCustomCommand model.DeviceModelCustomCommand

	deviceModelCustomCommand.ID = uuid.New()
	deviceModelCustomCommand.DeviceTemplateID = req.DeviceTemplateId
	deviceModelCustomCommand.ButtomName = req.ButtomName
	deviceModelCustomCommand.DataIdentifier = req.DataIdentifier
	deviceModelCustomCommand.Description = req.Description
	deviceModelCustomCommand.Instruct = req.Instruct
	deviceModelCustomCommand.EnableStatus = req.EnableStatus
	deviceModelCustomCommand.Remark = req.Remark
	deviceModelCustomCommand.TenantID = claims.TenantID

	err := dal.CreateDeviceModelCustomCommand(&deviceModelCustomCommand)
	return err
}

func (d *DeviceModel) DeleteDeviceModelCustomCommands(id string) error {
	err := dal.DeleteDeviceModelCustomCommandById(id)
	return err
}

func (d *DeviceModel) UpdateDeviceModelCustomCommands(req model.UpdateDeviceModelCustomCommandReq) error {

	if req.EnableStatus != "enable" && req.EnableStatus != "disable" {
		return fmt.Errorf("enable status error")
	}

	var deviceModelCustomCommand model.DeviceModelCustomCommand

	deviceModelCustomCommand.ID = req.ID
	deviceModelCustomCommand.ButtomName = req.ButtomName
	deviceModelCustomCommand.DataIdentifier = req.DataIdentifier
	deviceModelCustomCommand.Description = req.Description
	deviceModelCustomCommand.Instruct = req.Instruct
	deviceModelCustomCommand.EnableStatus = req.EnableStatus
	deviceModelCustomCommand.Remark = req.Remark

	_, err := dal.UpdateDeviceModelCustomCommand(&deviceModelCustomCommand)
	return err
}

func (d *DeviceModel) GetDeviceModelCustomCommandsByPage(req model.GetDeviceModelListByPageReq, claims *utils.UserClaims) (map[string]interface{}, error) {

	total, list, err := dal.GetDeviceModelCustomCommandsByPage(req, claims.TenantID)
	if err != nil {
		return nil, err
	}
	listRsp := make(map[string]interface{})
	listRsp["total"] = total
	listRsp["list"] = list

	return listRsp, err

}

func (d *DeviceModel) GetDeviceModelCustomCommandsByDeviceId(deviceId string, claims *utils.UserClaims) ([]*model.DeviceModelCustomCommand, error) {
	data, err := dal.GetDeviceModelCustomCommandsByDeviceId(deviceId, claims.TenantID)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (d *DeviceModel) CreateDeviceModelCustomControl(req model.CreateDeviceModelCustomControlReq, claims *utils.UserClaims) error {

	if req.EnableStatus != "enable" && req.EnableStatus != "disable" {
		return fmt.Errorf("enable status error")
	}

	var deviceModelCustomControl model.DeviceModelCustomControl

	deviceModelCustomControl.ID = uuid.New()
	deviceModelCustomControl.DeviceTemplateID = req.DeviceTemplateId
	deviceModelCustomControl.Name = req.Name
	deviceModelCustomControl.ControlType = req.ControlType
	deviceModelCustomControl.Description = req.Description
	deviceModelCustomControl.Content = req.Content
	deviceModelCustomControl.EnableStatus = req.EnableStatus
	deviceModelCustomControl.Remark = req.Remark
	deviceModelCustomControl.TenantID = claims.TenantID

	err := dal.CreateDeviceModelCustomControl(&deviceModelCustomControl)
	return err
}

func (d *DeviceModel) DeleteDeviceModelCustomControl(id string) error {
	err := dal.DeleteDeviceModelCustomControlById(id)
	return err
}

func (d *DeviceModel) UpdateDeviceModelCustomControl(req model.UpdateDeviceModelCustomControlReq) error {

	if *req.EnableStatus != "enable" && *req.EnableStatus != "disable" {
		return fmt.Errorf("enable status error")
	}

	var deviceModelCustomControl model.DeviceModelCustomControl

	deviceModelCustomControl.ID = req.ID
	deviceModelCustomControl.DeviceTemplateID = *req.DeviceTemplateId
	deviceModelCustomControl.Name = *req.Name
	deviceModelCustomControl.ControlType = *req.ControlType
	deviceModelCustomControl.Description = req.Description
	deviceModelCustomControl.Content = req.Content
	deviceModelCustomControl.EnableStatus = *req.EnableStatus
	deviceModelCustomControl.Remark = req.Remark

	_, err := dal.UpdateDeviceModelCustomControl(&deviceModelCustomControl)
	return err
}

func (d *DeviceModel) GetDeviceModelCustomControlByPage(req model.GetDeviceModelListByPageReq, claims *utils.UserClaims) (map[string]interface{}, error) {

	total, list, err := dal.GetDeviceModelCustomControlByPage(req, claims.TenantID)
	if err != nil {
		return nil, err
	}
	listRsp := make(map[string]interface{})
	listRsp["total"] = total
	listRsp["list"] = list

	return listRsp, err

}
