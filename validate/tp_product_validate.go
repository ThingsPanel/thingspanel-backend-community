package valid

import "ThingsPanel-Go/models"

type TpProductValidate struct {
	Id            string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
	Name          string `json:"name,omitempty" alias:"产品名称" valid:"MaxSize(99)"`
	SerialNumber  string `json:"serial_number,omitempty" alias:"产品编号" valid:"MaxSize(99)"`
	ProtocolType  string `json:"protocol_type,omitempty" alias:"产品类型" valid:"MaxSize(36)"`
	AuthType      string `json:"auth_type,omitempty" alias:"认证类型" valid:"MaxSize(36)"`
	Plugin        string `json:"plugin,omitempty" alias:"插件" valid:"MaxSize(10000)"`
	Describe      string `json:"describe,omitempty" alias:"描述" valid:"MaxSize(255)"`
	CreatedTime   int64  `json:"created_time,omitempty"`
	Remark        string `json:"remark,omitempty" alias:"备注" valid:"MaxSize(255)"`
	DeviceModelId string `json:"device_model_id,omitempty" alias:"插件id" valid:"MaxSize(36)"`
}

type AddTpProductValidate struct {
	Name          string `json:"name,omitempty" alias:"产品名称" valid:"Required;MaxSize(99)"`
	SerialNumber  string `json:"serial_number,omitempty" alias:"产品编号" valid:"Required;MaxSize(99)"`
	ProtocolType  string `json:"protocol_type,omitempty" alias:"产品类型" valid:"Required;MaxSize(36)"`
	AuthType      string `json:"auth_type,omitempty" alias:"认证类型" valid:"Required;MaxSize(36)"`
	Plugin        string `json:"plugin,omitempty" alias:"插件" valid:"Required;MaxSize(10000)"`
	Describe      string `json:"describe,omitempty" alias:"描述" valid:"MaxSize(255)"`
	CreatedTime   int64  `json:"created_time,omitempty"`
	Remark        string `json:"remark,omitempty" alias:"备注" valid:"MaxSize(255)"`
	DeviceModelId string `json:"device_model_id,omitempty" alias:"插件id" valid:"MaxSize(36)"`
}

type TpProductPaginationValidate struct {
	CurrentPage  int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage      int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	SerialNumber string `json:"serial_number,omitempty" alias:"产品编号" valid:"MaxSize(99)"`
	Name         string `json:"name,omitempty" alias:"产品名称" valid:"MaxSize(99)"`
	Id           string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpProductPaginationValidate struct {
	CurrentPage int                `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpProduct `json:"data" alias:"返回数据"`
	Total       int64              `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpProductIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}
