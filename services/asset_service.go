package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"fmt"

	"github.com/beego/beego/v2/core/logs"

	"github.com/beego/beego/v2/core/config/yaml"
	"gorm.io/gorm"
)

type AssetService struct {
}

type Device struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Device string `json:"device"`
}

type Extension struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Email       string `json:"email"`
}

type Widget struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Class       string `json:"class"`
	Thumbnail   string `json:"thumbnail"`
	Template    string `json:"template"`
}

type Field struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Type   int64  `json:"type"`
	Symbol string `json:"symbol"`
}

type AssetList struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CustomerID string `json:"customer_id"`
	BusinessID string `json:"business_id"`
}

// list 分页获取资产数据
func (*AssetService) List() []Device {
	var list []Device
	_, dirs, _ := utils.GetFilesAndDirs("./extensions")
	for _, dir := range dirs {
		f := utils.FileExist(dir + "/config.yaml")
		if f {
			conf, err := yaml.ReadYmlReader(dir + "/config.yaml")
			if err != nil {
				fmt.Println(err)
			}
			for k, v := range conf {
				str, ok := v.(map[string]interface{})
				if !ok {
					fmt.Println(err)
				}
				i := Device{
					ID:     k,
					Name:   fmt.Sprint(str["name"]),
					Device: fmt.Sprint(str["device"]),
				}
				list = append(list, i)
			}
		}
	}
	if len(list) == 0 {
		list = []Device{}
	}
	return list
}

// add 添加资产
// func (*AssetService) Add(data string) bool {
// 	// 解析json
// 	res, err := simplejson.NewJson([]byte(data))
// 	if err != nil {
// 		fmt.Println("解析出错", err)
// 	}
// 	rows, _ := res.Array()
// 	var asset_id string
// 	var asset models.Asset
// 	var device_id string
// 	var device models.Device
// 	var field_id string
// 	var field models.FieldMapping

// 	var asset_id2 string
// 	var asset2 models.Asset
// 	var device_id2 string
// 	var device2 models.Device
// 	var field_id2 string
// 	var field2 models.FieldMapping

// 	var asset_id3 string
// 	var asset3 models.Asset
// 	var device_id3 string
// 	var device3 models.Device
// 	var field_id3 string
// 	var field3 models.FieldMapping

