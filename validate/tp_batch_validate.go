package valid

import "ThingsPanel-Go/models"

type TpBatchValidate struct {
	Id            string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
	BatchNumber   string `json:"batch_number,omitempty" alias:"批次编号" valid:"MaxSize(36)"`
	ProductId     string `json:"product_id,omitempty" alias:"产品id" valid:"MaxSize(36)"`
	DeviceNumber  int    `json:"device_number,omitempty"  alias:"设备数量"`
	GenerateFlag  string `json:"generate_flag,omitempty" alias:"生成标志 0-未生成 1-已生成" valid:"MaxSize(36)"`
	Describle     string `json:"describle,omitempty" alias:"描述" valid:"MaxSize(255)"`
	CreatedTime   int64  `json:"created_time,omitempty"`
	Remark        string `json:"remark,omitempty" alias:"备注" valid:"MaxSize(255)"`
	AccessAddress string `json:"access_address,omitempty" alias:"接入地址" valid:"MaxSize(36)"`
}

type AddTpBatchValidate struct {
	BatchNumber   string `json:"batch_number,omitempty" alias:"批次编号" valid:"Required;MaxSize(36)"`
	ProductId     string `json:"product_id,omitempty" alias:"产品id" valid:"Required;MaxSize(36)"`
	DeviceNumber  int    `json:"device_number,omitempty"  alias:"设备数量"`
	GenerateFlag  string `json:"generate_flag,omitempty" alias:"生成标志 0-未生成 1-已生成" valid:"MaxSize(36)"`
	Describle     string `json:"describle,omitempty" alias:"描述" valid:"MaxSize(255)"`
	CreatedTime   int64  `json:"created_time,omitempty"`
	Remark        string `json:"remark,omitempty" alias:"备注" valid:"MaxSize(255)"`
	AccessAddress string `json:"access_address,omitempty" alias:"接入地址" valid:"MaxSize(36)"`
}

type TpBatchPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	BatchNumber string `json:"batch_number,omitempty" alias:"批次编号" valid:"MaxSize(99)"`
	Id          string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpBatchPaginationValidate struct {
	CurrentPage int              `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int              `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpBatch `json:"data" alias:"返回数据"`
	Total       int64            `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpBatchIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}
