package valid

import "ThingsPanel-Go/models"

type TpFunctionValidate struct {
	Id           string `json:"id"  alias:"ID" valid:"MaxSize(36)"` // ID
	FunctionName string `json:"function_name"  alias:"功能名称" valid:"MaxSize(99)"`
	Path         string `json:"path" alias:"页面路径" valid:"MaxSize(255)"`                  //
	Name         string `json:"name" alias:"页面名称" valid:"MaxSize(255)"`                  //
	Component    string `json:"component" alias:"组件路径" valid:"MaxSize(255)"`             //
	Title        string `json:"title" alias:"页面标题" valid:"MaxSize(255)"`                 //
	Icon         string `json:"icon" alias:"页面图表" valid:"MaxSize(255)"`                  //
	Type         string `json:"type" alias:"类型0-目录 1-菜单 2-页面 3-按钮" valid:"MaxSize(255)"` //
	FunctionCode string `json:"function_code" alias:"编码" valid:"MaxSize(255)"`           //
	ParentId     string `json:"parent_id" alias:"父id" valid:"MaxSize(36)"`               //
}

type TpFunctionTreeValidate struct {
	FunctionName string                   `json:"function_name,omitempty"  alias:"功能名称" valid:"MaxSize(99)"`
	Path         string                   `json:"path,omitempty" alias:"页面路径" valid:"MaxSize(255)"`        //
	Name         string                   `json:"name" alias:"页面名称" valid:"MaxSize(255)"`                  //
	Component    string                   `json:"component,omitempty" alias:"组件路径" valid:"MaxSize(255)"`   //
	Title        string                   `json:"title,omitempty" alias:"页面标题" valid:"MaxSize(255)"`       //
	Icon         string                   `json:"icon,omitempty" alias:"页面图表" valid:"MaxSize(255)"`        //
	Type         string                   `json:"type" alias:"类型0-目录 1-菜单 2-页面 3-按钮" valid:"MaxSize(255)"` //
	FunctionCode string                   `json:"function_code,omitempty" alias:"编码" valid:"MaxSize(255)"` //
	Children     []TpFunctionTreeValidate `json:"children,omitempty" alias:"子节点" valid:"MaxSize(36)"`      //

}

type TpFunctionTreeAuthValidate struct {
	Id           string                       `json:"id"  alias:"ID" valid:"MaxSize(36)"` // ID
	FunctionName string                       `json:"function_name,omitempty"  alias:"功能名称" valid:"MaxSize(99)"`
	Path         string                       `json:"path,omitempty" alias:"页面路径" valid:"MaxSize(255)"`        //
	Name         string                       `json:"name" alias:"页面名称" valid:"MaxSize(255)"`                  //
	Component    string                       `json:"component,omitempty" alias:"组件路径" valid:"MaxSize(255)"`   //
	Title        string                       `json:"title,omitempty" alias:"页面标题" valid:"MaxSize(255)"`       //
	Icon         string                       `json:"icon,omitempty" alias:"页面图表" valid:"MaxSize(255)"`        //
	Type         string                       `json:"type" alias:"类型0-目录 1-菜单 2-页面 3-按钮" valid:"MaxSize(255)"` //
	FunctionCode string                       `json:"function_code,omitempty" alias:"编码" valid:"MaxSize(255)"` //
	Children     []TpFunctionTreeAuthValidate `json:"children,omitempty" alias:"子节点" valid:"MaxSize(36)"`      //

}

type TpFunctionPullDownListValidate struct {
	Id           string                           `json:"id"  alias:"ID" valid:"MaxSize(36)"` // ID
	FunctionName string                           `json:"function_name"  alias:"功能名称" valid:"MaxSize(99)"`
	Children     []TpFunctionPullDownListValidate `json:"children,omitempty" alias:"子节点" valid:"MaxSize(36)"` //

}

type FunctionPaginationValidate struct {
	CurrentPage int                 `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                 `json:"per_page"  alias:"每页页数" valid:"Required;Max(36)"`
	Data        []models.TpFunction `json:"data,omitempty" alias:"返回数据"`
	Total       int64               `json:"total,omitempty" alias:"总数"`
	Id          string              `json:"id,omitempty" alias:"id"`
}