// 	flag := psql.Mydb.Transaction(func(tx *gorm.DB) error {
// 		for _, row := range rows {
// 			if each_map, ok := row.(map[string]interface{}); ok {
// 				asset_id = uuid.GetUuid()
// 				asset = models.Asset{
// 					ID:         asset_id,
// 					Name:       fmt.Sprint(each_map["name"]),
// 					Tier:       1,
// 					ParentID:   "0",
// 					BusinessID: fmt.Sprint(each_map["business_id"]),
// 				}
// 				if err := tx.Create(&asset).Error; err != nil {
// 					// 回滚事务
// 					return err
// 				}
// 				for _, each_map2 := range each_map["device"].([]interface{}) {
// 					device_id = uuid.GetUuid()
// 					uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
// 					m := md5.Sum([]byte(uniqid_str))
// 					token := fmt.Sprintf("%x", m)
// 					device = models.Device{
// 						ID:        device_id,
// 						AssetID:   asset.ID,
// 						Token:     token,
// 						Type:      fmt.Sprint(each_map2.(map[string]interface{})["type"]),
// 						Name:      fmt.Sprint(each_map2.(map[string]interface{})["name"]),
// 						Protocol:  fmt.Sprint(each_map2.(map[string]interface{})["protocol"]),
// 						Port:      fmt.Sprint(each_map2.(map[string]interface{})["port"]),
// 						Publish:   fmt.Sprint(each_map2.(map[string]interface{})["publish"]),
// 						Subscribe: fmt.Sprint(each_map2.(map[string]interface{})["subscribe"]),
// 						Username:  fmt.Sprint(each_map2.(map[string]interface{})["username"]),
// 						Password:  fmt.Sprint(each_map2.(map[string]interface{})["password"]),
// 					}
// 					if err := tx.Create(&device).Error; err != nil {
// 						return err
// 					}
// 					for _, each_map22 := range each_map2.(map[string]interface{})["mapping"].([]interface{}) {
// 						field_id = uuid.GetUuid()
// 						field = models.FieldMapping{
// 							ID:        field_id,
// 							DeviceID:  device.ID,
// 							FieldFrom: fmt.Sprint(each_map22.(map[string]interface{})["field_from"]),
// 							FieldTo:   fmt.Sprint(each_map22.(map[string]interface{})["field_to"]),
// 							Symbol:    fmt.Sprint(each_map22.(map[string]interface{})["symbol"]),
// 						}
// 						if err := tx.Create(&field).Error; err != nil {
// 							return err
// 						}
// 					}
// 				}
// 				for _, each_map3 := range each_map["two"].([]interface{}) {
// 					asset_id2 = uuid.GetUuid()
// 					asset2 = models.Asset{
// 						ID:         asset_id2,
// 						Name:       fmt.Sprint(each_map3.(map[string]interface{})["name"]),
// 						Tier:       2,
// 						ParentID:   asset_id,
// 						BusinessID: fmt.Sprint(each_map3.(map[string]interface{})["business_id"]),
// 					}
// 					if err := tx.Create(&asset2).Error; err != nil {
// 						return err
// 					}
// 					for _, each_map33 := range each_map3.(map[string]interface{})["device"].([]interface{}) {
// 						device_id2 = uuid.GetUuid()
// 						uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
// 						m := md5.Sum([]byte(uniqid_str))
// 						token := fmt.Sprintf("%x", m)
// 						device2 = models.Device{
// 							ID:        device_id2,
// 							AssetID:   asset2.ID,
// 							Token:     token,
// 							Type:      fmt.Sprint(each_map33.(map[string]interface{})["type"]),
// 							Name:      fmt.Sprint(each_map33.(map[string]interface{})["name"]),
// 							Protocol:  fmt.Sprint(each_map33.(map[string]interface{})["protocol"]),
// 							Port:      fmt.Sprint(each_map33.(map[string]interface{})["port"]),
// 							Publish:   fmt.Sprint(each_map33.(map[string]interface{})["publish"]),
// 							Subscribe: fmt.Sprint(each_map33.(map[string]interface{})["subscribe"]),
// 							Username:  fmt.Sprint(each_map33.(map[string]interface{})["username"]),
// 							Password:  fmt.Sprint(each_map33.(map[string]interface{})["password"]),
// 						}
// 						if err := tx.Create(&device2).Error; err != nil {
// 							return err
// 						}
// 						for _, each_map333 := range each_map33.(map[string]interface{})["mapping"].([]interface{}) {
// 							field_id2 = uuid.GetUuid()
// 							field2 = models.FieldMapping{
// 								ID:        field_id2,
// 								DeviceID:  device2.ID,
// 								FieldFrom: fmt.Sprint(each_map333.(map[string]interface{})["field_from"]),
// 								FieldTo:   fmt.Sprint(each_map333.(map[string]interface{})["field_to"]),
// 								Symbol:    fmt.Sprint(each_map333.(map[string]interface{})["symbol"]),
// 							}
// 							if err := tx.Create(&field2).Error; err != nil {
// 								return err
// 							}
// 						}
// 					}
// 					for _, each_map44 := range each_map3.(map[string]interface{})["there"].([]interface{}) {
// 						asset_id3 = uuid.GetUuid()
// 						asset3 = models.Asset{
// 							ID:         asset_id3,
// 							Name:       fmt.Sprint(each_map44.(map[string]interface{})["name"]),
// 							Tier:       3,
// 							ParentID:   asset_id2,
// 							BusinessID: fmt.Sprint(each_map44.(map[string]interface{})["business_id"]),
// 						}
// 						if err := tx.Create(&asset3).Error; err != nil {
// 							return err
// 						}
// 						for _, each_map444 := range each_map44.(map[string]interface{})["device"].([]interface{}) {
// 							device_id3 = uuid.GetUuid()
// 							uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
// 							m := md5.Sum([]byte(uniqid_str))
// 							token := fmt.Sprintf("%x", m)
// 							device3 = models.Device{
// 								ID:        device_id3,
// 								AssetID:   asset3.ID,
// 								Token:     token,
// 								Type:      fmt.Sprint(each_map444.(map[string]interface{})["type"]),
// 								Name:      fmt.Sprint(each_map444.(map[string]interface{})["name"]),
// 								Protocol:  fmt.Sprint(each_map444.(map[string]interface{})["protocol"]),
// 								Port:      fmt.Sprint(each_map444.(map[string]interface{})["port"]),
// 								Publish:   fmt.Sprint(each_map444.(map[string]interface{})["publish"]),
// 								Subscribe: fmt.Sprint(each_map444.(map[string]interface{})["subscribe"]),
// 								Username:  fmt.Sprint(each_map444.(map[string]interface{})["username"]),
// 								Password:  fmt.Sprint(each_map444.(map[string]interface{})["password"]),
// 							}
// 							if err := tx.Create(&device3).Error; err != nil {
// 								return err
// 							}
// 							for _, each_map4444 := range each_map444.(map[string]interface{})["mapping"].([]interface{}) {
// 								field_id3 = uuid.GetUuid()
// 								field3 = models.FieldMapping{
// 									ID:        field_id3,
// 									DeviceID:  device3.ID,
// 									FieldFrom: fmt.Sprint(each_map4444.(map[string]interface{})["field_from"]),
// 									FieldTo:   fmt.Sprint(each_map4444.(map[string]interface{})["field_to"]),
// 									Symbol:    fmt.Sprint(each_map4444.(map[string]interface{})["symbol"]),
// 								}
// 								if err := tx.Create(&field3).Error; err != nil {
// 									return err
// 								}
// 							}
// 						}
// 					}
// 				}
// 			}
// 		}
// 		return nil
// 	})

// 	if flag != nil {
// 		return false
// 	}
// 	return true
// }

// 新增设备分组方法,自动计算层级
// 2023-11-26
func (*AssetService) AddNew(data valid.AddAsset) (models.Asset, error) {
	logs.Debug("新增设备分组方法,自动计算层级")
	var asset = &models.Asset{
		ID:         uuid.GetUuid(),
		Name:       data.Name,
		ParentID:   data.ParentID,
		BusinessID: data.BusinessID,
	}

	if data.ParentID == "0" {
		asset.Tier = 1
	} else {
		var parent models.Asset
		psql.Mydb.Where("id = ?", data.ParentID).First(&parent)
		asset.Tier = parent.Tier + 1
	}
	logs.Debug("新增设备分组方法,自动计算层级,层级为", asset.Tier)

	if err := psql.Mydb.Create(&data).Error; err != nil {
		logs.Error("新增设备分组方法,自动计算层级,新增失败", err)
		return *asset, err
	}
	logs.Info("新增设备分组方法,自动计算层级,新增成功")
	return *asset, nil
}

