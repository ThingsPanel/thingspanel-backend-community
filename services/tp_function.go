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

// 获取功能列表
func (*TpFunctionService) GetFunctionList(FunctionPaginationValidate valid.FunctionPaginationValidate) (bool, []models.TpFunction, int64) {

	var TpFunctions []models.TpFunction
	offset := (FunctionPaginationValidate.CurrentPage - 1) * FunctionPaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpFunction{})
	if FunctionPaginationValidate.Id != "" {
		db.Where("id = ?", FunctionPaginationValidate.Id)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(FunctionPaginationValidate.PerPage).Offset(offset).Find(&TpFunctions)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, TpFunctions, 0
	}
	return true, TpFunctions, count

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
func (*TpFunctionService) FunctionPullDownList(authority string) []valid.TpFunctionPullDownListValidate {

	return PullDownListTree("0", authority)

}

func PullDownListTree(parent_id, authority string) []valid.TpFunctionPullDownListValidate {
	var TpFunctionPullDownListValidates []valid.TpFunctionPullDownListValidate
	var TpFunctions []models.TpFunction
	if authority == "SYS_ADMIN" { //系统管理员独有的功能
		result := psql.Mydb.Model(&models.TpFunction{}).Where("parent_id = ?", parent_id).Order("sort desc").Find(&TpFunctions)
		if result.Error != nil {
			errors.Is(result.Error, gorm.ErrRecordNotFound)
			return TpFunctionPullDownListValidates
		}
	} else {
		result := psql.Mydb.Model(&models.TpFunction{}).Where("parent_id = ? and (sys_flag != '1' or sys_flag is null)", parent_id).Order("sort desc").Find(&TpFunctions)
		if result.Error != nil {
			errors.Is(result.Error, gorm.ErrRecordNotFound)
			return TpFunctionPullDownListValidates
		}
	}

	if len(TpFunctions) > 0 {

		for _, TpFunction := range TpFunctions {
			var TpFunctionPullDownListValidate valid.TpFunctionPullDownListValidate
			TpFunctionPullDownListValidate.Id = TpFunction.Id
			TpFunctionPullDownListValidate.Name = TpFunction.Name
			TpFunctionPullDownListValidate.Title = TpFunction.Title
			TpFunctionPullDownListValidate.Children = PullDownListTree(TpFunction.Id, authority)
			TpFunctionPullDownListValidates = append(TpFunctionPullDownListValidates, TpFunctionPullDownListValidate)
		}
	} else {
		return TpFunctionPullDownListValidates
	}
	return TpFunctionPullDownListValidates
}

// 权限树
func (*TpFunctionService) AuthorityList() []valid.TpFunctionTreeAuthValidate {
	return AuthorityTree("0")
}

func AuthorityTree(parent_id string) []valid.TpFunctionTreeAuthValidate {
	var TpFunctionTreeAuthValidates []valid.TpFunctionTreeAuthValidate
	var TpFunctions []models.TpFunction
	result := psql.Mydb.Model(&models.TpFunction{}).Where("parent_id = ?", parent_id).Order("sort desc").Find(&TpFunctions)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return TpFunctionTreeAuthValidates
	}
	if len(TpFunctions) > 0 {
		for _, TpFunction := range TpFunctions {
			var TpFunctionTreeAuthValidate valid.TpFunctionTreeAuthValidate
			TpFunctionTreeAuthValidate.Id = TpFunction.Id
			TpFunctionTreeAuthValidate.Name = TpFunction.Name
			TpFunctionTreeAuthValidate.Title = TpFunction.Title
			TpFunctionTreeAuthValidate.Component = TpFunction.Component
			TpFunctionTreeAuthValidate.FunctionCode = TpFunction.FunctionCode
			TpFunctionTreeAuthValidate.FunctionName = TpFunction.FunctionName
			TpFunctionTreeAuthValidate.Icon = TpFunction.Icon
			TpFunctionTreeAuthValidate.Path = TpFunction.Path
			TpFunctionTreeAuthValidate.Type = TpFunction.Type
			TpFunctionTreeAuthValidate.SysFlag = TpFunction.SysFlag
			TpFunctionTreeAuthValidate.Describe = TpFunction.Describe
			TpFunctionTreeAuthValidate.Children = AuthorityTree(TpFunction.Id)
			TpFunctionTreeAuthValidates = append(TpFunctionTreeAuthValidates, TpFunctionTreeAuthValidate)
		}
	} else {
		return TpFunctionTreeAuthValidates
	}
	return TpFunctionTreeAuthValidates
}

