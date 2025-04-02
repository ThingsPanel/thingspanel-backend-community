package utils

import (
	"fmt"
	"reflect"
)

func StructToMap(obj interface{}) (map[string]interface{}, error) {
	// 确保输入是一个指针，并且不是nil
	if reflect.ValueOf(obj).Kind() != reflect.Ptr || reflect.ValueOf(obj).IsNil() {
		return nil, fmt.Errorf("input must be a non-nil pointer")
	}

	// 获取指针指向的实际结构体
	val := reflect.ValueOf(obj).Elem()

	// 创建映射
	output := make(map[string]interface{})

	// 遍历结构体的所有字段
	for i := 0; i < val.NumField(); i++ {
		// 获取字段的值
		valueField := val.Field(i)

		// 获取字段的类型
		typeField := val.Type().Field(i)

		// 获取字段名
		fieldName := typeField.Name

		// 将字段名和对应的值添加到映射中
		output[fieldName] = valueField.Interface()
	}

	return output, nil
}
