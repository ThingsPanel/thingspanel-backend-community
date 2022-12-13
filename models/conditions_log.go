package models

type ConditionsLog struct {
	ID            string `json:"id" gorm:"primaryKey,size:36"`
	DeviceId      string `json:"device_id" gorm:"size:36"`     // 设备ID
	OperationType string `json:"operation_type" gorm:"size:2"` // 操作类型1-定时触发 2-手动控制 3-自动控制
	Instruct      string `json:"instruct" gorm:"size:255"`     // 指令
	Sender        string `json:"sender" gorm:"size:99"`        // 发送者
	SendResult    string `json:"send_result" gorm:"size:2"`    //发送结果1-成功 2-失败
	Respond       string `json:"respond" gorm:"size:255"`      //设备反馈
	CteateTime    string `json:"cteate_time" gorm:"size:50"`
	ProtocolType  string `json:"protocol_type" gorm:"size:10"` //mqtt,tcp
	Remark        string `json:"remark" gorm:"size:2"`
}

func (ConditionsLog) TableName() string {
	return "conditions_log"
}
