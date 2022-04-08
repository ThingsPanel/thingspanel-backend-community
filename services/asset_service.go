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
func (*AssetService) Add(data string) bool {
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
					Name:       fmt.Sprint(each_map["name"]),
					Tier:       1,
					ParentID:   "0",
					BusinessID: fmt.Sprint(each_map["business_id"]),
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
						Type:      fmt.Sprint(each_map2.(map[string]interface{})["type"]),
						Name:      fmt.Sprint(each_map2.(map[string]interface{})["name"]),
						Protocol:  fmt.Sprint(each_map2.(map[string]interface{})["protocol"]),
						Port:      fmt.Sprint(each_map2.(map[string]interface{})["port"]),
						Publish:   fmt.Sprint(each_map2.(map[string]interface{})["publish"]),
						Subscribe: fmt.Sprint(each_map2.(map[string]interface{})["subscribe"]),
						Username:  fmt.Sprint(each_map2.(map[string]interface{})["username"]),
						Password:  fmt.Sprint(each_map2.(map[string]interface{})["password"]),
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
							FieldFrom: fmt.Sprint(each_map22.(map[string]interface{})["field_from"]),
							FieldTo:   fmt.Sprint(each_map22.(map[string]interface{})["field_to"]),
							Symbol:    fmt.Sprint(each_map22.(map[string]interface{})["symbol"]),
						}
						if err := tx.Create(&field).Error; err != nil {
							return err
						}
					}
				}
				for _, each_map3 := range each_map["two"].([]interface{}) {
					asset_id2 = uuid.GetUuid()
					asset2 = models.Asset{
						ID:         asset_id2,
						Name:       fmt.Sprint(each_map3.(map[string]interface{})["name"]),
						Tier:       2,
						ParentID:   asset_id,
						BusinessID: fmt.Sprint(each_map3.(map[string]interface{})["business_id"]),
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
							Type:      fmt.Sprint(each_map33.(map[string]interface{})["type"]),
							Name:      fmt.Sprint(each_map33.(map[string]interface{})["name"]),
							Protocol:  fmt.Sprint(each_map33.(map[string]interface{})["protocol"]),
							Port:      fmt.Sprint(each_map33.(map[string]interface{})["port"]),
							Publish:   fmt.Sprint(each_map33.(map[string]interface{})["publish"]),
							Subscribe: fmt.Sprint(each_map33.(map[string]interface{})["subscribe"]),
							Username:  fmt.Sprint(each_map33.(map[string]interface{})["username"]),
							Password:  fmt.Sprint(each_map33.(map[string]interface{})["password"]),
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
								FieldFrom: fmt.Sprint(each_map333.(map[string]interface{})["field_from"]),
								FieldTo:   fmt.Sprint(each_map333.(map[string]interface{})["field_to"]),
								Symbol:    fmt.Sprint(each_map333.(map[string]interface{})["symbol"]),
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
							Name:       fmt.Sprint(each_map44.(map[string]interface{})["name"]),
							Tier:       3,
							ParentID:   asset_id2,
							BusinessID: fmt.Sprint(each_map44.(map[string]interface{})["business_id"]),
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
								Type:      fmt.Sprint(each_map444.(map[string]interface{})["type"]),
								Name:      fmt.Sprint(each_map444.(map[string]interface{})["name"]),
								Protocol:  fmt.Sprint(each_map444.(map[string]interface{})["protocol"]),
								Port:      fmt.Sprint(each_map444.(map[string]interface{})["port"]),
								Publish:   fmt.Sprint(each_map444.(map[string]interface{})["publish"]),
								Subscribe: fmt.Sprint(each_map444.(map[string]interface{})["subscribe"]),
								Username:  fmt.Sprint(each_map444.(map[string]interface{})["username"]),
								Password:  fmt.Sprint(each_map444.(map[string]interface{})["password"]),
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
									FieldFrom: fmt.Sprint(each_map4444.(map[string]interface{})["field_from"]),
									FieldTo:   fmt.Sprint(each_map4444.(map[string]interface{})["field_to"]),
									Symbol:    fmt.Sprint(each_map4444.(map[string]interface{})["symbol"]),
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
				if _, exists := each_map["id"]; exists && fmt.Sprint(each_map["id"]) != "0" {
					// 修改
					asset_id = fmt.Sprint(each_map["id"])
					err := tx.Model(&models.Asset{}).Where("id = ?", asset_id).Updates(map[string]interface{}{
						"name":        fmt.Sprint(each_map["name"]),
						"business_id": fmt.Sprint(each_map["business_id"]),
					}).Error
					if err != nil {
						return err
					}
				} else {
					// 新增
					asset_id = uuid.GetUuid()
					asset = models.Asset{
						ID:         asset_id,
						Name:       fmt.Sprint(each_map["name"]),
						Tier:       1,
						ParentID:   "0",
						BusinessID: fmt.Sprint(each_map["business_id"]),
					}
					if err := tx.Create(&asset).Error; err != nil {
						// 回滚事务
						return err
					}
				}
				if each_map["device"] != nil {
					for _, each_map2 := range each_map["device"].([]interface{}) {
						if _, exists := each_map2.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map2.(map[string]interface{})["id"]) != "0" {
							// 修改
							device_id = fmt.Sprint(each_map2.(map[string]interface{})["id"])
							err := tx.Model(&models.Device{}).Where("id = ?", device_id).Updates(map[string]interface{}{
								"asset_id":  asset_id,
								"type":      fmt.Sprint(each_map2.(map[string]interface{})["type"]),
								"name":      fmt.Sprint(each_map2.(map[string]interface{})["name"]),
								"protocol":  fmt.Sprint(each_map2.(map[string]interface{})["protocol"]),
								"port":      fmt.Sprint(each_map2.(map[string]interface{})["port"]),
								"publish":   fmt.Sprint(each_map2.(map[string]interface{})["publish"]),
								"subscribe": fmt.Sprint(each_map2.(map[string]interface{})["subscribe"]),
								"username":  fmt.Sprint(each_map2.(map[string]interface{})["username"]),
								"password":  fmt.Sprint(each_map2.(map[string]interface{})["password"]),
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
								Type:      fmt.Sprint(each_map2.(map[string]interface{})["type"]),
								Name:      fmt.Sprint(each_map2.(map[string]interface{})["name"]),
								Protocol:  fmt.Sprint(each_map2.(map[string]interface{})["protocol"]),
								Port:      fmt.Sprint(each_map2.(map[string]interface{})["port"]),
								Publish:   fmt.Sprint(each_map2.(map[string]interface{})["publish"]),
								Subscribe: fmt.Sprint(each_map2.(map[string]interface{})["subscribe"]),
								Username:  fmt.Sprint(each_map2.(map[string]interface{})["username"]),
								Password:  fmt.Sprint(each_map2.(map[string]interface{})["password"]),
								Extension: "Extensions",
							}
							if err := tx.Create(&device).Error; err != nil {
								return err
							}
						}
						if each_map2.(map[string]interface{})["mapping"] != nil {
							for _, each_map22 := range each_map2.(map[string]interface{})["mapping"].([]interface{}) {
								if _, exists := each_map22.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map22.(map[string]interface{})["id"]) != "0" {
									// 修改
									err := tx.Model(&models.FieldMapping{}).Where("id = ?", fmt.Sprint(each_map22.(map[string]interface{})["id"])).Updates(map[string]interface{}{
										"device_id":  device_id,
										"field_from": fmt.Sprint(each_map22.(map[string]interface{})["field_from"]),
										"field_to":   fmt.Sprint(each_map22.(map[string]interface{})["field_to"]),
										"symbol":     fmt.Sprint(each_map22.(map[string]interface{})["symbol"]),
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
										FieldFrom: fmt.Sprint(each_map22.(map[string]interface{})["field_from"]),
										FieldTo:   fmt.Sprint(each_map22.(map[string]interface{})["field_to"]),
										Symbol:    fmt.Sprint(each_map22.(map[string]interface{})["symbol"]),
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
							if _, exists := each_map3.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map3.(map[string]interface{})["id"]) != "0" {
								// 修改
								asset_id2 = fmt.Sprint(each_map3.(map[string]interface{})["id"])
								err := tx.Model(&models.Asset{}).Where("id = ?", asset_id2).Updates(map[string]interface{}{
									"name":        fmt.Sprint(each_map3.(map[string]interface{})["name"]),
									"parent_id":   asset_id,
									"business_id": fmt.Sprint(each_map3.(map[string]interface{})["business_id"]),
								}).Error
								if err != nil {
									return err
								}
							} else {
								// 新增
								asset_id2 = uuid.GetUuid()
								asset2 = models.Asset{
									ID:         asset_id2,
									Name:       fmt.Sprint(each_map3.(map[string]interface{})["name"]),
									Tier:       2,
									ParentID:   asset_id,
									BusinessID: fmt.Sprint(each_map3.(map[string]interface{})["business_id"]),
								}
								if err := tx.Create(&asset2).Error; err != nil {
									return err
								}
							}
							if each_map3.(map[string]interface{})["device"] != nil {
								for _, each_map33 := range each_map3.(map[string]interface{})["device"].([]interface{}) {
									if _, exists := each_map33.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map33.(map[string]interface{})["id"]) != "0" {
										// 修改
										device_id2 = fmt.Sprint(each_map33.(map[string]interface{})["id"])
										err := tx.Model(&models.Device{}).Where("id = ?", device_id2).Updates(map[string]interface{}{
											"asset_id":  asset_id2,
											"type":      fmt.Sprint(each_map33.(map[string]interface{})["type"]),
											"name":      fmt.Sprint(each_map33.(map[string]interface{})["name"]),
											"protocol":  fmt.Sprint(each_map33.(map[string]interface{})["protocol"]),
											"port":      fmt.Sprint(each_map33.(map[string]interface{})["port"]),
											"publish":   fmt.Sprint(each_map33.(map[string]interface{})["publish"]),
											"subscribe": fmt.Sprint(each_map33.(map[string]interface{})["subscribe"]),
											"username":  fmt.Sprint(each_map33.(map[string]interface{})["username"]),
											"password":  fmt.Sprint(each_map33.(map[string]interface{})["password"]),
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
											Type:      fmt.Sprint(each_map33.(map[string]interface{})["type"]),
											Name:      fmt.Sprint(each_map33.(map[string]interface{})["name"]),
											Protocol:  fmt.Sprint(each_map33.(map[string]interface{})["protocol"]),
											Port:      fmt.Sprint(each_map33.(map[string]interface{})["port"]),
											Publish:   fmt.Sprint(each_map33.(map[string]interface{})["publish"]),
											Subscribe: fmt.Sprint(each_map33.(map[string]interface{})["subscribe"]),
											Username:  fmt.Sprint(each_map33.(map[string]interface{})["username"]),
											Password:  fmt.Sprint(each_map33.(map[string]interface{})["password"]),
											Extension: "Extensions",
										}
										if err := tx.Create(&device2).Error; err != nil {
											return err
										}
									}
									for _, each_map333 := range each_map33.(map[string]interface{})["mapping"].([]interface{}) {
										if _, exists := each_map333.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map333.(map[string]interface{})["id"]) != "0" {
											// 修改
											err := tx.Model(&models.FieldMapping{}).Where("id = ?", fmt.Sprint(each_map333.(map[string]interface{})["id"])).Updates(map[string]interface{}{
												"device_id":  device_id2,
												"field_from": fmt.Sprint(each_map333.(map[string]interface{})["field_from"]),
												"field_to":   fmt.Sprint(each_map333.(map[string]interface{})["field_to"]),
												"symbol":     fmt.Sprint(each_map333.(map[string]interface{})["symbol"]),
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
												FieldFrom: fmt.Sprint(each_map333.(map[string]interface{})["field_from"]),
												FieldTo:   fmt.Sprint(each_map333.(map[string]interface{})["field_to"]),
												Symbol:    fmt.Sprint(each_map333.(map[string]interface{})["symbol"]),
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
									if _, exists := each_map44.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map44.(map[string]interface{})["id"]) != "0" {
										// 修改
										asset_id3 = fmt.Sprint(each_map44.(map[string]interface{})["id"])
										err := tx.Model(&models.Asset{}).Where("id = ?", asset_id3).Updates(map[string]interface{}{
											"name":        fmt.Sprint(each_map44.(map[string]interface{})["name"]),
											"parent_id":   asset_id2,
											"business_id": fmt.Sprint(each_map44.(map[string]interface{})["business_id"]),
										}).Error
										if err != nil {
											return err
										}
									} else {
										// 新增
										asset_id3 = uuid.GetUuid()
										asset3 = models.Asset{
											ID:         asset_id3,
											Name:       fmt.Sprint(each_map44.(map[string]interface{})["name"]),
											Tier:       3,
											ParentID:   asset_id2,
											BusinessID: fmt.Sprint(each_map44.(map[string]interface{})["business_id"]),
										}
										if err := tx.Create(&asset3).Error; err != nil {
											return err
										}
									}
									if each_map44.(map[string]interface{})["device"] != nil {
										for _, each_map444 := range each_map44.(map[string]interface{})["device"].([]interface{}) {
											if _, exists := each_map444.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map444.(map[string]interface{})["id"]) != "0" {
												// 修改
												device_id3 = fmt.Sprint(each_map444.(map[string]interface{})["id"])
												err := tx.Model(&models.Device{}).Where("id = ?", device_id3).Updates(map[string]interface{}{
													"asset_id":  asset_id3,
													"type":      fmt.Sprint(each_map444.(map[string]interface{})["type"]),
													"name":      fmt.Sprint(each_map444.(map[string]interface{})["name"]),
													"protocol":  fmt.Sprint(each_map444.(map[string]interface{})["protocol"]),
													"port":      fmt.Sprint(each_map444.(map[string]interface{})["port"]),
													"publish":   fmt.Sprint(each_map444.(map[string]interface{})["publish"]),
													"subscribe": fmt.Sprint(each_map444.(map[string]interface{})["subscribe"]),
													"username":  fmt.Sprint(each_map444.(map[string]interface{})["username"]),
													"password":  fmt.Sprint(each_map444.(map[string]interface{})["password"]),
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
													Type:      fmt.Sprint(each_map444.(map[string]interface{})["type"]),
													Name:      fmt.Sprint(each_map444.(map[string]interface{})["name"]),
													Protocol:  fmt.Sprint(each_map444.(map[string]interface{})["protocol"]),
													Port:      fmt.Sprint(each_map444.(map[string]interface{})["port"]),
													Publish:   fmt.Sprint(each_map444.(map[string]interface{})["publish"]),
													Subscribe: fmt.Sprint(each_map444.(map[string]interface{})["subscribe"]),
													Username:  fmt.Sprint(each_map444.(map[string]interface{})["username"]),
													Password:  fmt.Sprint(each_map444.(map[string]interface{})["password"]),
													Extension: "Extensions",
												}
												if err := tx.Create(&device3).Error; err != nil {
													return err
												}
											}
											if each_map444.(map[string]interface{})["mapping"] != nil {
												for _, each_map4444 := range each_map444.(map[string]interface{})["mapping"].([]interface{}) {
													if _, exists := each_map4444.(map[string]interface{})["id"]; exists && fmt.Sprint(each_map4444.(map[string]interface{})["id"]) != "0" {
														// 修改
														err := tx.Model(&models.FieldMapping{}).Where("id = ?", fmt.Sprint(each_map4444.(map[string]interface{})["id"])).Updates(map[string]interface{}{
															"device_id":  device_id3,
															"field_from": fmt.Sprint(each_map4444.(map[string]interface{})["field_from"]),
															"field_to":   fmt.Sprint(each_map4444.(map[string]interface{})["field_to"]),
															"symbol":     fmt.Sprint(each_map4444.(map[string]interface{})["symbol"]),
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
															FieldFrom: fmt.Sprint(each_map4444.(map[string]interface{})["field_from"]),
															FieldTo:   fmt.Sprint(each_map4444.(map[string]interface{})["field_to"]),
															Symbol:    fmt.Sprint(each_map4444.(map[string]interface{})["symbol"]),
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
	if len(assets) == 0 {
		assets = []models.Asset{}
	}
	return assets, count
}

func (*AssetService) GetAssetsByTierAndBusinessID(business_id string) ([]models.Asset, int64) {
	var assets []models.Asset
	var count int64
	result := psql.Mydb.Model(&models.Asset{}).Where("tier=1 AND business_id = ?", business_id).Find(&assets)
	psql.Mydb.Model(&models.Asset{}).Where("tier=1 AND business_id = ?", business_id).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(assets) == 0 {
		assets = []models.Asset{}
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
											fit = fieldItem["type"].(int64)
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

// GetAsset
func (*AssetService) GetAssetByBusinessId(business_id string) ([]AssetList, int64) {
	var assets []AssetList
	var count int64
	result := psql.Mydb.Model(&models.Asset{}).Where("business_id = ? AND parent_id='0'", business_id).Find(&assets)
	psql.Mydb.Model(&models.Asset{}).Where("business_id = ? AND parent_id='0'", business_id).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(assets) == 0 {
		assets = []AssetList{}
	}
	return assets, count
}

// GetAssetDataByBusinessId
func (*AssetService) GetAssetDataByBusinessId(business_id string) (assets []AssetList, err error) {
	err = psql.Mydb.Model(&models.Asset{}).Where("business_id = ?", business_id).Find(&assets).Error
	if err != nil {
		return assets, err
	}
	return assets, err
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
	if len(assets) == 0 {
		assets = []models.Asset{}
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
	if len(assets) == 0 {
		assets = []models.Asset{}
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
