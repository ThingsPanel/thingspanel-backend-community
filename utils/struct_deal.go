package utils

import "reflect"

// @title    structAssign
// @description   使用反射对结构体赋值
// @auth      何卓              时间（2022/04/18   10:57 ）
// @param     输入参数名        参数类型         "解释"
// @param	  binding          interface 		要修改的结构体
// @param	  value            interace 		有数据的结构体
// @return
func StructAssign(binding interface{}, value interface{}) {
	bVal := reflect.ValueOf(binding).Elem() //获取reflect.Type类型
	vVal := reflect.ValueOf(value).Elem()   //获取reflect.Type类型
	vTypeOfT := vVal.Type()
	for i := 0; i < vVal.NumField(); i++ {
		// 在要修改的结构体中查询有数据结构体中相同属性的字段，有则修改其值
		name := vTypeOfT.Field(i).Name
		if ok := bVal.FieldByName(name).IsValid(); ok {
			bVal.FieldByName(name).Set(reflect.ValueOf(vVal.Field(i).Interface()))
		}
	}
}
