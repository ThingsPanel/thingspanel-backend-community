package models

type TpWarningStrategy struct {
	Id                  string `json:"id"  gorm:"primaryKey"`
	WarningStrategyName string `json:"warning_strategy_name,omitempty"`
	WarningLevel        string `json:"warning_level,omitempty"` // 告警级别
	RepeatCount         int64  `json:"repeat_count,omitempty"`  // 重复次数
	TriggerCount        int64  `json:"trigger_count,omitempty"` // 已触发次数
	InformWay           string `json:"inform_way,omitempty"`    // 通知方式
	Remark              string `json:"remark,omitempty"`
}

func (t *TpWarningStrategy) TableName() string {
	return "tp_warning_strategy"
}
