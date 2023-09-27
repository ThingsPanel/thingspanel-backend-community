package valid

import "ThingsPanel-Go/models"

type EditTpScenarioStrategyValidate struct {
	Id                   string                         `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
	TenantId             string                         `json:"tenant_id,omitempty" valid:"MaxSize(36)"`
	ScenarioName         string                         `json:"scenario_name,omitempty" valid:"MaxSize(99)"`         // 场景名称
	ScenarioDescription  string                         `json:"scenario_description,omitempty" valid:"MaxSize(999)"` // 场景描述
	CreatedBy            string                         `json:"created_by,omitempty" valid:"MaxSize(36)"`
	UpdateTime           int64                          `json:"update_time,omitempty"`
	Remark               string                         `json:"remark,omitempty" valid:"MaxSize(255)"`
	AddTpScenarioActions []EditTpScenarioActionValidate `json:"scenario_actions,omitempty" valid:"Required"`
}

type AddTpScenarioStrategyValidate struct {
	Id                   string                        `json:"id"  gorm:"primaryKey" valid:"MaxSize(36)"`
	TenantId             string                        `json:"tenant_id,omitempty" valid:"MaxSize(36)"`
	ScenarioName         string                        `json:"scenario_name,omitempty" valid:"MaxSize(99)"`         // 场景名称
	ScenarioDescription  string                        `json:"scenario_description,omitempty" valid:"MaxSize(999)"` // 场景描述
	CreatedAt            int64                         `json:"created_at,omitempty"`
	CreatedBy            string                        `json:"created_by,omitempty" valid:"MaxSize(36)"`
	UpdateTime           int64                         `json:"update_time,omitempty"`
	Remark               string                        `json:"remark,omitempty" valid:"MaxSize(255)"`
	AddTpScenarioActions []AddTpScenarioActionValidate `json:"scenario_actions,omitempty" valid:"Required"`
}

type TpScenarioStrategyPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Id          string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpScenarioStrategyPaginationValidate struct {
	CurrentPage int                         `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                         `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpScenarioStrategy `json:"data" alias:"返回数据"`
	Total       int64                       `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpScenarioStrategyIdValidate struct {
	Id string `json:"id" alias:"id" gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}
