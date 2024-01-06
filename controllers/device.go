package controllers

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	tphttp "ThingsPanel-Go/others/http"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"gorm.io/gorm"
)

type DeviceController struct {
	beego.Controller
}

// 设备列表
func (this *DeviceController) Index() {
	this.Data["json"] = "test devices"
	this.ServeJSON()
}

// 设备列表
func (this *DeviceController) Edit() {
	editDeviceValidate := valid.EditDevice{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &editDeviceValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(editDeviceValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(editDeviceValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var count int64
	tokenResult := psql.Mydb.Model(&models.Device{}).Where("token = ?", editDeviceValidate.Token).Count(&count)
	if tokenResult.Error != nil {
		errors.Is(tokenResult.Error, gorm.ErrRecordNotFound)
		response.SuccessWithMessage(400, "编辑失败", (*context2.Context)(this.Ctx))
		return
	} else {
		if count > 0 {
			response.SuccessWithMessage(400, "设备token已存在，请删除对应设备后再来添加！", (*context2.Context)(this.Ctx))
			return
		}
	}
	var DeviceService services.DeviceService
	e_err := DeviceService.Edit(editDeviceValidate)
	if e_err == nil {
		response.SuccessWithMessage(200, "编辑成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, e_err.Error(), (*context2.Context)(this.Ctx))
	return
}

// 废弃
func (this *DeviceController) Add() {
	addDeviceValidate := valid.Device{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &addDeviceValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(addDeviceValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(addDeviceValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	deviceData := models.Device{
		AssetID:        addDeviceValidate.AssetID,
		Token:          addDeviceValidate.Token,
		AdditionalInfo: addDeviceValidate.AdditionalInfo,
		Type:           addDeviceValidate.Type,
		Name:           addDeviceValidate.Name,
		Label:          addDeviceValidate.Label,
		SearchText:     addDeviceValidate.SearchText,
		ChartOption:    "{}",
		Protocol:       addDeviceValidate.Protocol,
		Port:           addDeviceValidate.Port,
		Publish:        addDeviceValidate.Publish,
		Subscribe:      addDeviceValidate.Subscribe,
		Username:       addDeviceValidate.Username,
		Password:       addDeviceValidate.Password,
		DId:            addDeviceValidate.DId,
		Location:       addDeviceValidate.Location,
		DeviceType:     addDeviceValidate.DeviceType,
		ParentId:       addDeviceValidate.ParentId,
		ProtocolConfig: addDeviceValidate.ProtocolConfig,
		CreatedAt:      time.Now().Unix(),
	}
	_, err = DeviceService.Add(deviceData)
	if err == nil {
		response.SuccessWithMessage(200, "添加成功", (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, err.Error(), (*context2.Context)(this.Ctx))
	return
}

type DeviceDash struct {
	ID             string            `json:"id" gorm:"primaryKey,size:36"`
	AssetID        string            `json:"asset_id" gorm:"size:36"`              // 资产id
	Token          string            `json:"token,omitempty"`                      // 安全key
	AdditionalInfo string            `json:"additional_info" gorm:"type:longtext"` // 存储基本配置
	CustomerID     string            `json:"customer_id" gorm:"size:36"`
	Type           string            `json:"type"` // 插件类型
	Name           string            `json:"name"` // 插件名
	Label          string            `json:"label"`
	SearchText     string            `json:"search_text"`
	ChartOption    string            `json:"chart_option"`
	Protocol       string            `json:"protocol" gorm:"size:50"`
	Port           string            `json:"port" gorm:"size:50"`
	Publish        string            `json:"publish" gorm:"size:255"`
	Subscribe      string            `json:"subscribe" gorm:"size:255"`
	Username       string            `json:"username" gorm:"size:255"`
	Password       string            `json:"password" gorm:"size:255"`
	DId            string            `json:"d_id" gorm:"size:255"`
	Location       string            `json:"location" gorm:"size:255"`
	DeviceType     string            `json:"device_type,omitempty" gorm:"size:2"`
	ParentId       string            `json:"parent_id,omitempty" gorm:"size:36"`
	ProtocolConfig string            `json:"protocol_config,omitempty" gorm:"type:longtext"`
	SubDeviceAddr  string            `json:"sub_device_addr,omitempty" gorm:"size:36"`
	ScriptId       string            `json:"script_id,omitempty" gorm:"size:36"`
	Dash           []services.Widget `json:"dash"`
}

func (c *DeviceController) AddOnly() {
	reqData := valid.Device{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DeviceService services.DeviceService
	if reqData.Token == "" {
		var uuid_d = uuid.GetUuid()
		reqData.Token = uuid_d
	}
	deviceData := models.Device{
		AssetID:        reqData.AssetID,
		Token:          reqData.Token,
		AdditionalInfo: reqData.AdditionalInfo,
		Type:           reqData.Type,
		Name:           reqData.Name,
		Label:          reqData.Label,
		SearchText:     reqData.SearchText,
		Protocol:       reqData.Protocol,
		Port:           reqData.Port,
		Publish:        reqData.Publish,
		Subscribe:      reqData.Subscribe,
		Username:       reqData.Username,
		Password:       reqData.Password,
		DId:            reqData.DId,
		Location:       reqData.Location,
		DeviceType:     reqData.DeviceType,
		ParentId:       reqData.ParentId,
		ProtocolConfig: reqData.ProtocolConfig,
		ScriptId:       reqData.ScriptId,
		TenantId:       tenantId,
	}
	if deviceData.ChartOption == "" {
		deviceData.ChartOption = "{}"
	}
	if deviceData.ProtocolConfig == "" {
		deviceData.ProtocolConfig = "{}"
	}
	if deviceData.DeviceType == "3" {
		deviceData.SubDeviceAddr = strings.Replace(uuid.GetUuid(), "-", "", -1)[0:9]
		//设置离线阈值为60
		deviceData.AdditionalInfo = `{"runningInfo":{"thresholdTime":60}}`
	}
	uuid, err := DeviceService.Add(deviceData)
	//result := psql.Mydb.Create(&deviceData)
	if err == nil {
		deviceData.ID = uuid
		// 判断是否是协议插件的网关子设备
		if deviceData.DeviceType == "3" && deviceData.Protocol != "MQTT" {
			// 通知插件子设备配置已修改
			var reqmap = make(map[string]interface{})
			reqmap["DeviceType"] = deviceData.DeviceType
			reqmap["ParentId"] = deviceData.ParentId
			reqmap["DeviceId"] = deviceData.ID
			var protocol_config_map = make(map[string]interface{})
			j_err := json.Unmarshal([]byte(deviceData.ProtocolConfig), &protocol_config_map)
			if j_err != nil {
				logs.Error(j_err.Error())
			} else {
				protocol_config_map["AccessToken"] = deviceData.Token
				protocol_config_map["SubDeviceAddr"] = deviceData.SubDeviceAddr
				reqmap["DeviceConfig"] = protocol_config_map
				reqdata, json_err := json.Marshal(reqmap)
				if json_err != nil {
					logs.Error(json_err.Error())
				} else {
					var TpProtocolPluginService services.TpProtocolPluginService
					pp := TpProtocolPluginService.GetByProtocolType(deviceData.Protocol, "2")
					tphttp.AddDeviceConfig(reqdata, pp.HttpAddress)

				}
			}

		}

		response.SuccessWithDetailed(200, "success", deviceData, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
	}
}

func (reqDate *DeviceController) UpdateOnly() {
	addDeviceValidate := valid.EditDevice{}
	err := json.Unmarshal(reqDate.Ctx.Input.RequestBody, &addDeviceValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(addDeviceValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(addDeviceValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(reqDate.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	// 判断前端送来的参数是否有script_id==""
	logs.Info("判断script_id")
	var reqMap = make(map[string]interface{})
	errs := json.Unmarshal(reqDate.Ctx.Input.RequestBody, &reqMap)
	if errs != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	if value, ok := reqMap["script_id"]; ok {
		if value == "" {
			err := DeviceService.ScriptIdEdit(addDeviceValidate)
			if err != nil {
				response.SuccessWithMessage(1000, "修改脚本id失败", (*context2.Context)(reqDate.Ctx))
				return
			}
		}
	}
	// 如果更换了插件需要删除设备属性当前值（未实现）

	//更换token要校验重复
	logs.Info("检验token")
	d, _ := DeviceService.GetDeviceByID(addDeviceValidate.ID)
	if d != nil {
		if addDeviceValidate.Token != "" && d.Token != addDeviceValidate.Token {
			if DeviceService.IsToken(addDeviceValidate.Token) {
				response.SuccessWithMessage(1000, "与其他设备的token重复", (*context2.Context)(reqDate.Ctx))
				return
			}
			// 删除redis中的token
			redis.DelKey(d.Token)
		}
	}
	// 是否密码有更新（根据密码的有无判断认证方式，需要改进）
	logs.Info("判断密码是否被设置为空")
	if value, ok := reqMap["password"]; ok {
		//密码为空，表中密码制空，mqtt密码制空
		if value == "" {
			err := DeviceService.PasswordEdit(addDeviceValidate, d.Token)
			if err != nil {
				response.SuccessWithMessage(1000, "修改password失败", (*context2.Context)(reqDate.Ctx))
				return
			}
		}
	}
	logs.Info("判断是否修改分组")
	// 判断是否修改了网关设备的分组
	if d.DeviceType == "2" {
		if addDeviceValidate.AssetID != "" && d.AssetID != "" && addDeviceValidate.AssetID != d.AssetID {
			logs.Info("需要修改所有子设备分组")
			DeviceService.EditSubDeviceAsset(addDeviceValidate.ID, addDeviceValidate.AssetID)
		}
		//有子设备的网关，不允许修改设备类型
		if addDeviceValidate.DeviceType == "1" {
			subDeviceCount, _ := DeviceService.GetSubDeviceCount(addDeviceValidate.ID)
			if subDeviceCount > int64(0) {
				response.SuccessWithMessage(400, "网关设备下存在子设备，需要先删除子设备！", (*context2.Context)(reqDate.Ctx))
				return
			}
		}
	}
	// wvp设备不能重复
	if len(addDeviceValidate.Protocol) >= 4 {
		if addDeviceValidate.Protocol[0:4] == "WVP_" && addDeviceValidate.DeviceType == "2" && addDeviceValidate.DId != "" && addDeviceValidate.DId != d.DId {
			didCount, err := DeviceService.GetWvpDeviceCount(addDeviceValidate.DId)
			if err != nil {
				response.SuccessWithMessage(400, err.Error(), (*context2.Context)(reqDate.Ctx))
				return
			}
			if didCount > int64(0) {
				response.SuccessWithMessage(400, "不能重复添加设备！", (*context2.Context)(reqDate.Ctx))
				return
			}

		}
	}
	result := DeviceService.Edit(addDeviceValidate)
	if result == nil {
		deviceDash, _ := DeviceService.GetDeviceByID(addDeviceValidate.ID)
		// 判断议插件修改
		if d.Protocol != "mqtt" && d.Protocol != "MQTT" && d.Protocol[0:4] != "WVP_" {
			// 判断是否设备表单、子设备地址、直连设备token修改;通知协议插件
			if d.DeviceType == "3" || d.DeviceType == "1" {
				if (addDeviceValidate.ProtocolConfig != "" && addDeviceValidate.ProtocolConfig != d.ProtocolConfig) ||
					(addDeviceValidate.SubDeviceAddr != "" && d.SubDeviceAddr != addDeviceValidate.SubDeviceAddr) ||
					(d.DeviceType == "1" && addDeviceValidate.Token != "" && d.Token != addDeviceValidate.Token) {
					dd, _ := DeviceService.GetDeviceByID(addDeviceValidate.ID)
					// 通知插件子设备配置已修改
					var reqmap = make(map[string]interface{})
					reqmap["DeviceType"] = dd.DeviceType
					reqmap["ParentId"] = dd.ParentId
					reqmap["DeviceId"] = dd.ID
					var protocol_config_map = make(map[string]interface{})
					j_err := json.Unmarshal([]byte(dd.ProtocolConfig), &protocol_config_map)
					if j_err != nil {
						logs.Error(j_err.Error())
					} else {
						protocol_config_map["AccessToken"] = dd.Token
						protocol_config_map["SubDeviceAddr"] = dd.SubDeviceAddr
						reqmap["DeviceConfig"] = protocol_config_map
						reqdata, json_err := json.Marshal(reqmap)
						if json_err != nil {
							logs.Error(json_err.Error())
						} else {
							if d.DeviceType == "1" {
								var TpProtocolPluginService services.TpProtocolPluginService
								pp := TpProtocolPluginService.GetByProtocolType(d.Protocol, "1")
								rsp, _ := tphttp.UpdateDeviceConfig(reqdata, pp.HttpAddress)
								if rsp.StatusCode == 200 {
									// 读取响应体
									bodyBytes, err := ioutil.ReadAll(rsp.Body)
									if err != nil {
										// 处理错误
										fmt.Println("Error reading response body:", err)
									} else {
										// 始终记得关闭响应体
										defer rsp.Body.Close()
										// 解析为map[string]interface{}
										var responseMap map[string]interface{}
										err = json.Unmarshal(bodyBytes, &responseMap)
										if err != nil {
											// 处理错误
											fmt.Println("Error unmarshaling JSON:", err)
										} else {
											if responseMap["code"].(float64) == 400 {
												//返回错误
												response.SuccessWithMessage(400, responseMap["message"].(string), (*context2.Context)(reqDate.Ctx))
												return
											}
										}
									}
								}
							} else if d.DeviceType == "3" {
								var TpProtocolPluginService services.TpProtocolPluginService
								pp := TpProtocolPluginService.GetByProtocolType(d.Protocol, "2")
								tphttp.UpdateDeviceConfig(reqdata, pp.HttpAddress)
							}
						}
					}
				}
			}
		} else if d.Protocol[0:4] == "WVP_" && d.DeviceType == "2" {
			var wvpDeviceService services.WvpDeviceService
			err := wvpDeviceService.AddSubVideoDevice(addDeviceValidate)
			if err != nil {
				response.SuccessWithMessage(400, err.Error(), (*context2.Context)(reqDate.Ctx))
			}
		}
		// 更新缓存
		err := redis.SetRedisForJsondata(addDeviceValidate.ID, deviceDash, 0)
		if err != nil {
			logs.Error(err.Error())
		}
		response.SuccessWithDetailed(200, "success", deviceDash, map[string]string{}, (*context2.Context)(reqDate.Ctx))
	} else {
		response.SuccessWithMessage(400, result.Error(), (*context2.Context)(reqDate.Ctx))
	}
}

// 扫码激活设备
func (this *DeviceController) Scan() {
	this.Data["json"] = "Scan success"
	this.ServeJSON()
}

// 获取设备token
func (this *DeviceController) Token() {
	tokenDeviceValidate := valid.TokenDevice{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &tokenDeviceValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(tokenDeviceValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(tokenDeviceValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	d, c := DeviceService.Token(tokenDeviceValidate.ID)
	if c != 0 {
		response.SuccessWithDetailed(200, "获取成功", d.Token, map[string]string{}, (*context2.Context)(this.Ctx))
		return
	}
	response.SuccessWithMessage(400, "设备不存在", (*context2.Context)(this.Ctx))
	return
}

// 删除
func (this *DeviceController) Delete() {
	deleteDeviceValidate := valid.DeleteDevice{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &deleteDeviceValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(deleteDeviceValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(deleteDeviceValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	// 获取用户租户id
	tenantId, ok := this.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(this.Ctx))
		return
	}
	d, _ := DeviceService.GetDeviceByID(deleteDeviceValidate.ID)
	err = DeviceService.Delete(deleteDeviceValidate.ID, tenantId)
	if err == nil {
		// 判断是否协议插件设备删除
		if d.Protocol != "mqtt" && d.Protocol != "MQTT" {
			// 通知插件子设备配置已修改
			var reqmap = make(map[string]interface{})
			reqmap["DeviceType"] = d.DeviceType
			reqmap["ParentId"] = d.ParentId
			reqmap["DeviceId"] = d.ID
			var protocol_config_map = make(map[string]interface{})
			j_err := json.Unmarshal([]byte(d.ProtocolConfig), &protocol_config_map)
			if j_err != nil {
				logs.Error(j_err.Error())
			} else {
				reqmap["DeviceConfig"] = protocol_config_map
				reqdata, json_err := json.Marshal(reqmap)
				if json_err != nil {
					logs.Error(json_err.Error())
				} else {
					var TpProtocolPluginService services.TpProtocolPluginService
					if d.DeviceType == "1" {
						pp := TpProtocolPluginService.GetByProtocolType(d.Protocol, "1")
						tphttp.DeleteDeviceConfig(reqdata, pp.HttpAddress)
					} else {
						pp := TpProtocolPluginService.GetByProtocolType(d.Protocol, "2")
						tphttp.DeleteDeviceConfig(reqdata, pp.HttpAddress)
					}
				}
			}
		}
		response.SuccessWithMessage(200, "删除成功", (*context2.Context)(this.Ctx))
		return
	} else {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(this.Ctx))
	}

}

// 获取配置参数
func (this *DeviceController) Configure() {
	configureDeviceValidate := valid.ConfigureDevice{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &configureDeviceValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(configureDeviceValidate)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(configureDeviceValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	//var DeviceService services.DeviceService
	//DeviceService
}

// 控制设备
func (c *DeviceController) Operating() {
	reqData := valid.OperatingDevice{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		utils.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// -------------------------------------------
	// 获取设备信息
	var DeviceService services.DeviceService
	deviceData, i := DeviceService.Token(reqData.DeviceId)
	if i == 0 {
		response.SuccessWithMessage(400, "no equipment", (*context2.Context)(c.Ctx))
		return
	}
	// 将struct转map
	valuesMap, _ := reqData.Values.(map[string]interface{})
	// 遍历map拼接指令内容并记录入库
	var instruct string = ""
	for k, v := range valuesMap {
		fmt.Println(reflect.TypeOf(v))
		switch v := v.(type) {
		case string:
			instruct = instruct + k + ":" + v
		case json.Number:
			instruct = instruct + k + ":" + v.String()
		case float64:
			instruct = instruct + k + ":" + strconv.Itoa(int(v))
		}
	}
	// 将map转为json
	newPayload, toErr := json.Marshal(valuesMap)
	if toErr != nil {
		logs.Info("JSON 编码失败：%v\n", toErr)
		response.SuccessWithMessage(400, toErr.Error(), (*context2.Context)(c.Ctx))
		return
	}
	f := DeviceService.SendMessage(newPayload, deviceData)
	//获取用户id
	userID, ok := c.Ctx.Input.GetData("user_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	ConditionsLog := models.ConditionsLog{
		DeviceId:      deviceData.ID,
		OperationType: "2",
		Instruct:      string(newPayload),
		ProtocolType:  "mqtt",
		CteateTime:    time.Now().Format("2006-01-02 15:04:05"),
		TenantId:      deviceData.TenantId,
		UserId:        userID,
	}
	var ConditionsLogService services.ConditionsLogService
	if f == nil {
		logs.Info("成功发送控制")
		ConditionsLog.SendResult = "1"
		ConditionsLogService.Insert(&ConditionsLog)
		response.SuccessWithDetailed(200, "success", valuesMap, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		logs.Info("成功发送失败")
		ConditionsLog.SendResult = "2"
		ConditionsLogService.Insert(&ConditionsLog)
		response.SuccessWithMessage(400, f.Error(), (*context2.Context)(c.Ctx))
	}
}

// func (request *DeviceController) Operating() {
// 	operatingDeviceValidate := valid.OperatingDevice{}
// 	err := json.Unmarshal(request.Ctx.Input.RequestBody, &operatingDeviceValidate)
// 	if err != nil {
// 		fmt.Println("参数解析失败", err.Error())
// 	}
// 	v := validation.Validation{}
// 	status, _ := v.Valid(operatingDeviceValidate)
// 	if !status {
// 		for _, err := range v.Errors {
// 			alias := gvalid.GetAlias(operatingDeviceValidate, err.Field)
// 			message := strings.Replace(err.Message, err.Field, alias, 1)
// 			response.SuccessWithMessage(1000, message, (*context2.Context)(request.Ctx))
// 			break
// 		}
// 		return
// 	}
// 	var payloadInterface interface{}
// 	//将json转为map
// 	logs.Info("==手动控制设备开始==")
// 	jsonErr := json.Unmarshal(request.Ctx.Input.RequestBody, &payloadInterface)
// 	if jsonErr != nil {
// 		fmt.Printf("JSON 解码失败：%v\n", jsonErr)
// 		response.SuccessWithMessage(400, jsonErr.Error(), (*context2.Context)(request.Ctx))
// 	}
// 	//获取设备token
// 	var DeviceService services.DeviceService
// 	deviceData, c := DeviceService.Token(operatingDeviceValidate.DeviceId)
// 	if c == 0 {
// 		response.SuccessWithMessage(400, "no equipment", (*context2.Context)(request.Ctx))
// 	}
// 	payloadInterface.(map[string]interface{})["token"] = deviceData.Token
// 	delete(payloadInterface.(map[string]interface{}), "device_id")
// 	//将value中的key做映射
// 	valueMap, ok := payloadInterface.(map[string]interface{})["values"].(map[string]interface{})
// 	newMap := make(map[string]interface{})
// 	if ok {
// 		for k, v := range valueMap {
// 			var fieldMappingService services.FieldMappingService
// 			newKey := fieldMappingService.TransformByDeviceid(operatingDeviceValidate.DeviceId, k)
// 			if newKey != "" {
// 				newMap[newKey] = v
// 			}
// 			delete(valueMap, k)
// 		}
// 	}
// 	//将map转为json
// 	payloadInterface.(map[string]interface{})["values"] = newMap
// 	newPayload, toErr := json.Marshal(payloadInterface)
// 	if toErr != nil {
// 		logs.Info("JSON 编码失败：%v\n", toErr)
// 		response.SuccessWithMessage(400, toErr.Error(), (*context2.Context)(request.Ctx))
// 	}
// 	logs.Info("-------------------------------", string(newPayload))
// 	f := cm.Send(newPayload)
// 	if f == nil {
// 		response.SuccessWithDetailed(200, "success", payloadInterface, map[string]string{}, (*context2.Context)(request.Ctx))
// 		return
// 	}
// 	response.SuccessWithMessage(400, f.Error(), (*context2.Context)(request.Ctx))
// }

// 重置设备
// func (deviceController *DeviceController) Reset() {
// 	operatingDevice := valid.OperatingDevice{}
// 	err := json.Unmarshal(deviceController.Ctx.Input.RequestBody, &operatingDevice)
// 	if err != nil {
// 		fmt.Println("参数解析失败", err.Error())
// 	}
// 	v := validation.Validation{}
// 	status, _ := v.Valid(operatingDevice)
// 	if !status {
// 		for _, err := range v.Errors {
// 			alias := gvalid.GetAlias(operatingDevice, err.Field)
// 			message := strings.Replace(err.Message, err.Field, alias, 1)
// 			response.SuccessWithMessage(1000, message, (*context2.Context)(deviceController.Ctx))
// 			break
// 		}
// 		return
// 	}
// 	var DeviceService services.DeviceService
// 	f, i := DeviceService.Token(operatingDevice.DeviceId)
// 	if i != 0 {
// 		operatingMap := map[string]interface{}{
// 			"token":  f.Token,
// 			"values": operatingDevice.Values,
// 		}
// 		newPayload, toErr := json.Marshal(operatingMap)
// 		if toErr != nil {
// 			fmt.Printf("JSON 编码失败：%v\n", toErr)
// 			response.SuccessWithMessage(400, toErr.Error(), (*context2.Context)(deviceController.Ctx))
// 		}
// 		log.Println(comm.ReplaceUserInput(string(newPayload)))
// 		redis.SetStr(f.Token, string(newPayload), 300*time.Second)
// 		//cache.Bm.Put(context.TODO(), f.Token, newPayload, 300*time.Second)
// 	} else {
// 		response.SuccessWithMessage(1000, "token不存在", (*context2.Context)(deviceController.Ctx))
// 	}
// 	response.SuccessWithMessage(200, "success", (*context2.Context)(deviceController.Ctx))
// 	//var DeviceService services.DeviceService
// 	//DeviceService
// }

func (DeviceController *DeviceController) DeviceById() {
	Device := valid.Device{}
	err := json.Unmarshal(DeviceController.Ctx.Input.RequestBody, &Device)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(Device)
	if !status {
		for _, err := range v.Errors {
			alias := gvalid.GetAlias(Device, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceController.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	d, _ := DeviceService.GetDeviceByID(Device.ID)
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(DeviceController.Ctx))
}

// 分页获取设备列表
func (DeviceController *DeviceController) PageList() {
	DevicePageListValidate := valid.DevicePageListValidate{}
	err := json.Unmarshal(DeviceController.Ctx.Input.RequestBody, &DevicePageListValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(DevicePageListValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DevicePageListValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceController.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	w, c := DeviceService.PageGetDevicesByAssetID(DevicePageListValidate.BusinessId, DevicePageListValidate.AssetId,
		DevicePageListValidate.DeviceId, DevicePageListValidate.CurrentPage, DevicePageListValidate.PerPage,
		DevicePageListValidate.DeviceType, DevicePageListValidate.Token, DevicePageListValidate.Name)
	var AssetService services.AssetService
	for _, deviceRow := range w {
		if deviceRow["device_type"] != nil {
			fields := AssetService.ExtensionName(deviceRow["device_type"].(string))
			deviceRow["structure"] = fields
		}
	}
	d := PaginateWarninglogList{
		CurrentPage: DevicePageListValidate.CurrentPage,
		Data:        w,
		Total:       c,
		PerPage:     DevicePageListValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(DeviceController.Ctx))
}

func (c *DeviceController) PageListTree() {
	reqData := valid.DevicePageListValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DeviceService services.DeviceService
	w, i := DeviceService.PageGetDevicesByAssetIDTree(reqData, tenantId)
	d := PaginateWarninglogList{
		CurrentPage: reqData.CurrentPage,
		Data:        w,
		Total:       i,
		PerPage:     reqData.PerPage,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}
func (c *DeviceController) OpenApiPageListTree() {
	reqData := valid.OpenApiDeviceListValidate{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DeviceService services.DeviceService
	w, i, err := DeviceService.AllDeviceListByTenantId(reqData, tenantId)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	d := PaginateWarninglogList{
		CurrentPage: reqData.CurrentPage,
		Data:        w,
		Total:       i,
		PerPage:     reqData.PerPage,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

func (DeviceController *DeviceController) GetGatewayConfig() {
	AccessTokenValidate := valid.AccessTokenValidate{}
	err := json.Unmarshal(DeviceController.Ctx.Input.RequestBody, &AccessTokenValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(AccessTokenValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(AccessTokenValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceController.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	d, err := DeviceService.GetConfigByToken(AccessTokenValidate.AccessToken, AccessTokenValidate.DeviceId)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(DeviceController.Ctx))
		return
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(DeviceController.Ctx))
}

func (DeviceController *DeviceController) GetAllDeviceConfig() {
	ProtocolFormValidate := valid.ProtocolFormValidate{}
	err := json.Unmarshal(DeviceController.Ctx.Input.RequestBody, &ProtocolFormValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(ProtocolFormValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(ProtocolFormValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceController.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	d, err := DeviceService.GetConfigByProtocolAndDeviceType(ProtocolFormValidate.ProtocolType, ProtocolFormValidate.DeviceType)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(DeviceController.Ctx))
		return
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(DeviceController.Ctx))
}
func (DeviceController *DeviceController) GetProtocolForm() {
	ProtocolFormValidate := valid.ProtocolFormValidate{}
	err := json.Unmarshal(DeviceController.Ctx.Input.RequestBody, &ProtocolFormValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(ProtocolFormValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(ProtocolFormValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceController.Ctx))
			break
		}
		return
	}
	if ProtocolFormValidate.ProtocolType[0:4] == "WVP_" {
		response.SuccessWithMessage(200, "success", (*context2.Context)(DeviceController.Ctx))
		return
	}
	var d = make(map[string]interface{})
	var TpProtocolPluginService services.TpProtocolPluginService
	// 查询插件注册
	pp := TpProtocolPluginService.GetByProtocolType(ProtocolFormValidate.ProtocolType, ProtocolFormValidate.DeviceType)
	if pp.Id != "" {
		var formType = ""
		if ProtocolFormValidate.DeviceType == "3" {
			formType = "2"
		} else {
			formType = "1"
		}
		rsp, rsp_err := tphttp.GetPluginFromConfig(pp.HttpAddress, ProtocolFormValidate.ProtocolType, formType)
		if rsp_err != nil {
			response.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(DeviceController.Ctx))
		}
		req_err := json.Unmarshal(rsp, &d)
		if req_err != nil {
			response.SuccessWithMessage(400, req_err.Error(), (*context2.Context)(DeviceController.Ctx))
			return
		}
	}
	response.SuccessWithDetailed(200, "success", d["data"], map[string]string{}, (*context2.Context)(DeviceController.Ctx))
}

func (DeviceController *DeviceController) AllDeviceList() {
	DevicePageListValidate := valid.DevicePageListValidate{}
	err := json.Unmarshal(DeviceController.Ctx.Input.RequestBody, &DevicePageListValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(DevicePageListValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DevicePageListValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceController.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	w, c := DeviceService.AllDeviceList(DevicePageListValidate)
	d := PaginateWarninglogList{
		CurrentPage: DevicePageListValidate.CurrentPage,
		Data:        w,
		Total:       c,
		PerPage:     DevicePageListValidate.PerPage,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(DeviceController.Ctx))
}
func (DeviceController *DeviceController) GetDeviceDetailsByParentTokenAndSubDeviceAddr() {
	TokenSubDeviceAddrValidate := valid.TokenSubDeviceAddrValidate{}
	err := json.Unmarshal(DeviceController.Ctx.Input.RequestBody, &TokenSubDeviceAddrValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(TokenSubDeviceAddrValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(TokenSubDeviceAddrValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(DeviceController.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	d, err := DeviceService.GetDeviceDetailsByParentTokenAndSubDeviceAddr(TokenSubDeviceAddrValidate.AccessToken, TokenSubDeviceAddrValidate.SubDeviceAddr)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(DeviceController.Ctx))
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(DeviceController.Ctx))
}

func (DeviceController *DeviceController) GetDeviceByCascade() {
	var DeviceService services.DeviceService
	d, err := DeviceService.GetDeviceByCascade()
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(DeviceController.Ctx))
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(DeviceController.Ctx))
}

// 地图接口
func (c *DeviceController) DeviceMapList() {
	reqData := valid.DeviceMapValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var DeviceService services.DeviceService
	d, err := DeviceService.DeviceMapList(reqData, tenantId)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 设备在线离线状态
func (c *DeviceController) DeviceStatus() {
	DeviceMapValidate := valid.DeviceIdListValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &DeviceMapValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(DeviceMapValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(DeviceMapValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	d, err := DeviceService.GetDeviceOnlineStatus(DeviceMapValidate)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 新增某产品下的所有设备列表
// 2023-3-20
func (c *DeviceController) DeviceListByProductId() {
	PaginationValidate := valid.DevicePaginationValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &PaginationValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(PaginationValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(PaginationValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	var DeviceService services.DeviceService
	isSuccess, d, t := DeviceService.DeviceListByProductId(PaginationValidate)
	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	dd := valid.RspDevicePaginationValidate{
		CurrentPage: PaginationValidate.CurrentPage,
		Data:        d,
		Total:       t,
		PerPage:     PaginationValidate.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))

}

// 设备事件上报历史纪录查询
func (c *DeviceController) DeviceEventHistoryList() {
	inputData := valid.DeviceEventCommandHistoryValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &inputData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	offset := (inputData.CurrentPage - 1) * inputData.PerPage
	var s services.DeviceEvnetHistory
	data, count := s.GetDeviceEvnetHistoryListByDeviceId(offset, inputData.PerPage, inputData.DeviceId)
	d := DataTransponList{
		CurrentPage: inputData.CurrentPage,
		Total:       count,
		PerPage:     inputData.PerPage,
		Data:        data,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))

}

// 设备命令下发历史纪录查询
func (c *DeviceController) DeviceCommandHistoryList() {
	inputData := valid.DeviceEventCommandHistoryValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &inputData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}

	offset := (inputData.CurrentPage - 1) * inputData.PerPage
	var s services.DeviceCommandHistory
	data, count := s.GetDeviceCommandHistoryListByDeviceId(offset, inputData.PerPage, inputData.DeviceId)
	d := DataTransponList{
		CurrentPage: inputData.CurrentPage,
		Total:       count,
		PerPage:     inputData.PerPage,
		Data:        data,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 查询设备支持的命令
func (c *DeviceController) DeviceCommandList() {

	inputData := valid.DeviceCommandListValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &inputData)
	if err != nil || len(inputData.DeviceId) == 0 {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	var device services.DeviceService

	deviceInfo, _ := device.GetDeviceByID(inputData.DeviceId)

	// 没有设备或者没有绑定device_model
	if len(deviceInfo.Type) == 0 {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	// 根据device.type，查询device_model.id
	var deviceModel services.DeviceModelService
	data, _ := deviceModel.GetModelCommandsByPluginId(deviceInfo.Type)

	// if err != nil {
	// 	response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
	// 	return
	// }

	response.SuccessWithDetailed(200, "success", data, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 向设备发送命令
func (c *DeviceController) DeviceCommandSend() {
	inputData := valid.DeviceCommandSendValid{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &inputData)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	var d services.DeviceService
	device, i := d.Token(inputData.DeviceId)
	if i == 0 {
		response.SuccessWithMessage(400, "no device", (*context2.Context)(c.Ctx))
		return
	}

	// if device.Protocol != "mqtt" && device.Protocol != "MQTT" {
	// 	response.SuccessWithMessage(400, "protocol error", (*context2.Context)(c.Ctx))
	// }

	//
	//获取用户id
	userID, ok := c.Ctx.Input.GetData("user_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}

	d.SendCommandToDevice(
		device, inputData.DeviceId, inputData.CommandIdentifier,
		[]byte(inputData.CommandData),
		inputData.CommandName,
		inputData.Desc, userID)

	response.SuccessWithDetailed(200, "success", nil, map[string]string{}, (*context2.Context)(c.Ctx))
}

func (c *DeviceController) GetBusinessIdAssetIdByDevice() {
	var inputData struct {
		ID string `json:"id" alias:"设备ID" valid:"Required;MaxSize(36)"`
	}

	err := json.Unmarshal(c.Ctx.Input.RequestBody, &inputData)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	var d services.DeviceService
	device, i := d.Token(inputData.ID)
	if i == 0 {
		response.SuccessWithMessage(400, "no device", (*context2.Context)(c.Ctx))
		return
	}

	var a services.AssetService
	asset, i := a.GetAssetById(device.AssetID)
	if i == 0 {
		response.SuccessWithMessage(400, "no asset", (*context2.Context)(c.Ctx))
		return
	}

	r := make(map[string]string)
	r["device_id"] = inputData.ID
	r["asset_id"] = device.AssetID
	r["business_id"] = asset.BusinessID

	response.SuccessWithDetailed(200, "success", r, map[string]string{}, (*context2.Context)(c.Ctx))
}

// 租户设备总数
func (c *DeviceController) DeviceTenantCount() {
	reqData := valid.DeviceTenantCountType{}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &reqData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var s services.DeviceService
	dd := s.GetTenantDeviceCount(tenantId, reqData.CountType)

	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))
	return

}

// 操作设备“在/离线”状态
func (c *DeviceController) DeviceOnlineStatus() {
	var inputData struct {
		ID     string `json:"id" alias:"设备ID" valid:"Required;MaxSize(36)"`
		Status string `json:"status" alias:"设备状态" valid:"Required"`
	}
	if err := valid.ParseAndValidate(&c.Ctx.Input.RequestBody, &inputData); err != nil {
		response.SuccessWithMessage(1000, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	//获取租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "代码逻辑错误", (*context2.Context)(c.Ctx))
		return
	}
	var s services.DeviceService
	err := s.OperateDeviceStatus(inputData.ID, inputData.Status, tenantId)
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	response.Success(200, (*context2.Context)(c.Ctx))
}