// 修改设备分组方法
// 校验parentid 不能为自己的id
// 校验parentid 不能为自己的子级id
// 2023-11-27
func (*AssetService) UpdateOnlyNew(data valid.EditAsset) (models.Asset, error) {
	logs.Debug("修改设备分组方法")
	var asset models.Asset
	psql.Mydb.Where("id = ?", data.ID).First(&asset)
	if asset.ID == "" {
		logs.Error("修改设备分组方法,修改失败,未找到该设备分组")
		return asset, errors.New("未找到该设备分组")
	}
	if asset.ID == data.ParentID {
		logs.Error("修改设备分组方法,修改失败,父级不能为自己")
		return asset, errors.New("父级不能为自己")
	}

	//校验parentid 不能为自己的子级id或者子级的子级等
	var parents []models.Asset
	parents, d := GetAssetFamilyById(data.ParentID)
	if d != 0 {
		//循环遍历parents中是否有asset
		for _, v := range parents {
			if v.ID == asset.ID {
				logs.Error("修改设备分组方法,修改失败,父级不能为自己的子级")
				return asset, errors.New("父级不能为自己的子级")
			}
		}
	}

	if err := psql.Mydb.Model(&asset).Where("id = ?", data.ID).Updates(map[string]interface{}{
		"name":        data.Name,
		"parent_id":   data.ParentID,
		"business_id": data.BusinessID,
	}).Error; err != nil {
		logs.Error("修改设备分组方法,修改失败", err)
		return asset, err
	}
	logs.Info("修改设备分组方法,修改成功")
	return asset, nil

}

// edit 编辑资产
// func (*AssetService) Edit(data string) bool {
// 	// 解析json
// 	res, err := simplejson.NewJson([]byte(data))
// 	if err != nil {
// 		fmt.Println("解析出错", err)
// 	}
// 	rows, _ := res.Array()
// 	var asset_id string
// 	var asset models.Asset
// 	var device_id string
// 	var device models.Device
// 	var field_id string
// 	var field models.FieldMapping

// 	var asset_id2 string
// 	var asset2 models.Asset
// 	var device_id2 string
// 	var device2 models.Device
// 	var field_id2 string
// 	var field2 models.FieldMapping

// 	var asset_id3 string
// 	var asset3 models.Asset
// 	var device_id3 string
// 	var device3 models.Device
// 	var field_id3 string
// 	var field3 models.FieldMapping

