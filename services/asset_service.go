package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	"crypto/md5"
	"errors"
	"fmt"

	"github.com/beego/beego/v2/core/config/yaml"
	simplejson "github.com/bitly/go-simplejson"
	"github.com/mintance/go-uniqid"
	"gorm.io/gorm"
)

type AssetService struct {
}

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
					ID:   k,
					Name: str["name"].(string),
				}
				list = append(list, i)
			}
		}
	}
	return list
}

// add 添加资产
func (*AssetService) Add(data string) bool {
	// var data string = `[
	// 	{
	// 		"name":"白鸽小屋",
	// 		"business_id":"03f6ab3e-00c0-d97c-a1be-6395b98645b1",
	// 		"device":[
	// 			{
	// 				"type":"weatherstations",
	// 				"dm":"代码",
	// 				"state":"正常",
	// 				"mapping":[],
	// 				"name":"水槽",
	// 				"dash":[
	// 					{"key":"weather_day","name":"24小时天气概况","description":"24小时天气概况","class":"通信","thumbnail":"/temperature.png","template":"weather_day"}
	// 				]
	// 			}
	// 		],
	// 		"two":[]
	// 	}
	// ]`
	// 解析json
	res, err := simplejson.NewJson([]byte(data))
	if err != nil {
		fmt.Println("解析出错", err)
	}
	rows, _ := res.Array()
	var asset_id string
	var asset models.Asset
	var device_id string
	var device models.Device
	var field_id string
	var field models.FieldMapping

	var asset_id2 string
	var asset2 models.Asset
	var device_id2 string
	var device2 models.Device
	var field_id2 string
	var field2 models.FieldMapping

	var asset_id3 string
	var asset3 models.Asset
	var device_id3 string
	var device3 models.Device
	var field_id3 string
	var field3 models.FieldMapping

	flag := psql.Mydb.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			if each_map, ok := row.(map[string]interface{}); ok {
				asset_id = uuid.GetUuid()
				asset = models.Asset{
					ID:         asset_id,
					Name:       each_map["name"].(string),
					Tier:       1,
					ParentID:   "0",
					BusinessID: each_map["business_id"].(string),
				}
				if err := tx.Create(&asset).Error; err != nil {
					// 回滚事务
					return err
				}
				for _, each_map2 := range each_map["device"].([]interface{}) {
					device_id = uuid.GetUuid()
					uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
					m := md5.Sum([]byte(uniqid_str))
					token := fmt.Sprintf("%x", m)
					device = models.Device{
						ID:        device_id,
						AssetID:   asset.ID,
						Token:     token,
						Type:      each_map2.(map[string]interface{})["type"].(string),
						Name:      each_map2.(map[string]interface{})["name"].(string),
						Extension: "Extensions",
					}
					if err := tx.Create(&device).Error; err != nil {
						return err
					}
					for _, each_map22 := range each_map2.(map[string]interface{})["mapping"].([]interface{}) {
						field_id = uuid.GetUuid()
						field = models.FieldMapping{
							ID:        field_id,
							DeviceID:  device.ID,
							FieldFrom: each_map22.(map[string]interface{})["field_from"].(string),
							FieldTo:   each_map22.(map[string]interface{})["field_to"].(string),
						}
						if err := tx.Create(&field).Error; err != nil {
							return err
						}
					}
				}
				for _, each_map3 := range each_map["two"].([]interface{}) {
					fmt.Println("two:", each_map3.(map[string]interface{})["name"].(string))
					asset_id2 = uuid.GetUuid()
					asset2 = models.Asset{
						ID:         asset_id2,
						Name:       each_map3.(map[string]interface{})["name"].(string),
						Tier:       2,
						ParentID:   asset_id,
						BusinessID: each_map3.(map[string]interface{})["business_id"].(string),
					}
					if err := tx.Create(&asset2).Error; err != nil {
						return err
					}
					for _, each_map33 := range each_map3.(map[string]interface{})["device"].([]interface{}) {
						device_id2 = uuid.GetUuid()
						uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
						m := md5.Sum([]byte(uniqid_str))
						token := fmt.Sprintf("%x", m)
						device2 = models.Device{
							ID:        device_id2,
							AssetID:   asset2.ID,
							Token:     token,
							Type:      each_map33.(map[string]interface{})["type"].(string),
							Name:      each_map33.(map[string]interface{})["name"].(string),
							Extension: "Extensions",
						}
						if err := tx.Create(&device2).Error; err != nil {
							return err
						}
						for _, each_map333 := range each_map33.(map[string]interface{})["mapping"].([]interface{}) {
							field_id2 = uuid.GetUuid()
							field2 = models.FieldMapping{
								ID:        field_id2,
								DeviceID:  device2.ID,
								FieldFrom: each_map333.(map[string]interface{})["field_from"].(string),
								FieldTo:   each_map333.(map[string]interface{})["field_to"].(string),
							}
							if err := tx.Create(&field2).Error; err != nil {
								return err
							}
						}
					}
					for _, each_map44 := range each_map3.(map[string]interface{})["there"].([]interface{}) {
						asset_id3 = uuid.GetUuid()
						asset3 = models.Asset{
							ID:         asset_id3,
							Name:       each_map44.(map[string]interface{})["name"].(string),
							Tier:       3,
							ParentID:   asset_id2,
							BusinessID: each_map44.(map[string]interface{})["business_id"].(string),
						}
						if err := tx.Create(&asset3).Error; err != nil {
							return err
						}
						for _, each_map444 := range each_map44.(map[string]interface{})["device"].([]interface{}) {
							device_id3 = uuid.GetUuid()
							uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
							m := md5.Sum([]byte(uniqid_str))
							token := fmt.Sprintf("%x", m)
							device3 = models.Device{
								ID:        device_id3,
								AssetID:   asset3.ID,
								Token:     token,
								Type:      each_map444.(map[string]interface{})["type"].(string),
								Name:      each_map444.(map[string]interface{})["name"].(string),
								Extension: "Extensions",
							}
							if err := tx.Create(&device3).Error; err != nil {
								return err
							}
							for _, each_map4444 := range each_map444.(map[string]interface{})["mapping"].([]interface{}) {
								field_id3 = uuid.GetUuid()
								field3 = models.FieldMapping{
									ID:        field_id3,
									DeviceID:  device3.ID,
									FieldFrom: each_map4444.(map[string]interface{})["field_from"].(string),
									FieldTo:   each_map4444.(map[string]interface{})["field_to"].(string),
								}
								if err := tx.Create(&field3).Error; err != nil {
									return err
								}
							}
						}
					}
				}
			}
		}
		return nil
	})

	if flag != nil {
		return false
	}
	return true
}

