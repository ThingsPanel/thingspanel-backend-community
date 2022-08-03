package valid

import "ThingsPanel-Go/models"

type DataTranspondValidate struct {
	Id          string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"` //
	ProcessId   string `json:"process_id" alias:"流程id" valid:"MaxSize(36)"`
	ProcessType string `json:"process_type" alias:"流程类型" valid:"MaxSize(36)"`
	Label       string `json:"label" alias:"标签" valid:"MaxSize(255)"`
	Disabled    string `json:"disabled" alias:"状态" valid:"MaxSize(10)"`
	Info        string `json:"info"  alias:"Info" valid:"MaxSize(255)"`            //
	Env         string `json:"env" alias:"Env" valid:"MaxSize(999)"`               //
	CustomerId  string `json:"customer_id" alias:"CustomerId" valid:"MaxSize(36)"` //
	CreatedAt   int64  `json:"created_at" alias:"CreatedAt" `                      //
	RoleType    string `json:"role_type" alias:"1-接入引擎 2-数据转发"`
}
type AddDataTranspondValidate struct {
	ProcessId   string `json:"process_id" alias:"流程id" valid:"Required;MaxSize(36)"`
	ProcessType string `json:"process_type" alias:"流程类型" valid:"MaxSize(36)"`
	Label       string `json:"label" alias:"标签" valid:"MaxSize(255)"`
	Disabled    string `json:"disabled" alias:"状态" valid:"MaxSize(10)"`
	Info        string `json:"info"  alias:"..." valid:"MaxSize(255)"`      //
	Env         string `json:"env" alias:"..." valid:"MaxSize(999)"`        //
	CustomerId  string `json:"customer_id" alias:"..." valid:"MaxSize(36)"` //
	CreatedAt   int64  `json:"created_at" alias:"..."`                      //
	RoleType    string `json:"role_type" alias:"1-接入引擎 2-数据转发"`
}

type PaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(36)"`
	Disabled    string `json:"disabled" alias:"状态" valid:"MaxSize(10)"`
	ProcessType string `json:"process_type" alias:"流程类型" valid:"MaxSize(36)"`
	RoleType    string `json:"role_type" alias:"1-接入引擎 2-数据转发"`
}

type RspPaginationValidate struct {
	CurrentPage int                    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                    `json:"per_page"  alias:"每页页数" valid:"Required;Max(36)"`
	Data        []models.DataTranspond `json:"data" alias:"返回数据" valid:"MaxSize(10)"`
	Total       int64                  `json:"total" alias:"总数" valid:"MaxSize(36)"`
}