// 	flag := psql.Mydb.Transaction(func(tx *gorm.DB) error {
// 		for _, row := range rows {
// 			if each_map, ok := row.(map[string]interface{}); ok {
// 				if _, exists := each_map["id"]; exists && fmt.Sprint(each_map["id"]) != "0" {
// 					// 修改
// 					asset_id = fmt.Sprint(each_map["id"])
// 					err := tx.Model(&models.Asset{}).Where("id = ?", asset_id).Updates(map[string]interface{}{
// 						"name":        fmt.Sprint(each_map["name"]),
// 						"business_id": fmt.Sprint(each_map["business_id"]),
// 					}).Error
// 					if err != nil {
// 						return err
// 					}
// 				} else {
// 					// 新增
// 					asset_id = uuid.GetUuid()
// 					asset = models.Asset{
// 						ID:         asset_id,
// 						Name:       fmt.Sprint(each_map["name"]),
// 						Tier:       1,
// 						ParentID:   "0",
// 						BusinessID: fmt.Sprint(each_map["business_id"]),
// 					}
// 					if err := tx.Create(&asset).Error; err != nil {
// 						// 回滚事务
// 						return err
// 					}
// 				}
// 				if each_map["device"] != nil {
// 					for _, each_map2 := range each_map["device"].([]interface{}) {
// 						if _, exists := each_map2.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map2.(map[string]interface{})["id"]) != "0" {
// 							// 修改
// 							device_id = fmt.Sprint(each_map2.(map[string]interface{})["id"])
// 							err := tx.Model(&models.Device{}).Where("id = ?", device_id).Updates(map[string]interface{}{
// 								"asset_id":  asset_id,
// 								"type":      fmt.Sprint(each_map2.(map[string]interface{})["type"]),
// 								"name":      fmt.Sprint(each_map2.(map[string]interface{})["name"]),
// 								"protocol":  fmt.Sprint(each_map2.(map[string]interface{})["protocol"]),
// 								"port":      fmt.Sprint(each_map2.(map[string]interface{})["port"]),
// 								"publish":   fmt.Sprint(each_map2.(map[string]interface{})["publish"]),
// 								"subscribe": fmt.Sprint(each_map2.(map[string]interface{})["subscribe"]),
// 								"username":  fmt.Sprint(each_map2.(map[string]interface{})["username"]),
// 								"password":  fmt.Sprint(each_map2.(map[string]interface{})["password"]),
// 							}).Error
// 							if err != nil {
// 								return err
// 							}
// 						} else {
// 							// 新增
// 							device_id = uuid.GetUuid()
// 							uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
// 							m := md5.Sum([]byte(uniqid_str))
// 							token := fmt.Sprintf("%x", m)
// 							device = models.Device{
// 								ID:        device_id,
// 								AssetID:   asset_id,
// 								Token:     token,
// 								Type:      fmt.Sprint(each_map2.(map[string]interface{})["type"]),
// 								Name:      fmt.Sprint(each_map2.(map[string]interface{})["name"]),
// 								Protocol:  fmt.Sprint(each_map2.(map[string]interface{})["protocol"]),
// 								Port:      fmt.Sprint(each_map2.(map[string]interface{})["port"]),
// 								Publish:   fmt.Sprint(each_map2.(map[string]interface{})["publish"]),
// 								Subscribe: fmt.Sprint(each_map2.(map[string]interface{})["subscribe"]),
// 								Username:  fmt.Sprint(each_map2.(map[string]interface{})["username"]),
// 								Password:  fmt.Sprint(each_map2.(map[string]interface{})["password"]),
// 							}
// 							if err := tx.Create(&device).Error; err != nil {
// 								return err
// 							}
// 						}
// 						if each_map2.(map[string]interface{})["mapping"] != nil {
// 							for _, each_map22 := range each_map2.(map[string]interface{})["mapping"].([]interface{}) {
// 								if _, exists := each_map22.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map22.(map[string]interface{})["id"]) != "0" {
// 									// 修改
// 									err := tx.Model(&models.FieldMapping{}).Where("id = ?", fmt.Sprint(each_map22.(map[string]interface{})["id"])).Updates(map[string]interface{}{
// 										"device_id":  device_id,
// 										"field_from": fmt.Sprint(each_map22.(map[string]interface{})["field_from"]),
// 										"field_to":   fmt.Sprint(each_map22.(map[string]interface{})["field_to"]),
// 										"symbol":     fmt.Sprint(each_map22.(map[string]interface{})["symbol"]),
// 									}).Error
// 									if err != nil {
// 										return err
// 									}
// 								} else {
// 									// 新增
// 									field_id = uuid.GetUuid()
// 									field = models.FieldMapping{
// 										ID:        field_id,
// 										DeviceID:  device_id,
// 										FieldFrom: fmt.Sprint(each_map22.(map[string]interface{})["field_from"]),
// 										FieldTo:   fmt.Sprint(each_map22.(map[string]interface{})["field_to"]),
// 										Symbol:    fmt.Sprint(each_map22.(map[string]interface{})["symbol"]),
// 									}
// 									if err := tx.Create(&field).Error; err != nil {
// 										return err
// 									}
// 								}
// 							}
// 						}
// 					}
// 					if each_map["two"] != nil {
// 						for _, each_map3 := range each_map["two"].([]interface{}) {
// 							if _, exists := each_map3.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map3.(map[string]interface{})["id"]) != "0" {
// 								// 修改
// 								asset_id2 = fmt.Sprint(each_map3.(map[string]interface{})["id"])
// 								err := tx.Model(&models.Asset{}).Where("id = ?", asset_id2).Updates(map[string]interface{}{
// 									"name":        fmt.Sprint(each_map3.(map[string]interface{})["name"]),
// 									"parent_id":   asset_id,
// 									"business_id": fmt.Sprint(each_map3.(map[string]interface{})["business_id"]),
// 								}).Error
// 								if err != nil {
// 									return err
// 								}
// 							} else {
// 								// 新增
// 								asset_id2 = uuid.GetUuid()
// 								asset2 = models.Asset{
// 									ID:         asset_id2,
// 									Name:       fmt.Sprint(each_map3.(map[string]interface{})["name"]),
// 									Tier:       2,
// 									ParentID:   asset_id,
// 									BusinessID: fmt.Sprint(each_map3.(map[string]interface{})["business_id"]),
// 								}
// 								if err := tx.Create(&asset2).Error; err != nil {
// 									return err
// 								}
// 							}
// 							if each_map3.(map[string]interface{})["device"] != nil {
// 								for _, each_map33 := range each_map3.(map[string]interface{})["device"].([]interface{}) {
// 									if _, exists := each_map33.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map33.(map[string]interface{})["id"]) != "0" {
// 										// 修改
// 										device_id2 = fmt.Sprint(each_map33.(map[string]interface{})["id"])
// 										err := tx.Model(&models.Device{}).Where("id = ?", device_id2).Updates(map[string]interface{}{
// 											"asset_id":  asset_id2,
// 											"type":      fmt.Sprint(each_map33.(map[string]interface{})["type"]),
// 											"name":      fmt.Sprint(each_map33.(map[string]interface{})["name"]),
// 											"protocol":  fmt.Sprint(each_map33.(map[string]interface{})["protocol"]),
// 											"port":      fmt.Sprint(each_map33.(map[string]interface{})["port"]),
// 											"publish":   fmt.Sprint(each_map33.(map[string]interface{})["publish"]),
// 											"subscribe": fmt.Sprint(each_map33.(map[string]interface{})["subscribe"]),
// 											"username":  fmt.Sprint(each_map33.(map[string]interface{})["username"]),
// 											"password":  fmt.Sprint(each_map33.(map[string]interface{})["password"]),
// 										}).Error
// 										if err != nil {
// 											return err
// 										}
// 									} else {
// 										// 新增
// 										device_id2 = uuid.GetUuid()
// 										uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
// 										m := md5.Sum([]byte(uniqid_str))
// 										token := fmt.Sprintf("%x", m)
// 										device2 = models.Device{
// 											ID:        device_id2,
// 											AssetID:   asset_id2,
// 											Token:     token,
// 											Type:      fmt.Sprint(each_map33.(map[string]interface{})["type"]),
// 											Name:      fmt.Sprint(each_map33.(map[string]interface{})["name"]),
// 											Protocol:  fmt.Sprint(each_map33.(map[string]interface{})["protocol"]),
// 											Port:      fmt.Sprint(each_map33.(map[string]interface{})["port"]),
// 											Publish:   fmt.Sprint(each_map33.(map[string]interface{})["publish"]),
// 											Subscribe: fmt.Sprint(each_map33.(map[string]interface{})["subscribe"]),
// 											Username:  fmt.Sprint(each_map33.(map[string]interface{})["username"]),
// 											Password:  fmt.Sprint(each_map33.(map[string]interface{})["password"]),
// 										}
// 										if err := tx.Create(&device2).Error; err != nil {
// 											return err
// 										}
// 									}
// 									for _, each_map333 := range each_map33.(map[string]interface{})["mapping"].([]interface{}) {
// 										if _, exists := each_map333.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map333.(map[string]interface{})["id"]) != "0" {
// 											// 修改
// 											err := tx.Model(&models.FieldMapping{}).Where("id = ?", fmt.Sprint(each_map333.(map[string]interface{})["id"])).Updates(map[string]interface{}{
// 												"device_id":  device_id2,
// 												"field_from": fmt.Sprint(each_map333.(map[string]interface{})["field_from"]),
// 												"field_to":   fmt.Sprint(each_map333.(map[string]interface{})["field_to"]),
// 												"symbol":     fmt.Sprint(each_map333.(map[string]interface{})["symbol"]),
// 											}).Error
// 											if err != nil {
// 												return err
// 											}
// 										} else {
// 											// 新增
// 											field_id2 = uuid.GetUuid()
// 											field2 = models.FieldMapping{
// 												ID:        field_id2,
// 												DeviceID:  device_id2,
// 												FieldFrom: fmt.Sprint(each_map333.(map[string]interface{})["field_from"]),
// 												FieldTo:   fmt.Sprint(each_map333.(map[string]interface{})["field_to"]),
// 												Symbol:    fmt.Sprint(each_map333.(map[string]interface{})["symbol"]),
// 											}
// 											if err := tx.Create(&field2).Error; err != nil {
// 												return err
// 											}
// 										}
// 									}
// 								}
// 							}
// 							if each_map3.(map[string]interface{})["there"] != nil {
// 								for _, each_map44 := range each_map3.(map[string]interface{})["there"].([]interface{}) {
// 									if _, exists := each_map44.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map44.(map[string]interface{})["id"]) != "0" {
// 										// 修改
// 										asset_id3 = fmt.Sprint(each_map44.(map[string]interface{})["id"])
// 										err := tx.Model(&models.Asset{}).Where("id = ?", asset_id3).Updates(map[string]interface{}{
// 											"name":        fmt.Sprint(each_map44.(map[string]interface{})["name"]),
// 											"parent_id":   asset_id2,
// 											"business_id": fmt.Sprint(each_map44.(map[string]interface{})["business_id"]),
// 										}).Error
// 										if err != nil {
// 											return err
// 										}
// 									} else {
// 										// 新增
// 										asset_id3 = uuid.GetUuid()
// 										asset3 = models.Asset{
// 											ID:         asset_id3,
// 											Name:       fmt.Sprint(each_map44.(map[string]interface{})["name"]),
// 											Tier:       3,
// 											ParentID:   asset_id2,
// 											BusinessID: fmt.Sprint(each_map44.(map[string]interface{})["business_id"]),
// 										}
// 										if err := tx.Create(&asset3).Error; err != nil {
// 											return err
// 										}
// 									}
// 									if each_map44.(map[string]interface{})["device"] != nil {
// 										for _, each_map444 := range each_map44.(map[string]interface{})["device"].([]interface{}) {
// 											if _, exists := each_map444.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map444.(map[string]interface{})["id"]) != "0" {
// 												// 修改
// 												device_id3 = fmt.Sprint(each_map444.(map[string]interface{})["id"])
// 												err := tx.Model(&models.Device{}).Where("id = ?", device_id3).Updates(map[string]interface{}{
// 													"asset_id":  asset_id3,
// 													"type":      fmt.Sprint(each_map444.(map[string]interface{})["type"]),
// 													"name":      fmt.Sprint(each_map444.(map[string]interface{})["name"]),
// 													"protocol":  fmt.Sprint(each_map444.(map[string]interface{})["protocol"]),
// 													"port":      fmt.Sprint(each_map444.(map[string]interface{})["port"]),
// 													"publish":   fmt.Sprint(each_map444.(map[string]interface{})["publish"]),
// 													"subscribe": fmt.Sprint(each_map444.(map[string]interface{})["subscribe"]),
// 													"username":  fmt.Sprint(each_map444.(map[string]interface{})["username"]),
// 													"password":  fmt.Sprint(each_map444.(map[string]interface{})["password"]),
// 												}).Error
// 												if err != nil {
// 													return err
// 												}
// 											} else {
// 												// 新增
// 												device_id3 = uuid.GetUuid()
// 												uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
// 												m := md5.Sum([]byte(uniqid_str))
// 												token := fmt.Sprintf("%x", m)
// 												device3 = models.Device{
// 													ID:        device_id3,
// 													AssetID:   asset_id3,
// 													Token:     token,
// 													Type:      fmt.Sprint(each_map444.(map[string]interface{})["type"]),
// 													Name:      fmt.Sprint(each_map444.(map[string]interface{})["name"]),
// 													Protocol:  fmt.Sprint(each_map444.(map[string]interface{})["protocol"]),
// 													Port:      fmt.Sprint(each_map444.(map[string]interface{})["port"]),
// 													Publish:   fmt.Sprint(each_map444.(map[string]interface{})["publish"]),
// 													Subscribe: fmt.Sprint(each_map444.(map[string]interface{})["subscribe"]),
// 													Username:  fmt.Sprint(each_map444.(map[string]interface{})["username"]),
// 													Password:  fmt.Sprint(each_map444.(map[string]interface{})["password"]),
// 												}
// 												if err := tx.Create(&device3).Error; err != nil {
// 													return err
// 												}
// 											}
// 											if each_map444.(map[string]interface{})["mapping"] != nil {
// 												for _, each_map4444 := range each_map444.(map[string]interface{})["mapping"].([]interface{}) {
// 													if _, exists := each_map4444.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map4444.(map[string]interface{})["id"]) != "0" {
// 														// 修改
// 														err := tx.Model(&models.FieldMapping{}).Where("id = ?", fmt.Sprint(each_map4444.(map[string]interface{})["id"])).Updates(map[string]interface{}{
// 															"device_id":  device_id3,
// 															"field_from": fmt.Sprint(each_map4444.(map[string]interface{})["field_from"]),
// 															"field_to":   fmt.Sprint(each_map4444.(map[string]interface{})["field_to"]),
// 															"symbol":     fmt.Sprint(each_map4444.(map[string]interface{})["symbol"]),
// 														}).Error
// 														if err != nil {
// 															return err
// 														}
// 													} else {
// 														// 新增
// 														field_id3 = uuid.GetUuid()
// 														field3 = models.FieldMapping{
// 															ID:        field_id3,
// 															DeviceID:  device_id3,
// 															FieldFrom: fmt.Sprint(each_map4444.(map[string]interface{})["field_from"]),
// 															FieldTo:   fmt.Sprint(each_map4444.(map[string]interface{})["field_to"]),
// 															Symbol:    fmt.Sprint(each_map4444.(map[string]interface{})["symbol"]),
// 														}
// 														if err := tx.Create(&field3).Error; err != nil {
// 															return err
// 														}
// 													}
// 												}
// 											}
// 										}
// 									}
// 								}
// 							}
// 						}
// 					}
// 				}
// 			}
// 		}
// 		return nil
// 	})
// 	if flag != nil {
// 		return false
// 	}
// 	return true
// }