// edit 编辑资产
func (*AssetService) Edit(data string) bool {
	// 解析json
	// var data string = `[
	// 	{
	// 		"id":"ea9ebb6c-deba-3640-a103-cb4d0baa9866",
	// 		"name":"白鸽小屋",
	// 		"customer_id":"",
	// 		"business_id":"03f6ab3e-00c0-d97c-a1be-6395b98645b1",
	// 		"widget_id":"",
	// 		"widget_name":"",
	// 		"device":[
	// 			{
	// 				"id":"6af67cb2-0113-a4e1-fb15-7c0724eadb29",
	// 				"name":"水槽",
	// 				"type":"weatherstations",
	// 				"disabled":true,
	// 				"dm":"代码",
	// 				"state":"正常",
	// 				"dash":[
	// 					{"key":"air_quality","name":"空气质量","description":"空气质量","class":"通信","thumbnail":"/temperature.png","template":"air_quality"}
	// 				],
	// 				"mapping":[
	// 					{
	// 						"field_from":"A","field_to":"weather","btnname":"新增","btncolor":"primary","btnevent":"add"
	// 					}
	// 				]
	// 			}
	// 		],
	// 		"two":null
	// 	}
	// ]`
	res, err := simplejson.NewJson([]byte(data))
	if err != nil {
		fmt.Println("解析出错", err)
	}
	rows, _ := res.Array()
	var asset_id string
	var asset models.Asset
	var device_id string
	var device models.Device
	var field_id string
	var field models.FieldMapping

	var asset_id2 string
	var asset2 models.Asset
	var device_id2 string
	var device2 models.Device
	var field_id2 string
	var field2 models.FieldMapping

	var asset_id3 string
	var asset3 models.Asset
	var device_id3 string
	var device3 models.Device
	var field_id3 string
	var field3 models.FieldMapping

	flag := psql.Mydb.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			if each_map, ok := row.(map[string]interface{}); ok {
				if _, exists := each_map["id"]; exists {
					// 修改
					asset_id = each_map["id"].(string)
					err := tx.Model(&models.Asset{}).Where("id = ?", asset_id).Updates(map[string]interface{}{
						"name":        each_map["name"].(string),
						"business_id": each_map["business_id"].(string),
					}).Error
					if err != nil {
						return err
					}
				} else {
					// 新增
					asset_id = uuid.GetUuid()
					asset = models.Asset{
						ID:         asset_id,
						Name:       each_map["name"].(string),
						Tier:       1,
						ParentID:   "0",
						BusinessID: each_map["business_id"].(string),
					}
					if err := tx.Create(&asset).Error; err != nil {
						// 回滚事务
						return err
					}
				}
				if each_map["device"] != nil {
					for _, each_map2 := range each_map["device"].([]interface{}) {
						if _, exists := each_map2.(map[string]interface{})["id"]; exists {
							// 修改
							device_id = each_map2.(map[string]interface{})["id"].(string)
							err := tx.Model(&models.Device{}).Where("id = ?", device_id).Updates(map[string]interface{}{
								"asset_id": asset_id,
								"type":     each_map2.(map[string]interface{})["type"].(string),
								"name":     each_map2.(map[string]interface{})["name"].(string),
							}).Error
							if err != nil {
								return err
							}
						} else {
							// 新增
							device_id = uuid.GetUuid()
							uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
							m := md5.Sum([]byte(uniqid_str))
							token := fmt.Sprintf("%x", m)
							device = models.Device{
								ID:        device_id,
								AssetID:   asset_id,
								Token:     token,
								Type:      each_map2.(map[string]interface{})["type"].(string),
								Name:      each_map2.(map[string]interface{})["name"].(string),
								Extension: "Extensions",
							}
							if err := tx.Create(&device).Error; err != nil {
								return err
							}
						}
						if each_map2.(map[string]interface{})["mapping"] != nil {
							for _, each_map22 := range each_map2.(map[string]interface{})["mapping"].([]interface{}) {
								if _, exists := each_map22.(map[string]interface{})["id"]; exists {
									// 修改
									err := tx.Model(&models.FieldMapping{}).Where("id = ?", each_map22.(map[string]interface{})["id"].(string)).Updates(map[string]interface{}{
										"device_id":  device_id,
										"field_from": each_map22.(map[string]interface{})["field_from"].(string),
										"field_to":   each_map22.(map[string]interface{})["field_to"].(string),
									}).Error
									if err != nil {
										return err
									}
								} else {
									// 新增
									field_id = uuid.GetUuid()
									field = models.FieldMapping{
										ID:        field_id,
										DeviceID:  device_id,
										FieldFrom: each_map22.(map[string]interface{})["field_from"].(string),
										FieldTo:   each_map22.(map[string]interface{})["field_to"].(string),
									}
									if err := tx.Create(&field).Error; err != nil {
										return err
									}
								}
							}
						}
					}
					if each_map["two"] != nil {
						for _, each_map3 := range each_map["two"].([]interface{}) {
							if _, exists := each_map3.(map[string]interface{})["id"]; exists {
								// 修改
								asset_id2 = each_map3.(map[string]interface{})["id"].(string)
								err := tx.Model(&models.Asset{}).Where("id = ?", asset_id2).Updates(map[string]interface{}{
									"name":        each_map3.(map[string]interface{})["name"].(string),
									"parent_id":   asset_id,
									"business_id": each_map3.(map[string]interface{})["business_id"].(string),
								}).Error
								if err != nil {
									return err
								}
							} else {
								// 新增
								asset_id2 = uuid.GetUuid()
								asset2 = models.Asset{
									ID:         asset_id2,
									Name:       each_map3.(map[string]interface{})["name"].(string),
									Tier:       2,
									ParentID:   asset_id,
									BusinessID: each_map3.(map[string]interface{})["business_id"].(string),
								}
								if err := tx.Create(&asset2).Error; err != nil {
									return err
								}
							}
							if each_map3.(map[string]interface{})["device"] != nil {
								for _, each_map33 := range each_map3.(map[string]interface{})["device"].([]interface{}) {
									if _, exists := each_map33.(map[string]interface{})["id"]; exists {
										// 修改
										device_id2 = each_map33.(map[string]interface{})["id"].(string)
										err := tx.Model(&models.Device{}).Where("id = ?", device_id2).Updates(map[string]interface{}{
											"asset_id": asset_id2,
											"type":     each_map33.(map[string]interface{})["type"].(string),
											"name":     each_map33.(map[string]interface{})["name"].(string),
										}).Error
										if err != nil {
											return err
										}
									} else {
										// 新增
										device_id2 = uuid.GetUuid()
										uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
										m := md5.Sum([]byte(uniqid_str))
										token := fmt.Sprintf("%x", m)
										device2 = models.Device{
											ID:        device_id2,
											AssetID:   asset_id2,
											Token:     token,
											Type:      each_map33.(map[string]interface{})["type"].(string),
											Name:      each_map33.(map[string]interface{})["name"].(string),
											Extension: "Extensions",
										}
										if err := tx.Create(&device2).Error; err != nil {
											return err
										}
									}
									for _, each_map333 := range each_map33.(map[string]interface{})["mapping"].([]interface{}) {
										if _, exists := each_map333.(map[string]interface{})["id"]; exists {
											// 修改
											err := tx.Model(&models.FieldMapping{}).Where("id = ?", each_map333.(map[string]interface{})["id"].(string)).Updates(map[string]interface{}{
												"device_id":  device_id2,
												"field_from": each_map333.(map[string]interface{})["field_from"].(string),
												"field_to":   each_map333.(map[string]interface{})["field_to"].(string),
											}).Error
											if err != nil {
												return err
											}
										} else {
											// 新增
											field_id2 = uuid.GetUuid()
											field2 = models.FieldMapping{
												ID:        field_id2,
												DeviceID:  device_id2,
												FieldFrom: each_map333.(map[string]interface{})["field_from"].(string),
												FieldTo:   each_map333.(map[string]interface{})["field_to"].(string),
											}
											if err := tx.Create(&field2).Error; err != nil {
												return err
											}
										}
									}
								}
							}
							if each_map3.(map[string]interface{})["there"] != nil {
								for _, each_map44 := range each_map3.(map[string]interface{})["there"].([]interface{}) {
									if _, exists := each_map44.(map[string]interface{})["id"]; exists {
										// 修改
										asset_id3 = each_map44.(map[string]interface{})["id"].(string)
										err := tx.Model(&models.Asset{}).Where("id = ?", asset_id3).Updates(map[string]interface{}{
											"name":        each_map44.(map[string]interface{})["name"].(string),
											"parent_id":   asset_id2,
											"business_id": each_map44.(map[string]interface{})["business_id"].(string),
										}).Error
										if err != nil {
											return err
										}
									} else {
										// 新增
										asset_id3 = uuid.GetUuid()
										asset3 = models.Asset{
											ID:         asset_id3,
											Name:       each_map44.(map[string]interface{})["name"].(string),
											Tier:       3,
											ParentID:   asset_id2,
											BusinessID: each_map44.(map[string]interface{})["business_id"].(string),
										}
										if err := tx.Create(&asset3).Error; err != nil {
											return err
										}
									}
									if each_map44.(map[string]interface{})["device"] != nil {
										for _, each_map444 := range each_map44.(map[string]interface{})["device"].([]interface{}) {
											if _, exists := each_map444.(map[string]interface{})["id"]; exists {
												// 修改
												device_id3 = each_map444.(map[string]interface{})["id"].(string)
												err := tx.Model(&models.Device{}).Where("id = ?", device_id3).Updates(map[string]interface{}{
													"asset_id": asset_id3,
													"type":     each_map444.(map[string]interface{})["type"].(string),
													"name":     each_map444.(map[string]interface{})["name"].(string),
												}).Error
												if err != nil {
													return err
												}
											} else {
												// 新增
												device_id3 = uuid.GetUuid()
												uniqid_str := uniqid.New(uniqid.Params{Prefix: "token", MoreEntropy: true})
												m := md5.Sum([]byte(uniqid_str))
												token := fmt.Sprintf("%x", m)
												device3 = models.Device{
													ID:        device_id3,
													AssetID:   asset_id3,
													Token:     token,
													Type:      each_map444.(map[string]interface{})["type"].(string),
													Name:      each_map444.(map[string]interface{})["name"].(string),
													Extension: "Extensions",
												}
												if err := tx.Create(&device3).Error; err != nil {
													return err
												}
											}
											if each_map444.(map[string]interface{})["mapping"] != nil {
												for _, each_map4444 := range each_map444.(map[string]interface{})["mapping"].([]interface{}) {
													if _, exists := each_map4444.(map[string]interface{})["id"]; exists {
														// 修改
														err := tx.Model(&models.FieldMapping{}).Where("id = ?", each_map4444.(map[string]interface{})["id"].(string)).Updates(map[string]interface{}{
															"device_id":  device_id3,
															"field_from": each_map4444.(map[string]interface{})["field_from"].(string),
															"field_to":   each_map4444.(map[string]interface{})["field_to"].(string),
														}).Error
														if err != nil {
															return err
														}
													} else {
														// 新增
														field_id3 = uuid.GetUuid()
														field3 = models.FieldMapping{
															ID:        field_id3,
															DeviceID:  device_id3,
															FieldFrom: each_map4444.(map[string]interface{})["field_from"].(string),
															FieldTo:   each_map4444.(map[string]interface{})["field_to"].(string),
														}
														if err := tx.Create(&field3).Error; err != nil {
															return err
														}
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
		return nil
	})
	if flag != nil {
		return false
	}
	return true
}

// 根据ID删除一条asset数据
func (*AssetService) Delete(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.Asset{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	} else {
		return true
	}
}

// 根据ID获取下级资产
func (*AssetService) GetAssetsByParentID(parent_id string) ([]models.Asset, int64) {
	var assets []models.Asset
	var count int64
	result := psql.Mydb.Model(&models.Asset{}).Where("parent_id = ?", parent_id).Find(&assets)
	psql.Mydb.Model(&models.Asset{}).Where("parent_id = ?", parent_id).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return assets, count

}

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
						Type:        e["type"].(string),
						Name:        e["name"].(string),
						Description: e["description"].(string),
						Version:     e["version"].(string),
						Author:      e["author"].(string),
						Email:       e["email"].(string),
					}
					es = append(es, i)
				}
			}
		}
	}
	return es
}

// widget 获取组件
func (*AssetService) Widget(id string) []Widget {
	var w []Widget
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
								Name:        item["name"].(string),
								Description: item["description"].(string),
								Class:       item["class"].(string),
								Thumbnail:   item["thumbnail"].(string),
								Template:    item["template"].(string),
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
								Name:        item["name"].(string),
								Description: item["description"].(string),
								Class:       item["class"].(string),
								Thumbnail:   item["thumbnail"].(string),
								Template:    item["template"].(string),
							}
							w = append(w, i)
						}
					}
				}
			}
		}
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
											fin = fieldItem["name"].(string)
										}
										if fieldItem["type"] == nil {
											fit = 0
										} else {
											fit = fieldItem["type"].(int64)
										}
										if fieldItem["symbol"] == nil {
											fis = ""
										} else {
											fis = fieldItem["symbol"].(string)
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
	return w
}

