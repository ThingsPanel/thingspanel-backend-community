package dal

import (
	"context"
	"errors"
	"time"

	model "project/internal/model"
	query "project/internal/query"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

const (
	DEVICE_TYPE_1 = "DRIECT_ATTACHED_PROTOCOL"
	DEVICE_TYPE_2 = "GATEWAY_PROTOCOL"
)

func CreateProtocolPluginWithDict(p *model.CreateProtocolPluginReq) (*model.ProtocolPlugin, error) {
	var dictCode string
	if p.DeviceType == 1 {
		dictCode = DEVICE_TYPE_1
	} else if p.DeviceType == 2 {
		dictCode = DEVICE_TYPE_2
	} else {
		return nil, errors.New("deviceType is invalid")
	}
	logrus.Info("dictCode:", dictCode)
	// 开启事物
	tx, err := StartTransaction()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	t := time.Now().UTC()
	// 创建sys_dict
	var dict = model.SysDict{}
	dictId := uuid.New()
	dict.ID = dictId
	dict.DictCode = dictCode
	dict.DictValue = p.ProtocolType
	dict.CreatedAt = t
	if err := CreateDict(&dict, tx); err != nil {
		tx.Rollback()
		return nil, err
	}
	// 创建sys_dict_language
	var dictLanguage = model.SysDictLanguage{}
	dictLanguage.ID = uuid.New()
	dictLanguage.DictID = dictId
	dictLanguage.LanguageCode = p.LanguageCode
	dictLanguage.Translation = p.Name

	if err := CreateDictLanguage(&dictLanguage, tx); err != nil {
		tx.Rollback()
		return nil, err
	}
	// 创建protocol_plugins
	var protocolPlugin = model.ProtocolPlugin{}
	protocolPlugin.ID = uuid.New()
	protocolPlugin.Name = p.Name
	protocolPlugin.DeviceType = p.DeviceType
	protocolPlugin.ProtocolType = p.ProtocolType
	protocolPlugin.AccessAddress = p.AccessAddress
	protocolPlugin.HTTPAddress = p.HTTPAddress
	protocolPlugin.SubTopicPrefix = p.SubTopicPrefix
	protocolPlugin.Description = p.Description
	protocolPlugin.AdditionalInfo = p.AdditionalInfo
	protocolPlugin.CreatedAt = t
	protocolPlugin.UpdateAt = t
	protocolPlugin.Remark = p.Remark
	if err := tx.ProtocolPlugin.Create(&protocolPlugin); err != nil {
		tx.Rollback()
		return nil, err
	} else {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	return &protocolPlugin, nil
}

func DeleteProtocolPluginWithDict(id string) error {
	// 开启事物
	tx, err := StartTransaction()
	if err != nil {
		return err
	}

	// 根据ID获取信息
	dictLanguage, err := tx.ProtocolPlugin.Where(tx.ProtocolPlugin.ID.Eq(id)).First()
	if err != nil {
		return err
	}

	var dictCode string
	if dictLanguage.DeviceType == 1 {
		dictCode = DEVICE_TYPE_1
	} else {
		dictCode = DEVICE_TYPE_2
	}

	// 查询dict,用于删除dict_language表
	dict, err := tx.SysDict.Where(tx.SysDict.DictCode.Eq(dictCode), tx.SysDict.DictValue.Eq(dictLanguage.ProtocolType)).First()
	if err != nil {
		// 查询失败，无需回滚
		return err
	}

	// 删除dict
	_, err = tx.SysDict.Where(tx.SysDict.DictCode.Eq(dictCode), tx.SysDict.DictValue.Eq(dictLanguage.ProtocolType)).Delete()
	if err != nil {
		// 删除失败，无需回滚，前面无变更
		return err
	}

	// 删除language
	_, err = tx.SysDictLanguage.Where(tx.SysDictLanguage.DictID.Eq(dict.ID)).Delete()
	if err != nil {
		// 删除失败，因为上面已经删除，需要回滚
		tx.Rollback()
		return err
	}

	// 删除协议插件
	_, err = tx.ProtocolPlugin.Where(tx.ProtocolPlugin.ID.Eq(id)).Delete()
	if err != nil {
		// 删除失败，因为上面已经删除，需要回滚
		tx.Rollback()
		return err
	}

	// 提交
	tx.Commit()
	return nil
}

func UpdateProtocolPluginWithDict(p *model.UpdateProtocolPluginReq) error {

	// 开启事物
	tx, err := StartTransaction()
	if err != nil {
		return err
	}

	// 更新dict时，需要知道旧的ProtocolType
	oldProtocolPlugin, err := tx.ProtocolPlugin.Where(tx.ProtocolPlugin.ID.Eq(p.Id)).First()
	if err != nil {
		return err
	}

	// 更新插件
	var pp = model.ProtocolPlugin{}
	pp.ID = p.Id
	pp.Name = p.Name
	pp.DeviceType = p.DeviceType
	pp.ProtocolType = p.ProtocolType
	pp.AccessAddress = p.AccessAddress
	pp.HTTPAddress = p.HTTPAddress
	pp.SubTopicPrefix = p.SubTopicPrefix
	pp.Description = p.Description
	pp.AdditionalInfo = p.AdditionalInfo
	pp.UpdateAt = time.Now().UTC()
	pp.Remark = p.Remark

	err = tx.ProtocolPlugin.Save(&pp)
	if err != nil {
		return err
	}

	// 判断新旧dict是否一致
	// if oldProtocolPlugin.DeviceType != p.DeviceType || oldProtocolPlugin.ProtocolType != p.ProtocolType {
	var dictCode string
	if oldProtocolPlugin.DeviceType == int16(1) {
		dictCode = DEVICE_TYPE_1
	} else {
		dictCode = DEVICE_TYPE_2
	}

	// 查找已存在的dict
	dict, err := tx.SysDict.Where(tx.SysDict.DictCode.Eq(dictCode), tx.SysDict.DictValue.Eq(oldProtocolPlugin.ProtocolType)).First()

	if err != nil {
		// 查找失败需要回滚，上方已经写入
		tx.Rollback()
		return err
	}

	var newDict = model.SysDict{}
	var newDictCode string
	if p.DeviceType == int16(1) {
		newDictCode = DEVICE_TYPE_1
	} else {
		newDictCode = DEVICE_TYPE_2
	}
	newDict.ID = dict.ID
	newDict.DictCode = newDictCode
	newDict.DictValue = p.ProtocolType

	err = tx.SysDict.Save(&newDict)
	if err != nil {
		// update失败需要回滚
		tx.Rollback()
		return err
	}

	// 判断新旧dict_language是否一致 查找已存在的dict_language
	oldDictLanguage, err := tx.SysDictLanguage.Where(tx.SysDictLanguage.DictID.Eq(dict.ID)).First()
	if err != nil {
		// update失败需要回滚
		tx.Rollback()
		return err
	}
	if oldProtocolPlugin.Name != p.Name || oldDictLanguage.LanguageCode != p.LanguageCode {
		var newDictLanguage = model.SysDictLanguage{}
		newDictLanguage.LanguageCode = p.LanguageCode
		newDictLanguage.Translation = p.Name
		_, err = tx.SysDictLanguage.Where(tx.SysDictLanguage.DictID.Eq(dict.ID)).Updates(newDictLanguage)
		if err != nil {
			// update失败需要回滚
			tx.Rollback()
			return err
		}
	}

	// }

	tx.Commit()

	return nil
}

func GetProtocolPluginListByPage(p *model.GetProtocolPluginListByPageReq) (int64, interface{}, error) {
	q := query.ProtocolPlugin
	var count int64
	queryBuilder := q.WithContext(context.Background())
	count, err := queryBuilder.Count()

	if p.Page != 0 && p.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(p.PageSize)
		queryBuilder = queryBuilder.Offset((p.Page - 1) * p.PageSize)
	}

	protocolPluginList, err := queryBuilder.Select().Order(q.CreatedAt.Desc()).Find()
	if err != nil {
		logrus.Error(err)
		return count, protocolPluginList, err
	}
	return count, protocolPluginList, err
}

// 通过协议类型和设备类型获取协议插件,没有查到回返回nil
func GetProtocolPluginByDeviceTypeAndProtocolType(deviceType int16, protocolType string) (*model.ProtocolPlugin, error) {
	q := query.ProtocolPlugin
	protocolPlugin, err := q.Where(q.DeviceType.Eq(deviceType), q.ProtocolType.Eq(protocolType)).First()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return protocolPlugin, nil
}

// 通过设备配置id获取协议插件，没有则返回nil
func GetProtocolPluginByDeviceConfigID(deviceConfigID string) (*model.ProtocolPlugin, error) {
	q := query.DeviceConfig
	deviceConfig, err := q.Where(q.ID.Eq(deviceConfigID)).First()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	if deviceConfig.ProtocolType == nil {
		return nil, errors.New("protocolType is nil")
	}
	// 网关设备和子设备都是2
	var deviceType int16
	if deviceConfig.DeviceType == "1" {
		deviceType = 1
	} else if deviceConfig.DeviceType == "2" || deviceConfig.DeviceType == "3" {
		deviceType = 2
	} else {
		return nil, errors.New("deviceType is invalid")
	}

	// 系统内的协议类型为MQTT的不需要协议插件
	if *deviceConfig.ProtocolType == "MQTT" {
		return nil, nil
	}
	protocolPlugin, err := GetProtocolPluginByDeviceTypeAndProtocolType(deviceType, *deviceConfig.ProtocolType)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return protocolPlugin, nil
}