// 根据ID删除一条asset数据

func (*AssetService) Delete(id, tenantId string) bool {
	result := psql.Mydb.Where("id = ? and tenant_id = ?", id, tenantId).Delete(&models.Asset{})
	return result.Error == nil
}

// 根据ID获取下级资产
func (*AssetService) GetAssetsByParentID(parent_id string) ([]models.Asset, int64, error) {
	var assets []models.Asset
	var count int64
	db := psql.Mydb.Model(&models.Asset{}).Where("parent_id = ?", parent_id)
	db.Count(&count)
	//sqlWhere += "order by wl.created_at desc offset ? limit ?"
	result := db.Find(&assets)
	if len(assets) == 0 {
		assets = []models.Asset{}
	}
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return assets, 0, nil
		}
		return nil, 0, result.Error
	}
	return assets, count, nil
}

// 根据ID和TenantId获取下级资产
func (*AssetService) GetAssetsByParentIDAndTenantId(parent_id, tenant_id string) ([]models.Asset, int64) {
	var assets []models.Asset
	var count int64
	psql.Mydb.Model(&models.Asset{}).Where("parent_id = ? and tenant_id = ?", parent_id, tenant_id).Count(&count)
	//sqlWhere += "order by wl.created_at desc offset ? limit ?"
	result := psql.Mydb.Model(&models.Asset{}).Where("parent_id = ? and tenant_id = ?", parent_id, tenant_id).Find(&assets)

	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(assets) == 0 {
		assets = []models.Asset{}
	}
	return assets, count
}