// GetAsset
func (*AssetService) GetAssetByBusinessId(business_id string) ([]AssetList, int64) {
	var assets []AssetList
	var count int64
	result := psql.Mydb.Model(&models.Asset{}).Where("business_id = ? AND parent_id='0'", business_id).Find(&assets)
	psql.Mydb.Model(&models.Asset{}).Where("business_id = ? AND parent_id='0'", business_id).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return assets, count
}

// GetAssetDataByBusinessId
func (*AssetService) GetAssetDataByBusinessId(business_id string) ([]AssetList, int64) {
	var assets []AssetList
	var count int64
	result := psql.Mydb.Model(&models.Asset{}).Where("business_id = ?", business_id).Find(&assets)
	psql.Mydb.Model(&models.Asset{}).Where("business_id = ?", business_id).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return assets, count
}

// 设备数据
func (*AssetService) GetAssetData(business_id string) ([]models.Asset, int64) {
	var assets []models.Asset
	var count int64
	result := psql.Mydb.Model(&models.Asset{}).Where("business_id = ? AND tier=1", business_id).Find(&assets)
	psql.Mydb.Model(&models.Asset{}).Where("business_id = ? AND tier=1", business_id).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return assets, count
}

// 获取全部Asset
func (*AssetService) All() ([]models.Asset, int64) {
	var assets []models.Asset
	result := psql.Mydb.Find(&assets)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return assets, result.RowsAffected
}
