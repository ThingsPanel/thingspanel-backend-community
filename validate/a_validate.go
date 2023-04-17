package valid

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/beego/beego/v2/core/validation"
)

func ParseAndValidate(requestBody *[]byte, reqData interface{}) error {
	valueType := reflect.ValueOf(reqData).Elem().Type() // 获取结构体类型
	newStruct := reflect.New(valueType).Interface()     // 创建新的结构体
	if err := json.Unmarshal(*requestBody, newStruct); err != nil {
		return err
	}

	v := validation.Validation{}
	status, err := v.Valid(newStruct)
	if err != nil {
		return err
	}
	if !status {
		for _, err := range v.Errors {
			// 获取 newStruct 指向的真实结构体的类型
			structType := reflect.Indirect(reflect.ValueOf(newStruct)).Type()
			field, _ := structType.FieldByName(err.Field)
			alias := field.Tag.Get("alias")
			message := strings.Replace(err.Message, err.Field, alias, 1)
			return errors.New(message)
		}
	}

	reflect.ValueOf(reqData).Elem().Set(reflect.ValueOf(newStruct).Elem())
	return nil
}

// import (
// 	"encoding/json"
// 	"fmt"
// 	"reflect"
// 	"strings"

// 	v "ThingsPanel-Go/initialize/validate"

// 	"github.com/go-playground/validator/v10"
// )

// // ValidationErrors 是自定义的错误类型
// type ValidationErrors []error

// // Error 方法实现了 error 接口
// func (v ValidationErrors) Error() string {
// 	var errStr []string
// 	for _, err := range v {
// 		errStr = append(errStr, err.Error())
// 	}
// 	return strings.Join(errStr, ",")
// }

// // ParseJSON 解析 JSON 数据
// func ParseJSON(requestBody []byte, reqData interface{}) error {
// 	if err := json.Unmarshal(requestBody, reqData); err != nil {
// 		return err
// 	}
// 	return nil
// }

// // FormatValidationError 获取校验错误信息
// func FormatValidationError(err validator.FieldError, fieldType reflect.StructField) error {
// 	fieldName := fieldType.Name
// 	if alias := fieldType.Tag.Get("alias"); alias != "" {
// 		fieldName = alias
// 	}
// 	return fmt.Errorf("%s %s", fieldName, err.Translate(v.Trans))
// }

// // ParseAndValidate 解析并校验数据
// func ParseAndValidate(requestBody *[]byte, reqData interface{}) error {
// 	// 解析 JSON 数据
// 	if err := ParseJSON(*requestBody, reqData); err != nil {
// 		return err
// 	}

// 	// 校验数据
// 	if err := v.Validate.Struct(reqData); err != nil {
// 		var validationErrors ValidationErrors
// 		for _, e := range err.(validator.ValidationErrors) {
// 			// 获取结构体字段的别名和类型
// 			fieldType, ok := reflect.TypeOf(reqData).Elem().FieldByName(e.Field())
// 			if !ok {
// 				// 没有找到指定名称的字段
// 				continue
// 			}

// 			// 格式化校验错误信息
// 			if err := FormatValidationError(e, fieldType); err != nil {
// 				validationErrors = append(validationErrors, err)
// 			}
// 		}

// 		if len(validationErrors) > 0 {
// 			return validationErrors
// 		}
// 	}

// 	return nil
// }