// func (*AssetService) GetAssetsByTierAndBusinessID(business_id string) ([]models.Asset, int64) {
// 	var assets []models.Asset
// 	var count int64
// 	db := psql.Mydb.Model(&models.Asset{}).Where("tier=1 AND business_id = ?", business_id)
// 	result := db.Find(&assets)
// 	db.Count(&count)
// 	if len(assets) == 0 {
// 		assets = []models.Asset{}
// 	}
// 	if result.Error != nil {
// 		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
// 			return assets, 1
// 		}
// 		return nil, 0
// 	}
// 	return assets, count
// }

// Extension 获取组件
func (*AssetService) Extension() []Extension {
	var es []Extension
	_, dirs, _ := utils.GetFilesAndDirs("./extensions")
	for _, dir := range dirs {
		f := utils.FileExist(dir + "/config.yaml")
		if f {
			conf, err := yaml.ReadYmlReader(dir + "/config.yaml")
			if err != nil {
				fmt.Println(err)
			}
			for k, v := range conf {
				e, _ := v.(map[string]interface{})
				if len(e) > 0 {
					i := Extension{
						Key:         k,
						Type:        fmt.Sprint(e["type"]),
						Name:        fmt.Sprint(e["name"]),
						Description: fmt.Sprint(e["description"]),
						Version:     fmt.Sprint(e["version"]),
						Author:      fmt.Sprint(e["author"]),
						Email:       fmt.Sprint(e["email"]),
					}
					es = append(es, i)
				}
			}
		}
	}
	if len(es) == 0 {
		es = []Extension{}
	}
	return es
}

