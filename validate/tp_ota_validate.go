package valid

import "ThingsPanel-Go/models"

type TpOtaValidate struct {
	Id                 string `json:"id" gorm:"primaryKey" valid:"Required;MaxSize(36)"`
	PackageName        string `json:"package_name,omitempty" ailas:"升级包名称" valid:"Required;MaxSize(100)"`
	PackageVersion     string `json:"package_version,omitempty" ailas:"升级包版本号" valid:"Required;MaxSize(20)"`
	PackageModule      string `json:"package_module,omitempty" ailas:"升级包模块" valid:"Required;MaxSize(64)"`
	ProductId          string `json:"product_id,omitempty" ailas:"产品ID" valid:"MaxSize(36)"`
	SignatureAlgorithm string `json:"signature_algorithm,omitempty" ailas:"签名算法" valid:"Required;MaxSize(50)"` //签名算法
	PackageUrl         string `json:"package_url,omitempty" ailas:"升级包url" valid:"MaxSize(255)"`
	Description        string `json:"description,omitempty" ailas:"描述" valid:"MaxSize(255)"`
	AdditionalInfo     string `json:"additional_info,omitempty"  ailas:"其他配置"`
	CreatedAt          int64  `json:"created_at,omitempty" ailas:"创建日期"`
	Sign               string `json:"sign,omitempty"  ailas:"其他配置"`
}
type AddTpOtaValidate struct {
	PackageName        string `json:"package_name,omitempty" ailas:"升级包名称" valid:"Required;MaxSize(36)"`
	PackageVersion     string `json:"package_version,omitempty" ailas:"升级包版本号" valid:"Required;MaxSize(36)"`
	PackageModule      string `json:"package_module,omitempty" ailas:"升级包模块"`
	ProductId          string `json:"product_id,omitempty" ailas:"产品ID" valid:"Required;MaxSize(36)"`
	SignatureAlgorithm string `json:"signature_algorithm,omitempty" ailas:"签名算法" valid:"Required;MaxSize(36)"` //签名算法
	PackageUrl         string `json:"package_url,omitempty" ailas:"升级包url"`
	PackagePath        string `json:"package_path,omitempty" ailas:"升级包url"`
	Description        string `json:"description,omitempty" ailas:"描述"`
	AdditionalInfo     string `json:"additional_info,omitempty" ailas:"其他配置"`
	CreatedAt          int64  `json:"created_at,omitempty" ailas:"创建日期"`
}
type TpOtaPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	PackageName string `json:"package_name,omitempty" alias:"升级包名称" valid:"MaxSize(99)"`
	Id          string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
	ProductId   string `json:"product_id,omitempty" alias:"product_id" valid:"MaxSize(36)"`
}

type RspTpOtaPaginationValidate struct {
	CurrentPage int            `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int            `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpOta `json:"data" alias:"返回数据"`
	Total       int64          `json:"total" alias:"总数" valid:"Max(10000)"`
}
type TpOtaIdValidate struct {
	Id string `json:"id,omitempty"   gorm:"primaryKey"  alias:"Id" valid:"MaxSize(36)"`
}
