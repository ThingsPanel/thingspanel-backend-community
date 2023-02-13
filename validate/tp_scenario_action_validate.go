package valid

import "ThingsPanel-Go/models"

type EditTpScenarioActionValidate struct {
	Id                 string `json:"id" gorm:"primaryKey" valid:"MaxSize(36)"`
	ScenarioStrategyId string `json:"scenario_strategy_id,omitempty" valid:"MaxSize(36)"`
	ActionType         string `json:"action_type,omitempty" valid:"MaxSize(2)"`
	DeviceId           string `json:"device_id,omitempty" valid:"MaxSize(36)"`
	DeviceModel        string `json:"device_model,omitempty" valid:"MaxSize(2)"` // 模型类型1-设定属性 2-调动服务
	Instruct           string `json:"instruct,omitempty" valid:"MaxSize(999)"`   // 指令
	Remark             string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type AddTpScenarioActionValidate struct {
	Id                 string `json:"id" gorm:"primaryKey" valid:"MaxSize(36)"`
	ScenarioStrategyId string `json:"scenario_strategy_id,omitempty" valid:"MaxSize(36)"`
	ActionType         string `json:"action_type,omitempty" valid:"MaxSize(2)"`
	DeviceId           string `json:"device_id,omitempty" valid:"MaxSize(36)"`
	DeviceModel        string `json:"device_model,omitempty" valid:"MaxSize(2)"` // 模型类型1-设定属性 2-调动服务
	Instruct           string `json:"instruct,omitempty" valid:"MaxSize(999)"`   // 指令
	Remark             string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type TpScenarioActionPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Id          string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpScenarioActionPaginationValidate struct {
	CurrentPage int                       `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                       `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpScenarioAction `json:"data" alias:"返回数据"`
	Total       int64                     `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpScenarioActionIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}