// Extension 插件名列表
func (*AssetService) ExtensionName(reqField string) []models.ExtensionDataMap {
	//dirs, _ := utils.GetDirs("./extensions")
	var dataMapStructList []models.ExtensionDataMap
	// for _, reqField := range dirs {
	f := utils.FileExist("./extensions/" + reqField + "/config.yaml")
	if f {
		conf, err := yaml.ReadYmlReader("./extensions/" + reqField + "/config.yaml")
		if err != nil {
			fmt.Println(err)
		}
		//去重list
		var diffStringList []string
		//0-没有重复 1-有重复
		diffFlag := 0
		var fieldStructList []models.ExtensionFields
		for _, v := range conf[reqField].(map[string]interface{})["widgets"].(map[string]interface{}) {
			for km, kv := range v.(map[string]interface{}) {
				if km == "fields" {
					vMap, _ := kv.(map[string]interface{})
					for fk, fv := range vMap {
						//遍历list，检查是否有重复的值
						for _, s := range diffStringList {
							if s == fk {
								diffFlag = 1
							}
						}
						//如果没有
						if diffFlag == 0 {
							//放入去重list
							diffStringList = append(diffStringList, fk)
							fieldStruct := models.ExtensionFields{
								Key:    fk,
								Name:   fmt.Sprint(fv.(map[string]interface{})["name"]),
								Symbol: fmt.Sprint(fv.(map[string]interface{})["symbol"]),
								Type:   fmt.Sprint(fv.(map[string]interface{})["type"]),
							}
							fieldStructList = append(fieldStructList, fieldStruct)
						} else {
							diffFlag = 0
						}
					}
				}
			}
		}
		if len(fieldStructList) != 0 {
			dataMapStruct := models.ExtensionDataMap{
				Name:  fmt.Sprint(conf[reqField].(map[string]interface{})["device"]),
				Field: fieldStructList,
			}
			dataMapStructList = append(dataMapStructList, dataMapStruct)
		}

	}

	//}
	return dataMapStructList
}

// widget 获取组件
func (*AssetService) Widget(id string) []Widget {

	var w []Widget
	if id != "" {
		_, dirs, _ := utils.GetFilesAndDirs("./extensions")
		for _, dir := range dirs {
			f := utils.FileExist(dir + "/config.yaml")
			if f {
				conf, err := yaml.ReadYmlReader(dir + "/config.yaml")
				if err != nil {
					fmt.Println(err)
				}
				for k, v := range conf {
					if id == "" {
						str, _ := v.(map[string]interface{})
						widgets, _ := str["widgets"].(map[string]interface{})
						if len(widgets) > 0 {
							for wk, wv := range widgets {
								item, _ := wv.(map[string]interface{})
								i := Widget{
									Key:         wk,
									Name:        fmt.Sprint(item["name"]),
									Description: fmt.Sprint(item["description"]),
									Class:       fmt.Sprint(item["class"]),
									Thumbnail:   fmt.Sprint(item["thumbnail"]),
									Template:    fmt.Sprint(item["template"]),
								}
								w = append(w, i)
							}
						}
					} else if id == k {
						str, _ := v.(map[string]interface{})
						widgets, _ := str["widgets"].(map[string]interface{})
						if len(widgets) > 0 {
							for wk, wv := range widgets {
								item, _ := wv.(map[string]interface{})
								i := Widget{
									Key:         wk,
									Name:        fmt.Sprint(item["name"]),
									Description: fmt.Sprint(item["description"]),
									Class:       fmt.Sprint(item["class"]),
									Thumbnail:   fmt.Sprint(item["thumbnail"]),
									Template:    fmt.Sprint(item["template"]),
								}
								w = append(w, i)
							}
						}
					}
				}
			}
		}
	}
	if len(w) == 0 {
		w = []Widget{}
	}
	return w
}