// 用户权限查询
func (*TpFunctionService) Authority(email string) ([]valid.TpFunctionTreeValidate, []string, []valid.TpFunctionTreeValidate) {

	return UserAuthorityTree(email, "0")
}
func UserAuthorityTree(email string, parent_id string) ([]valid.TpFunctionTreeValidate, []string, []valid.TpFunctionTreeValidate) {
	// 根据email查询用户，获取用户权限
	getUserByEmail, _, _ := new(UserService).GetUserByEmail(email)
	authority := getUserByEmail.Authority
	var TpFunctionTreeValidates []valid.TpFunctionTreeValidate
	var functionList []string
	var pageList []valid.TpFunctionTreeValidate
	var TpFunctions []models.TpFunction
	var result *gorm.DB
	if authority == "SYS_ADMIN" {
		//sys_flag：1代表系统管理员独有的功能 2代表租户管理员和系统管理员共有的功能
		result = psql.Mydb.Model(&models.TpFunction{}).Where("parent_id = ? and sys_flag in ('1','2')", parent_id).Order("sort desc").Find(&TpFunctions)
	} else if authority == "TENANT_ADMIN" {
		result = psql.Mydb.Model(&models.TpFunction{}).Where("parent_id = ? and (sys_flag != '1' or sys_flag is null)", parent_id).Order("sort desc").Find(&TpFunctions)
	} else {
		result = psql.Mydb.Raw(`select tf.id,tf.function_name,tf."path" ,tf."name" ,tf.component ,tf.title ,tf.icon ,tf."type" ,tf.function_code from 
		(select distinct (crp.v1) from casbin_rule crp inner join (select cr.v1 from casbin_rule cr  where cr.ptype ='g' and cr.v0 = ? ) crr
	   on crr.v1 = crp.v0 where crp.ptype ='p') t left join tp_function tf on t.v1 = tf.id where tf.parent_id =? order by tf.sort desc`, email, parent_id).Scan(&TpFunctions)
	}

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
				TpFunctionTreeValidate.Name = TpFunction.Name
				TpFunctionTreeValidate.Component = TpFunction.Component
				TpFunctionTreeValidate.Title = TpFunction.Title
				TpFunctionTreeValidate.Icon = TpFunction.Icon
				TpFunctionTreeValidate.Path = TpFunction.Path
				TpFunctionTreeValidate.Type = TpFunction.Type
				TpFunctionTreeValidate.Children, l, page = UserAuthorityTree(email, TpFunction.Id)
				pageList = append(pageList, page...)
				functionList = append(functionList, l...)
				TpFunctionTreeValidates = append(TpFunctionTreeValidates, TpFunctionTreeValidate)
			} else if TpFunction.Type == "2" { //页面
				TpFunctionTreeValidate.Component = TpFunction.Component
				TpFunctionTreeValidate.Path = TpFunction.Path
				TpFunctionTreeValidate.Type = TpFunction.Type
				TpFunctionTreeValidate.Name = TpFunction.Name
				pageList = append(pageList, TpFunctionTreeValidate)
				_, l, page = UserAuthorityTree(email, TpFunction.Id)
				pageList = append(pageList, page...)
				functionList = append(functionList, l...)
			} else if TpFunction.Type == "3" { //按钮等
				functionList = append(functionList, TpFunction.FunctionCode)
				_, l, page = UserAuthorityTree(email, TpFunction.Id)
				pageList = append(pageList, page...)
				functionList = append(functionList, l...)
			}

		}
	} else {
		return TpFunctionTreeValidates, functionList, pageList
	}
	return TpFunctionTreeValidates, functionList, pageList
}
