package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"

	"gorm.io/gorm"
)

type TpFunctionService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取角色列表
func (*TpFunctionService) GetFunctionList() (bool, []models.TpFunction) {
	var TpFunctions []models.TpFunction
	result := psql.Mydb.Model(&models.TpFunction{}).Find(&TpFunctions)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, TpFunctions
	}
	return true, TpFunctions
}

// Add新增角色
func (*TpFunctionService) AddFunction(tp_function models.TpFunction) (bool, models.TpFunction) {
	var uuid = uuid.GetUuid()
	tp_function.Id = uuid
	result := psql.Mydb.Create(&tp_function)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, tp_function
	}
	return true, tp_function
}

// 根据ID编辑role
func (*TpFunctionService) EditFunction(tp_function models.TpFunction) bool {
	result := psql.Mydb.Save(&tp_function)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除角色
func (*TpFunctionService) DeleteFunction(tp_function models.TpFunction) bool {
	result := psql.Mydb.Delete(&tp_function)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 功能下拉列表
func (*TpFunctionService) FunctionPullDownList() []valid.TpFunctionPullDownListValidate {
	return PullDownListTree("0")
}

func PullDownListTree(parent_id string) []valid.TpFunctionPullDownListValidate {
	var TpFunctionPullDownListValidates []valid.TpFunctionPullDownListValidate
	var TpFunctions []models.TpFunction
	result := psql.Mydb.Model(&models.TpFunction{}).Where("parent_id = ?", parent_id).Order("sort desc").Find(&TpFunctions)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return TpFunctionPullDownListValidates
	}
	if len(TpFunctions) > 0 {
		for _, TpFunction := range TpFunctions {
			var TpFunctionPullDownListValidate valid.TpFunctionPullDownListValidate
			TpFunctionPullDownListValidate.Id = TpFunction.Id
			TpFunctionPullDownListValidate.FunctionName = TpFunction.Name
			TpFunctionPullDownListValidate.Children = PullDownListTree(TpFunction.Id)
			TpFunctionPullDownListValidates = append(TpFunctionPullDownListValidates, TpFunctionPullDownListValidate)
		}
	} else {
		return TpFunctionPullDownListValidates
	}
	return TpFunctionPullDownListValidates
}

// // 权限树
// func (*TpFunctionService) AuthorityList() []valid.TpFunctionTreeAuthValidate {
// 	return AuthorityTree("0")
// }

// func AuthorityTree(parent_id string) []valid.TpFunctionTreeAuthValidate {
// 	var TpFunctionTreeAuthValidates []valid.TpFunctionTreeAuthValidate
// 	var TpFunctions []models.TpFunction
// 	result := psql.Mydb.Model(&models.TpFunction{}).Where("parent_id = ?", parent_id).Order("sort desc").Find(&TpFunctions)
// 	if result.Error != nil {
// 		errors.Is(result.Error, gorm.ErrRecordNotFound)
// 		return TpFunctionTreeAuthValidates
// 	}
// 	if len(TpFunctions) > 0 {
// 		for _, TpFunction := range TpFunctions {
// 			var TpFunctionTreeAuthValidate valid.TpFunctionTreeAuthValidate
// 			TpFunctionTreeAuthValidate.Id = TpFunction.Id
// 			TpFunctionTreeAuthValidate.FunctionName = TpFunction.Name
// 			TpFunctionTreeAuthValidate.Children = AuthorityTree(TpFunction.Id)
// 			TpFunctionTreeAuthValidates = append(TpFunctionTreeAuthValidates, TpFunctionTreeAuthValidate)
// 		}
// 	} else {
// 		return TpFunctionTreeAuthValidates
// 	}
// 	return TpFunctionTreeAuthValidates
// }

// 用户权限查询
func (*TpFunctionService) Authority() ([]valid.TpFunctionTreeValidate, []string, []valid.TpFunctionTreeValidate) {

	return UserAuthorityTree("0")
}
func UserAuthorityTree(parent_id string) ([]valid.TpFunctionTreeValidate, []string, []valid.TpFunctionTreeValidate) {
	var TpFunctionTreeValidates []valid.TpFunctionTreeValidate
	var functionList []string
	var pageList []valid.TpFunctionTreeValidate
	var TpFunctions []models.TpFunction
	result := psql.Mydb.Model(&models.TpFunction{}).Where("parent_id = ?", parent_id).Order("sort desc").Find(&TpFunctions)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return TpFunctionTreeValidates, functionList, pageList
	}
	if len(TpFunctions) > 0 {
		for _, TpFunction := range TpFunctions {
			var TpFunctionTreeValidate valid.TpFunctionTreeValidate
			var l []string
			var page []valid.TpFunctionTreeValidate
			if TpFunction.Type == "0" || TpFunction.Type == "1" { //目录、菜单
				TpFunctionTreeValidate.FunctionName = TpFunction.Name
				TpFunctionTreeValidate.Component = TpFunction.Component
				TpFunctionTreeValidate.Title = TpFunction.Title
				TpFunctionTreeValidate.Icon = TpFunction.Icon
				TpFunctionTreeValidate.Path = TpFunction.Path
				TpFunctionTreeValidate.Type = TpFunction.Type
				TpFunctionTreeValidate.Children, l, page = UserAuthorityTree(TpFunction.Id)
				pageList = append(pageList, page...)
				functionList = append(functionList, l...)
				TpFunctionTreeValidates = append(TpFunctionTreeValidates, TpFunctionTreeValidate)
			} else if TpFunction.Type == "2" { //页面
				TpFunctionTreeValidate.Component = TpFunction.Component
				TpFunctionTreeValidate.Path = TpFunction.Path
				TpFunctionTreeValidate.Type = TpFunction.Type
				pageList = append(pageList, TpFunctionTreeValidate)
				_, l, page = UserAuthorityTree(TpFunction.Id)
				pageList = append(pageList, page...)
				functionList = append(functionList, l...)
			} else if TpFunction.Type == "3" { //按钮等
				functionList = append(functionList, TpFunction.FunctionCode)
				_, l, page = UserAuthorityTree(TpFunction.Id)
				pageList = append(pageList, page...)
				functionList = append(functionList, l...)
			}

		}
	} else {
		return TpFunctionTreeValidates, functionList, pageList
	}
	return TpFunctionTreeValidates, functionList, pageList
}
