package models

type TpAutomationCondition struct {
	Id                  string `json:"id"  gorm:"primaryKey"`
	AutomationId        string `json:"automation_id,omitempty"`
	GroupNumber         int64  `json:"group_number,omitempty"`   // 小组编号
	ConditionType       string `json:"condition_type,omitempty"` // 条件类型1-设备条件 2-时间条件
	DeviceId            string `json:"device_id,omitempty"`
	TimeConditionType   string `json:"time_condition_type,omitempty"`   // 时间条件类型0-时间范围 1-单次 2-重复
	DeviceConditionType string `json:"device_condition_type,omitempty"` // 设备条件类型
	V1                  string `json:"v1,omitempty"`
	V2                  string `json:"v2,omitempty"`
	V3                  string `json:"v3,omitempty"`
	V4                  string `json:"v4,omitempty"`
	V5                  string `json:"v5,omitempty"`
	Remark              string `json:"remark,omitempty"`
}

/*
||v1|v2|v3|v4|v5|v6|
|-|-|-|-|-|-|-|
|时间条件-时间范围|起始时间|结束时间
|时间条件-重复|1-每小时|cronID|mm|
|时间条件-重复|2-每天|cronID|HH:mm|
|时间条件-重复|3-每周|cronID|周几|HH:mm|
|时间条件-重复|4-每月|cronID|周几|dd:HH:mm|
|时间条件-重复|5-自定义cron|cronID|
|时间条件-单次（执行后自动删除）|yyyy-MM-dd HH:mm:ss|
|设备-触发方式|1-属性|操作符|数值|
|设备-触发方式|2-事件|数值|
|设备-触发方式|3-在线离线状态|1-上线 2-下线 3-上下线|
*/
func (t *TpAutomationCondition) TableName() string {
	return "tp_automation_condition"
}
