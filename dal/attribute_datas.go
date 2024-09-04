package dal

import (
	model "project/internal/model"
	query "project/query"

	"github.com/go-basic/uuid"
	"gorm.io/gorm"
)

func GetAttributeDataList(deviceId string) ([]*model.AttributeData, error) {
	data, err := query.AttributeData.
		Where(query.AttributeData.DeviceID.Eq(deviceId)).Find()
	if err != nil {
		return nil, err
	}
	return data, nil
}

/*
select ad.*,dma.data_name from attribute_datas ad
left join devices on ad.device_id = devices.id  left join  device_configs dc on devices.device_config_id = dc.id
left join device_templates dt on dt.id = dc.device_template_id
left join device_model_attributes dma on dt.id = dma.device_template_id and ad.key = dma.data_identifier
where devices.id = 'ca33926c-5ee5-3e9f-147e-94e188fde65b'
*/
// 根据设备ID获取设备属性数据列表并关联查到数据名称如以上sql
func GetAttributeDataListWithDeviceName(deviceId string) ([]map[string]interface{}, error) {
	var data []map[string]interface{}
	err := query.AttributeData.
		Select(query.AttributeData.ALL, query.DeviceModelAttribute.DataName, query.DeviceModelAttribute.Unit, query.DeviceModelAttribute.DataType, query.DeviceModelAttribute.AdditionalInfo.As("enum")).
		LeftJoin(query.Device, query.AttributeData.DeviceID.EqCol(query.Device.ID)).
		LeftJoin(query.DeviceConfig, query.Device.DeviceConfigID.EqCol(query.DeviceConfig.ID)).
		LeftJoin(query.DeviceTemplate, query.DeviceConfig.DeviceTemplateID.EqCol(query.DeviceTemplate.ID)).
		LeftJoin(query.DeviceModelAttribute, query.DeviceTemplate.ID.EqCol(query.DeviceModelAttribute.DeviceTemplateID), query.AttributeData.Key.EqCol(query.DeviceModelAttribute.DataIdentifier)).
		Where(query.Device.ID.Eq(deviceId)).Scan(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func DeleteAttributeData(id string) error {
	_, err := query.AttributeData.
		Where(query.AttributeData.ID.Eq(id)).
		Delete()
	return err
}

func CreateAttributeData(data *model.AttributeData) error {
	return query.AttributeData.Create(data)
}

// 更新设备属性数据，如果数据不存在，UUID生成一个ID，创建一条新的数据
func UpdateAttributeData(data *model.AttributeData) (*model.AttributeData, error) {
	info, err := query.AttributeData.Where(query.AttributeData.DeviceID.Eq(data.DeviceID)).
		Where(query.AttributeData.TenantID.Eq(*data.TenantID)).
		Where(query.AttributeData.Key.Eq(data.Key)).Updates(data)
	if err != nil {
		return nil, err
	} else if info.RowsAffected == 0 {
		data.ID = uuid.New()
		err = query.AttributeData.Create(data)
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	return data, nil
}

func GetAttributeOneKeys(deviceId string, keys string) (interface{}, error) {
	data, err := query.AttributeData.Where(query.AttributeData.DeviceID.Eq(deviceId), query.AttributeData.Key.Eq(keys)).Order(query.AttributeData.T.Desc()).First()
	var result interface{}
	if err != nil {
		return result, err
	} else if err == gorm.ErrRecordNotFound {
		return result, nil
	}
	if data.BoolV != nil {
		//result = fmt.Sprintf("%t", *data.BoolV)
		result = *data.BoolV
	}
	if data.NumberV != nil {
		//result = fmt.Sprintf("%d", data.NumberV)
		result = *data.NumberV
	}
	if data.StringV != nil {
		result = *data.StringV
	}
	return result, nil
}
