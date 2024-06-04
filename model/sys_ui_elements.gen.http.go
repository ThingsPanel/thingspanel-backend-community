package model

type CreateUiElementsReq struct {
	ID           string  `json:"id"`                                        // 主键ID
	ParentID     string  `json:"parent_id" validate:"required,max=36"`      // 父元素id
	ElementCode  string  `json:"element_code" validate:"required,max=100"`  // 元素标识符
	ElementType  int     `json:"element_type"  validate:"omitempty,max=10"` // 元素类型1-菜单 2-目录 3-按钮 4-路由
	Orders       int     `json:"orders" validate:"omitempty,max=10000"`     // 排序
	Param1       *string `json:"param1" validate:"omitempty,max=255"`
	Param2       *string `json:"param2" validate:"omitempty,max=255"`
	Param3       *string `json:"param3" validate:"omitempty,max=255"`
	Authority    string  `json:"authority" validate:"required"`           // 权限(多选)TENANT_ADMIN-租户管理员 SYS_ADMIN-系统管理员
	Description  *string `json:"description" validate:"omitempty,max=36"` // 描述
	Remark       *string `json:"remark" validate:"omitempty,max=255"`
	Multilingual *string `json:"multilingual" validate:"omitempty,max=255"` // 多语言
	RoutePath    *string `json:"route_path" validate:"omitempty,max=255"`   // 路由路径
}
type UpdateUiElementsReq struct {
	Id           string  `json:"id" validate:"required,max=36"`
	ParentID     *string `json:"parent_id" form:"parent_id" validate:"required,max=36"`         // 父元素id
	ElementCode  *string `json:"element_code" form:"element_code" validate:"required,max=100"`  // 元素标识符
	ElementType  *int16  `json:"element_type" form:"element_type"  validate:"omitempty,max=10"` // 元素类型1-菜单 2-目录 3-按钮 4-路由
	Orders       *int16  `json:"orders" form:"orders" validate:"omitempty,max=10000"`           // 排序
	Param1       *string `json:"param1" form:"param1" validate:"omitempty,max=255"`
	Param2       *string `json:"param2" form:"param2" validate:"omitempty,max=255"`
	Param3       *string `json:"param3" form:"param3" validate:"omitempty,max=255"`
	Authority    *string `json:"authority" form:"authority" validate:"required"`                // 权限(多选)TENANT_ADMIN-租户管理员 SYS_ADMIN-系统管理员
	Description  *string `json:"description" form:"description" validate:"omitempty,max=36"`    // 描述
	Multilingual *string `json:"multilingual" form:"multilingual" validate:"omitempty,max=255"` // 多语言
	RoutePath    *string `json:"route_path" form:"route_path" validate:"omitempty,max=255"`     // 路由路径
	Remark       *string `json:"remark" form:"remark" validate:"omitempty,max=255"`             // 备注
}

type UiElementsListReq struct {
	Id           string  `json:"id" form:"id" validate:"required,max=36"`
	ParentID     string  `json:"parent_id" form:"parent_id" validate:"required,max=36"`         // 父元素id
	ElementCode  string  `json:"element_code" form:"element_code" validate:"required,max=100"`  // 元素标识符
	ElementType  *int16  `json:"element_type" form:"element_type"  validate:"omitempty,max=10"` // 元素类型1-菜单 2-目录 3-按钮 4-路由
	Orders       *int16  `json:"orders" form:"orders" validate:"omitempty,max=10000"`           // 排序
	Param1       *string `json:"param1" form:"param1" validate:"omitempty,max=255"`
	Param2       *string `json:"param2" form:"param2" validate:"omitempty,max=255"`
	Param3       *string `json:"param3" form:"param3" validate:"omitempty,max=255"`
	Authority    string  `json:"authority" form:"authority" validate:"required"`                // 权限(多选)TENANT_ADMIN-租户管理员 SYS_ADMIN-系统管理员
	Description  *string `json:"description" form:"description" validate:"omitempty,max=36"`    // 描述
	Multilingual *string `json:"multilingual" form:"multilingual" validate:"omitempty,max=255"` // 多语言
	RoutePath    *string `json:"route_path" form:"route_path" validate:"omitempty,max=255"`     // 路由路径
}

type UiElementsListRsp struct {
	ID           string               `json:"id" form:"id" validate:"required,max=36"`                       //主键
	ParentID     string               `json:"parent_id" form:"parent_id" validate:"required,max=36"`         // 父元素id
	ElementCode  string               `json:"element_code" form:"element_code" validate:"required,max=100"`  // 元素标识符
	ElementType  *int16               `json:"element_type" form:"element_type"  validate:"omitempty,max=10"` // 元素类型1-菜单 2-目录 3-按钮 4-路由
	Orders       *int16               `json:"orders" form:"orders" validate:"omitempty,max=10000"`           // 排序
	Param1       *string              `json:"param1" form:"param1" validate:"omitempty,max=255"`
	Param2       *string              `json:"param2" form:"param2" validate:"omitempty,max=255"`
	Param3       *string              `json:"param3" form:"param3" validate:"omitempty,max=255"`
	Authority    string               `json:"authority" form:"authority" validate:"required"`             // 权限(多选)TENANT_ADMIN-租户管理员 SYS_ADMIN-系统管理员
	Description  *string              `json:"description" form:"description" validate:"omitempty,max=36"` // 描述
	Remark       *string              `json:"remark" form:"remark" validate:"omitempty,max=255"`
	Multilingual *string              `json:"multilingual" form:"multilingual" validate:"omitempty,max=255"` // 多语言
	RoutePath    *string              `json:"route_path" form:"route_path" validate:"omitempty,max=255"`     // 路由路径
	Children     []*UiElementsListRsp `json:"children" form:"children"`
}
type UiElementsListRsp1 struct {
	ID          string                `json:"id" form:"id" validate:"required,max=36"`                       //主键
	ParentID    string                `json:"parent_id" form:"parent_id" validate:"required,max=36"`         // 父元素id
	ElementCode string                `json:"element_code" form:"element_code" validate:"required,max=100"`  // 元素标识符
	ElementType *int16                `json:"element_type" form:"element_type"  validate:"omitempty,max=10"` // 元素类型1-菜单 2-目录 3-按钮 4-路由
	Description *string               `json:"description" form:"description" validate:"omitempty,max=36"`    // 描述
	Children    []*UiElementsListRsp1 `json:"children" form:"children"`
}

type GetUiElementsListByPageReq struct {
	PageReq
}

func (u *SysUIElement) ToRsp() *UiElementsListRsp {
	return &UiElementsListRsp{
		ID:           u.ID,
		ParentID:     u.ParentID,
		ElementCode:  u.ElementCode,
		ElementType:  &u.ElementType,
		Orders:       u.Order_,
		Param1:       u.Param1,
		Param2:       u.Param2,
		Param3:       u.Param3,
		Authority:    u.Authority,
		Description:  u.Description,
		Remark:       u.Remark,
		Multilingual: u.Multilingual,
		RoutePath:    u.RoutePath,
		Children:     []*UiElementsListRsp{},
	}
}
func (u *SysUIElement) ToRsp1() *UiElementsListRsp1 {
	return &UiElementsListRsp1{
		ID:          u.ID,
		ParentID:    u.ParentID,
		ElementCode: u.ElementCode,
		ElementType: &u.ElementType,
		Description: u.Description,
		Children:    []*UiElementsListRsp1{},
	}
}
