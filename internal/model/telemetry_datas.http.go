package model

type GetTelemetryHistoryDataReq struct {
	DeviceID  string `json:"device_id" form:"device_id" validate:"required,max=36"`
	Key       string `json:"key" form:"key" validate:"required,max=255"`
	StartTime int64  `json:"start_time" form:"start_time" validate:"required"`
	EndTime   int64  `json:"end_time" form:"end_time"  validate:"required"`
}

type DeleteTelemetryDataReq struct {
	DeviceID string `json:"device_id" form:"device_id" validate:"required,max=36"`
	Key      string `json:"key" form:"key" validate:"required,max=255"`
}

type GetTelemetryCurrentDataKeysReq struct {
	DeviceID string   `json:"device_id" form:"device_id" validate:"required,max=36"`
	Keys     []string `json:"key" form:"keys" validate:"required,max=255"`
}

type GetTelemetryHistoryDataByPageReq struct {
	DeviceID    string `json:"device_id" form:"device_id" validate:"required,max=36"`
	Key         string `json:"key" form:"key" validate:"required,max=255"`
	StartTime   int64  `json:"start_time" form:"start_time" validate:"required"`
	EndTime     int64  `json:"end_time" form:"end_time"  validate:"required"`
	ExportExcel *bool  `json:"export_excel" form:"export_excel" validate:"omitempty"`
}

type GetTelemetrySetLogsListByPageReq struct {
	PageReq
	DeviceId      string  `json:"device_id" form:"device_id" validate:"required,max=36"`               // 设备ID
	Status        *string `json:"status" form:"status" validate:"omitempty,oneof=1 2" `                //状态 1-发送成功 2-失败
	OperationType *string `json:"operation_type" form:"operation_type" validate:"omitempty,oneof=1 2"` //操作类型 1-手动操作 2-自动触发

}

type SimulationTelemetryDataReq struct {
	Command string `json:"command" form:"command" validate:"required,max=500"` // mosquitto_pub 命令
}

type ServeEchoDataReq struct {
	DeviceId string `json:"device_id" form:"device_id" validate:"required,max=36"` // 设备ID
}

type GetTelemetryStatisticReq struct {
	DeviceId          string `json:"device_id" form:"device_id" validate:"required,max=36"` // 设备ID
	Key               string `json:"key" form:"key" validate:"required"`
	StartTime         int64  `json:"start_time" form:"start_time" validate:"omitempty"`                         // 开始时间
	EndTime           int64  `json:"end_time" form:"end_time" validate:"omitempty"`                             // 结束时间
	TimeRange         string `json:"time_range" form:"time_range" validate:"required"`                          // 时间范围
	AggregateWindow   string `json:"aggregate_window" form:"aggregate_window" validate:"required"`              // 聚合间隔
	AggregateFunction string `json:"aggregate_function" form:"aggregate_function" validate:"omitempty,max=255"` // 聚合方法
	IsExport          bool   `json:"is_export" form:"is_export" validate:"omitempty"`                           // 是否导出
}
