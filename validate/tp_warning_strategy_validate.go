package valid

import "ThingsPanel-Go/models"

type TpWarningStrategy struct {
	Id                  string `json:"id"  gorm:"primaryKey"`
	WarningStrategyName string `json:"warning_strategy_name,omitempty"`
	WarningLevel        string `json:"warning_level,omitempty"` // 告警级别
	RepeatCount         int64  `json:"repeat_count,omitempty"`  // 重复次数
	TriggerCount        int64  `json:"trigger_count,omitempty"` // 已触发次数
	InformWay           string `json:"inform_way,omitempty"`    // 通知方式
	Remark              string `json:"remark,omitempty"`
}

type TpWarningStrategyValidate struct {
	Id                  string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
	WarningStrategyName string `json:"warning_strategy_name,omitempty" valid:"MaxSize(99)"`
	WarningLevel        string `json:"warning_level,omitempty" valid:"MaxSize(2)"` // 告警级别
	RepeatCount         int64  `json:"repeat_count,omitempty"`                     // 重复次数
	TriggerCount        int64  `json:"trigger_count,omitempty"`                    // 已触发次数
	InformWay           string `json:"inform_way,omitempty" valid:"MaxSize(99)"`   // 通知方式
	Remark              string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type AddTpWarningStrategyValidate struct {
	Id                  string `json:"id"  gorm:"primaryKey" valid:"MaxSize(36)"`
	WarningStrategyName string `json:"warning_strategy_name,omitempty" valid:"MaxSize(99)"`
	WarningLevel        string `json:"warning_level,omitempty" valid:"MaxSize(2)"` // 告警级别
	RepeatCount         int64  `json:"repeat_count,omitempty"`                     // 重复次数
	TriggerCount        int64  `json:"trigger_count,omitempty"`                    // 已触发次数
	InformWay           string `json:"inform_way,omitempty" valid:"MaxSize(99)"`   // 通知方式
	Remark              string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type TpWarningStrategyPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Id          string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpWarningStrategyPaginationValidate struct {
	CurrentPage int                        `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                        `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpWarningStrategy `json:"data" alias:"返回数据"`
	Total       int64                      `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpWarningStrategyIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}
