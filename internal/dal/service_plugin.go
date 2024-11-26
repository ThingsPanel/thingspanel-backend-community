package dal

import (
	"context"
	"encoding/json"
	"errors"
	"project/internal/model"
	"project/internal/query"
	"time"

	"github.com/sirupsen/logrus"
)

// 删除服务插件
func DeleteServicePlugin(id string) error {

	tx, err := StartTransaction()
	if err != nil {
		Rollback(tx)
		return err
	}
	serviceAccess := tx.ServiceAccess

	_, err = serviceAccess.Where(serviceAccess.ServicePluginID.Eq(id)).Delete()
	if err != nil {
		Rollback(tx)
		return err
	}

	servicePlugin := tx.ServicePlugin

	_, err = servicePlugin.Where(query.ServicePlugin.ID.Eq(id)).Delete()
	if err != nil {
		Rollback(tx)
		return err
	}

	err = Commit(tx)
	return err
}

func UpdateServicePlugin(id string, updates map[string]interface{}) error {
	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())
	_, err := queryBuilder.Where(query.ServicePlugin.ID.Eq(id)).Updates(updates)
	return err
}

func GetServicePluginListByPage(req *model.GetServicePluginByPageReq) (int64, interface{}, error) {
	var count int64
	var servicePlugins []map[string]interface{}

	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())
	if req.ServiceType != 0 {
		queryBuilder = queryBuilder.Where(q.ServiceType.Eq(req.ServiceType))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, servicePlugins, err
	}
	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}
	timeNow := time.Now().UTC()
	err = queryBuilder.Select().Order(q.CreateAt.Desc()).Scan(&servicePlugins)
	if err != nil {
		logrus.Error(err)
		return count, servicePlugins, err
	}
	// 在 Go 代码中计算 service_heartbeat
	for i := range servicePlugins {
		lastActiveTime, ok := servicePlugins[i]["last_active_time"].(time.Time)
		if !ok {
			// 处理 LastActiveTime 不是 time.Time 类型的情况
			logrus.Warn("LastActiveTime is not of type time.Time for plugin ", i)
			servicePlugins[i]["service_heartbeat"] = 2 // 默认设置为不活跃
			continue
		}

		if timeNow.Sub(lastActiveTime) > time.Minute {
			servicePlugins[i]["service_heartbeat"] = 2 // 不活跃
		} else {
			servicePlugins[i]["service_heartbeat"] = 1 // 活跃
		}
	}
	return count, servicePlugins, err
}

func GetServicePlugin(id string) (interface{}, error) {
	var servicePlugin *model.ServicePlugin

	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())

	queryBuilder = queryBuilder.Where(q.ID.Eq(id))

	servicePlugin, err := queryBuilder.First()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return servicePlugin, err
}

// 通过service_plugin_id获取插件服务信息
func GetServicePluginByID(id string) (*model.ServicePlugin, error) {
	// 使用first查询
	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())
	servicePlugin, err := queryBuilder.Where(q.ID.Eq(id)).Select().First()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return servicePlugin, nil
}

// 通过service_plugin_id获取配置文件中的http_address
func GetServicePluginHttpAddressByID(id string) (*model.ServicePlugin, string, error) {
	servicePlugin, err := GetServicePluginByID(id)
	if err != nil {
		return nil, "", err
	}

	if servicePlugin.ServiceConfig == nil || *servicePlugin.ServiceConfig == "" {
		// 服务配置错误，无法获取表单
		return nil, "", errors.New("service plugin config error, can not get form")
	}
	// 解析服务配置model.ServicePluginConfig
	var serviceAccessConfig model.ServiceAccessConfig
	err = json.Unmarshal([]byte(*servicePlugin.ServiceConfig), &serviceAccessConfig)
	if err != nil {
		return nil, "", errors.New("service plugin config error: " + err.Error())
	}
	// 校验服务配置的HttpAddress是否是ip:port格式
	if serviceAccessConfig.HttpAddress == "" {
		return nil, "", errors.New("服务插件HTTP服务地址未配置，请联系系统管理员检测配置")
	}
	return servicePlugin, serviceAccessConfig.HttpAddress, nil
}

// 通过service_identifier获取插件服务信息
func GetServicePluginByServiceIdentifier(serviceIdentifier string) (*model.ServicePlugin, error) {
	if serviceIdentifier == "MQTT" {
		return &model.ServicePlugin{
			Name:              "MQTT",
			ServiceType:       1,
			ServiceConfig:     nil,
			ServiceIdentifier: "MQTT",
		}, nil
	}
	// 使用first查询
	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())
	servicePlugin, err := queryBuilder.Where(q.ServiceIdentifier.Eq(serviceIdentifier)).Select().First()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return servicePlugin, nil
}

// 通过device_config_id获取插件服务信息
func GetServicePluginByDeviceConfigID(deviceConfigID string) (*model.ServicePlugin, error) {
	// 获取设备配置信息
	deviceConfig, err := GetDeviceConfigByID(deviceConfigID)
	if err != nil {
		return nil, err
	}
	// 插件服务信息
	return GetServicePluginByServiceIdentifier(*deviceConfig.ProtocolType)
}

// 通过device_config_id获取主题前缀
func GetServicePluginSubTopicPrefixByDeviceConfigID(deviceConfigID string) (string, error) {
	servicePlugin, err := GetServicePluginByDeviceConfigID(deviceConfigID)
	if err != nil {
		logrus.Error("failed to get service plugin by device config id: ", err)
		return "", err
	}
	var subTopicPrefix string
	if servicePlugin.ServiceType == int32(1) {
		var protocolAccessConfig model.ProtocolAccessConfig
		if servicePlugin.ServiceConfig == nil {
			err = errors.New("service config is empty")
			return "", err
		}
		err = json.Unmarshal([]byte(*servicePlugin.ServiceConfig), &protocolAccessConfig)
		if err != nil {
			logrus.Error("failed to unmarshal service config: ", err)
			return "", err
		}
		if protocolAccessConfig.SubTopicPrefix != "" {
			subTopicPrefix = protocolAccessConfig.SubTopicPrefix
		}
	} else if servicePlugin.ServiceType == int32(2) {
		var serviceAccessConfig model.ServiceAccessConfig
		if servicePlugin.ServiceConfig == nil {
			err = errors.New("service config is empty")
			return "", err
		}
		err = json.Unmarshal([]byte(*servicePlugin.ServiceConfig), &serviceAccessConfig)
		if err != nil {
			logrus.Error("failed to unmarshal service config: ", err)
			return "", err
		}
		if serviceAccessConfig.SubTopicPrefix != "" {
			subTopicPrefix = serviceAccessConfig.SubTopicPrefix
		}

	}
	return subTopicPrefix, nil
}

// 更新服务插件的心跳时间
func UpdateServicePluginHeartbeat(serviceIdentifier string) error {
	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())
	// last_active_time UTC时间
	t := time.Now().UTC()
	info, err := queryBuilder.Where(q.ServiceIdentifier.Eq(serviceIdentifier)).Update(q.LastActiveTime, t)
	if err != nil {
		logrus.Error(err)
	}
	if info.RowsAffected == 0 {
		return errors.New("service plugin not found")
	}
	return err
}

// GetServiceSelectList
func GetServiceSelectList() ([]model.ServicePlugin, error) {
	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())
	var servicePlugins []model.ServicePlugin
	err := queryBuilder.Select().Scan(&servicePlugins)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return servicePlugins, nil
}
