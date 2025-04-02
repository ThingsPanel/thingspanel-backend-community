package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type ServiceGroup struct {
	User
	Role
	Dict
	Product
	OTA
	ProtocolPlugin
	Device
	DeviceModel
	DeviceTemplate
	DeviceGroup
	UiElements
	TelemetryData
	EventData
	AttributeData
	CommandData
	OperationLogs
	Logo
	DataPolicy
	Board
	DeviceConfig
	DataScript
	Casbin
	NotificationGroup
	NotificationHisory
	NotificationServicesConfig
	Alarm
	Scene
	SceneAutomation
	SceneAutomationLog
	Automate
	AutomateTask
	SysFunction
	VisPlugin
	ServicePlugin
	ServiceAccess
	ExpectedData
	OpenAPIKey
	MessagePush
}

var GroupApp = new(ServiceGroup)

// safeDeref 安全地解引用字符串指针，如果是nil则返回空字符串
func SafeDeref(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

// stringPtr 返回一个字符串的指针
func StringPtr(s string) *string {
	return &s
}

// IsJSON 校验一个字符串是否为有效的 JSON 格式
func IsJSON(str string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(str), &js) == nil
}

// contains checks if a slice contains a specific string.
func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// 将结构体转换为map，并校验json格式的字段
// 对于nil值的字段不会被转换
// 例如：StructToMapAndVerifyJson(req, "additional_info")
func StructToMapAndVerifyJson(obj interface{}, jsonTagsToCheck ...string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	val := reflect.ValueOf(obj)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input is not a struct")
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		typeField := typ.Field(i)

		jsonTag := typeField.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		jsonKey := strings.Split(jsonTag, ",")[0]

		// Adjust the condition to check if the field is a pointer or not before calling IsNil
		if field.Kind() == reflect.Ptr && !field.IsNil() {
			strVal, ok := field.Interface().(*string)
			if ok && strVal != nil && contains(jsonTagsToCheck, jsonKey) {
				if !IsJSON(*strVal) {
					return nil, fmt.Errorf("%s is not valid JSON", jsonKey)
				}
			}
		}

		if field.IsValid() && (field.Kind() != reflect.Ptr || !field.IsNil()) {
			result[jsonKey] = field.Interface()
		}
	}
	return result, nil
}

// 结构体转map;对于nil值的字段不会被转换
func StructToMap(obj interface{}, _ ...string) map[string]interface{} {
	result := make(map[string]interface{})
	val := reflect.ValueOf(obj)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return result
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		typeField := typ.Field(i)

		jsonTag := typeField.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		// Get the first part of the json tag, ignore omitempty etc.
		jsonKey := strings.Split(jsonTag, ",")[0]

		if field.Kind() == reflect.Ptr || field.Kind() == reflect.Slice || field.Kind() == reflect.Map || field.Kind() == reflect.Interface || field.Kind() == reflect.Chan || field.Kind() == reflect.Func {
			if !field.IsNil() {
				result[jsonKey] = field.Interface()
			}
		} else {
			result[jsonKey] = field.Interface()
		}
	}
	return result
}
