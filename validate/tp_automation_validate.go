package valid

import "ThingsPanel-Go/models"

type TpAutomation struct {
	Id                  string `json:"id"  gorm:"primaryKey"`
	TenantId            string `json:"tenant_id,omitempty"`
	AutomationName      string `json:"automation_name,omitempty"`
	AutomationDescribed string `json:"automation_described,omitempty"`
	UpdateTime          int64  `json:"update_time,omitempty"`
	CreatedAt           int64  `json:"created_at,omitempty"`
	CreatedBy           string `json:"created_by,omitempty"`
	Priority            int64  `json:"priority,omitempty"` //优先级|1-100越小越高
	Enabled             string `json:"enabled,omitempty"`  //启用状态 |0-未开启 1-已开启
	Remark              string `json:"remark,omitempty"`
}

type TpAutomationValidate struct {
	Id                   string                          `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
	TenantId             string                          `json:"tenant_id,omitempty" valid:"MaxSize(36)"`
	AutomationName       string                          `json:"automation_name,omitempty" valid:"MaxSize(99)"`
	AutomationDescribed  string                          `json:"automation_described,omitempty" valid:"MaxSize(999)"`
	CreatedBy            string                          `json:"created_by,omitempty" valid:"MaxSize(36)"`
	UpdateTime           int64                           `json:"update_time,omitempty"`
	Priority             int64                           `json:"priority,omitempty"`                   //优先级|1-100越小越高
	Enabled              string                          `json:"enabled,omitempty" valid:"MaxSize(1)"` //启用状态 |0-未开启 1-已开启
	Remark               string                          `json:"remark,omitempty" valid:"MaxSize(255)"`
	AutomationConditions []TpAutomationConditionValidate `json:"automation_conditions,omitempty" valid:"Required"`
	AutomationActions    []TpAutomationActionValidate    `json:"automation_actions,omitempty" valid:"Required"`
}

type AddTpAutomationValidate struct {
	Id                   string                             `json:"id"  gorm:"primaryKey" valid:"MaxSize(36)"`
	TenantId             string                             `json:"tenant_id,omitempty" valid:"MaxSize(36)"`
	AutomationName       string                             `json:"automation_name,omitempty" valid:"MaxSize(99)"`
	AutomationDescribed  string                             `json:"automation_described,omitempty" valid:"MaxSize(999)"`
	CreatedBy            string                             `json:"created_by,omitempty" valid:"MaxSize(36)"`
	CreatedAt            int64                              `json:"created_at,omitempty"`
	UpdateTime           int64                              `json:"update_time,omitempty"`
	Priority             int64                              `json:"priority,omitempty"`                   //优先级|1-100越小越高
	Enabled              string                             `json:"enabled,omitempty" valid:"MaxSize(1)"` //启用状态 |0-未开启 1-已开启
	Remark               string                             `json:"remark,omitempty" valid:"MaxSize(255)"`
	AutomationConditions []AddTpAutomationConditionValidate `json:"automation_conditions,omitempty" valid:"Required"`
	AutomationActions    []AddTpAutomationActionValidate    `json:"automation_actions,omitempty" valid:"Required"`
}

type TpAutomationPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Enabled     string `json:"enabled,omitempty" alias:"启用状态" valid:"MaxSize(99)"`
	Id          string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpAutomationPaginationValidate struct {
	CurrentPage int                   `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                   `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpAutomation `json:"data" alias:"返回数据"`
	Total       int64                 `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpAutomationIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}
