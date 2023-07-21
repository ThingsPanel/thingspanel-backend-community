package valid

import "ThingsPanel-Go/models"

type OpenApiPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Id          string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
	Name        string `json:"name,omitempty" alias:"名称"  valid:"MaxSize(50)"`
}

type RspOpenapiAuthPaginationValidate struct {
	CurrentPage int                    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpOpenapiAuth `json:"data" alias:"返回数据"`
	Total       int64                  `json:"total" alias:"总数" valid:"Max(10000)"`
}

type AddOpenapiAuthValidate struct {
	Name              string `json:"name,omitempty" alias:"名称"  valid:"Required;MaxSize(36)"`
	SignatureMode     string `json:"signature_mode,omitempty" alias:"签名方式"  valid:"Required;MaxSize(50)"`
	IpWhitelist       string `json:"ip_white_list,omitempty" alias:"ip白名单"  valid:"MaxSize(500)"`
	DeviceAccessScope string `json:"device_access_scope,omitempty" alias:"设备访问范围" valid:"Required;MaxSize(2)"`
	ApiAccessScope    string `json:"api_access_scope,omitempty" alias:"接口访问范围"  valid:"Required;MaxSize(2)"`
	Description       string `json:"description,omitempty" alias:"描述"  valid:"MaxSize(255)"`
}

type OpenapiAuthValidate struct {
	Id                string `json:"id,omitempty" alias:"id"  valid:"Required;MaxSize(36)"`
	Name              string `json:"name,omitempty" alias:"名称"  valid:"Required;MaxSize(36)"`
	SignatureMode     string `json:"signature_mode,omitempty" alias:"签名方式"  valid:"Required;MaxSize(50)"`
	IpWhitelist       string `json:"ip_white_list,omitempty" alias:"ip白名单"  valid:"MaxSize(500)"`
	DeviceAccessScope string `json:"device_access_scope,omitempty" alias:"设备访问范围" valid:"Required;MaxSize(2)"`
	ApiAccessScope    string `json:"api_access_scope,omitempty" alias:"接口访问范围"  valid:"Required;MaxSize(2)"`
	Description       string `json:"description,omitempty" alias:"描述"  valid:"MaxSize(255)"`
}

type ApiPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Id          string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
	Name        string `json:"name,omitempty" alias:"名称"  valid:"MaxSize(50)"`
	ApiType     string `json:"api_type,omitempty" alias:"接口类型"  valid:"MaxSize(20)"`
	ServiceType string `json:"api_type,omitempty" alias:"服务类型"  valid:"MaxSize(2)"`
}

type ApiValidate struct {
	Id          string `json:"id,omitempty" alias:"id"  valid:"Required;MaxSize(36)"`
	Name        string `json:"name,omitempty" alias:"名称"  valid:"Required;MaxSize(36)"`
	Url         string `json:"url,omitempty" alias:"url"  valid:"Required;MaxSize(500)"`
	ApiType     string `json:"ip_white_list,omitempty" alias:"接口类型"  valid:"Required,MaxSize(20)"`
	ServiceType string `json:"device_access_scope,omitempty" alias:"服务类型" valid:"Required;MaxSize(2)"`
	Remark      string `json:"remark,omitempty" alias:"描述"  valid:"MaxSize(255)"`
}

type AddApiValidate struct {
	Name        string `json:"name,omitempty" alias:"名称"  valid:"Required;MaxSize(36)"`
	Url         string `json:"url,omitempty" alias:"签名方式"  valid:"Required;MaxSize(500)"`
	ApiType     string `json:"api_type,omitempty" alias:"ip白名单"  valid:"Required,MaxSize(20)"`
	ServiceType string `json:"service_type,omitempty" alias:"设备访问范围" valid:"Required;MaxSize(2)"`
	Remark      string `json:"remark,omitempty" alias:"描述"  valid:"MaxSize(255)"`
}

type RspApiPaginationValidate struct {
	CurrentPage int            `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int            `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpApi `json:"data" alias:"返回数据"`
	Total       int64          `json:"total" alias:"总数" valid:"Max(10000)"`
}

// 接口授权关系
type AddROpenApiValidate struct {
	TpOpenapiAuthId string   `json:"tp_openapi_auth_id,omitempty" alias:"授权id"  valid:"Required;MaxSize(36)"`
	TpApiId         []string `json:"tp_api_id,omitempty" alias:"接口id"  valid:"Required;"`
}

// 接口授权关系
type ROpenApiValidate struct {
	Id              string   `json:"id,omitempty" alias:"id"  valid:"Required;MaxSize(36)"`
	TpOpenapiAuthId string   `json:"tp_openapi_auth_id,omitempty" alias:"授权id"  valid:"Required;MaxSize(36)"`
	TpApiId         []string `json:"tp_api_id,omitempty" alias:"接口id"  valid:"Required;"`
}

// 设备授权关系
type RDeviceValidate struct {
	Id              string   `json:"id,omitempty" alias:"id"  valid:"Required;MaxSize(36)"`
	TpOpenapiAuthId string   `json:"tp_openapi_auth_id,omitempty" alias:"授权id"  valid:"Required;MaxSize(36)"`
	DeviceId        []string `json:"device_id,omitempty" alias:"设备id"  valid:"Required;"`
}
