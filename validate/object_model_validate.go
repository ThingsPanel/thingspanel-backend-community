package valid

import "ThingsPanel-Go/models"

type ObjectModelValidate struct {
	Id             string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
	Sort           int64  `json:"sort,omitempty"`
	ObjectDescribe string `json:"object_describe,omitempty" valid:"MaxSize(255)"`
	ObjectName     string `json:"object_name,omitempty" valid:"MaxSize(99)"` // 物模型名称
	ObjectType     string `json:"object_type,omitempty" valid:"MaxSize(36)"` // 物模型类型
	ObjectData     string `json:"object_data,omitempty"`                     // 物模型json
	CreatedAt      int64  `json:"created_at,omitempty"`
	Remark         string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type AddObjectModelValidate struct {
	Sort           int64  `json:"sort,omitempty"`
	ObjectDescribe string `json:"object_describe,omitempty" valid:"MaxSize(255)"`
	ObjectName     string `json:"object_name,omitempty" valid:"Required;MaxSize(99)"` // 物模型名称
	ObjectType     string `json:"object_type,omitempty" valid:"Required;MaxSize(36)"` // 物模型类型
	ObjectData     string `json:"object_data,omitempty"`                              // 物模型json
	CreatedAt      int64  `json:"created_at,omitempty"`
	Remark         string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type ObjectModelPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	ObjectType  string `json:"object_type" alias:"插件类型" valid:"MaxSize(36)"`
	Id          string `json:"id" alias:"id" valid:"MaxSize(36)"`
}

type RspObjectModelPaginationValidate struct {
	CurrentPage int                  `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                  `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.ObjectModel `json:"data" alias:"返回数据"`
	Total       int64                `json:"total" alias:"总数" valid:"Max(10000)"`
}