// widget 获取组件字段fields
func (*AssetService) Field(id string, widget_id string) []Field {
	var w []Field
	_, dirs, _ := utils.GetFilesAndDirs("./extensions")
	for _, dir := range dirs {
		f := utils.FileExist(dir + "/config.yaml")
		if f {
			conf, err := yaml.ReadYmlReader(dir + "/config.yaml")
			if err != nil {
				fmt.Println(err)
			}
			for k, v := range conf {
				if id == k {
					str, _ := v.(map[string]interface{})
					widgets, _ := str["widgets"].(map[string]interface{})
					if len(widgets) > 0 {
						for wk, wv := range widgets {
							if wk == widget_id {
								item, _ := wv.(map[string]interface{})
								fields, _ := item["fields"].(map[string]interface{})
								if len(fields) > 0 {
									for fk, fv := range fields {
										fieldItem, _ := fv.(map[string]interface{})
										var fin string
										var fit int64
										var fis string
										if fieldItem["name"] == nil {
											fin = ""
										} else {
											fin = fmt.Sprint(fieldItem["name"])
										}
										if fieldItem["type"] == nil {
											fit = 0
										} else {
											fit = int64(fieldItem["type"].(int))
										}
										if fieldItem["symbol"] == nil {
											fis = ""
										} else {
											fis = fmt.Sprint(fieldItem["symbol"])
										}
										i := Field{
											Key:    fk,
											Name:   fin,
											Type:   fit,
											Symbol: fis,
										}
										w = append(w, i)
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if len(w) == 0 {
		w = []Field{}
	}
	return w
}

// 根据业务id查询父资产
func (*AssetService) GetAssetByBusinessId(business_id, tenant_id string) ([]AssetList, int64) {
	var assets []AssetList
	var count int64
	db := psql.Mydb.Model(&models.Asset{}).Where("business_id = ? AND parent_id='0'", business_id)
	result := db.Find(&assets)
	db.Count(&count)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return assets, 1
		}
		return nil, 0
	}
	if len(assets) == 0 {
		assets = []AssetList{}
	}
	return assets, count
}

// GetAssetDataByBusinessId根据业务id查询业务下所有资产
func (*AssetService) GetAssetDataByBusinessId(business_id, tenantId string) (assets []AssetList, err error) {
	err = psql.Mydb.Model(&models.Asset{}).Where("business_id = ? and tenant_id = ?", business_id, tenantId).Find(&assets).Error
	if err != nil {
		return nil, err
	}
	return assets, nil
}

// 设备数据
// func (*AssetService) GetAssetData(business_id string) ([]models.Asset, int64) {
// 	var assets []models.Asset
// 	var count int64
// 	db := psql.Mydb.Model(&models.Asset{}).Where("business_id = ? AND tier=1", business_id)
// 	result := db.Find(&assets)
// 	db.Count(&count)
// 	if len(assets) == 0 {
// 		assets = []models.Asset{}
// 	}
// 	if result.Error != nil {
// 		logs.Error("GetAssetData", result.Error)
// 		return assets, 0
// 	}
// 	return assets, count
// }

// 获取全部Asset
func (*AssetService) All() ([]models.Asset, int64) {
	var assets []models.Asset
	result := psql.Mydb.Find(&assets)
	if len(assets) == 0 {
		assets = []models.Asset{}
	}
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return nil, 0
	}
	return assets, result.RowsAffected
}

// 资产下拉框
func (*AssetService) Simple() (assets []models.Simple, err error) {
	if err = psql.Mydb.Table("asset").Find(&assets).Error; err != nil {
		return nil, err
	}
	return assets, nil
}

// 通过业务id获取设备分组
func (*AssetService) PageGetDeviceGroupByBussinessID(business_id, tenant_id string, current int, pageSize int) ([]map[string]interface{}, int64) {
	sqlWhere := `select a.id,a."name" , (with RECURSIVE ast as 
		( 
		(select aa.id,cast('/'as varchar(255))as name,aa.parent_id  from asset aa where id=a.id) 
		union  
		(select tt.id,cast (CONCAT('/',kk.name,tt.name ) as varchar(255))as name ,kk.parent_id from ast tt inner join asset  kk on kk.id = tt.parent_id )
		)select name from ast where parent_id='0' limit 1) as parent_group ,a.parent_id from asset a where business_id = ? and tenant_id = ? order by parent_group asc`
	var values []interface{}
	values = append(values, business_id, tenant_id)
	var count int64
	result := psql.Mydb.Raw(sqlWhere, values...).Count(&count)
	if result.Error != nil {
		// errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
	}
	var offset int = (current - 1) * pageSize
	var limit int = pageSize
	sqlWhere += " offset ? limit ?"
	values = append(values, offset, limit)
	var assetList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&assetList)
	if dataResult.Error != nil {
		//errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
		logs.Error(dataResult.Error.Error())
	}
	return assetList, count
}

// 通过业务id获取设备分组下拉列表
func (*AssetService) DeviceGroupByBussinessID(business_id, tenant_id string) ([]map[string]interface{}, int64) {
	sqlWhere := `select a.id,(with RECURSIVE ast as 
		( 
		(select aa.id,cast(CONCAT('/',aa.name) as varchar(255))as name,aa.parent_id  from asset aa where id=a.id) 
		union  
		(select tt.id,cast (CONCAT('/',kk.name,tt.name ) as varchar(255))as name ,kk.parent_id from ast tt inner join asset  kk on kk.id = tt.parent_id )
		)select name from ast where parent_id='0' limit 1)  as device_group from asset a where business_id = ? and tenant_id = ? order by device_group asc`
	var values []interface{}
	values = append(values, business_id, tenant_id)
	var count int64
	result := psql.Mydb.Raw(sqlWhere, values...)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
	} else {
		count = result.RowsAffected
	}
	var assetList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&assetList)
	if dataResult.Error != nil {
		//errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
		logs.Error(dataResult.Error.Error())
	}
	return assetList, count
}

// 根据id获取一条asset数据
func (*AssetService) GetAssetById(id string) (*models.Asset, int64) {
	var asset models.Asset
	result := psql.Mydb.Where("id = ?", id).First(&asset)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		return nil, 0
	}
	return &asset, result.RowsAffected
}

// 根据id获取所有父级数据
func GetAssetFamilyById(id string) ([]models.Asset, int64) {
	var asset []models.Asset
	sqlWhere := `WITH RECURSIVE parent_chain AS (
		SELECT
		  id,
		  parent_id,
		  name,
			  tier
		FROM
		  asset
		WHERE
		  id = ?
		UNION ALL
		SELECT
		  yt.id,
		  yt.parent_id,
		  yt.name,
			  yt.tier
		FROM
		  asset yt
		INNER JOIN
		  parent_chain pc ON yt.id = pc.parent_id
	  )
	  SELECT * FROM parent_chain`
	result := psql.Mydb.Raw(sqlWhere, id).Scan(&asset)
	if result.Error != nil {
		return nil, 0
	}
	return asset, result.RowsAffected
}

// 根据GetAssetFamilyById的数据，用于组装告警信息
func (*AssetService) GetAssetFamilyInfoById(id string) string {
	data, rowsAffected := GetAssetFamilyById(id)
	if rowsAffected == 0 {
		return ""
	}
	var assetNames string
	// 反向组装，将父级放在前面
	for i := rowsAffected - 1; i >= 0; i-- {
		assetNames += data[i].Name
		if i == (rowsAffected - 1) {
			assetNames += "/"
		} else {
			assetNames += "/"
		}
	}
	return assetNames
}
