package valid

import "ThingsPanel-Go/models"

type TpWarningInformationValidate struct {
	Id                     string `json:"id"  valid:"MaxSize(36)"`
	TenantId               string `json:"tenant_id,omitempty"  valid:"MaxSize(36)"`
	WarningName            string `json:"warning_name,omitempty"  valid:"MaxSize(99)"`
	WarningLevel           string `json:"warning_level,omitempty"  valid:"MaxSize(99)"`            // 告警级别
	WarningDescription     string `json:"warning_description,omitempty"  valid:"MaxSize(36)"`      // 告警描述
	WarningContent         string `json:"warning_content,omitempty"  valid:"MaxSize(2)"`           // 告警内容
	ProcessingResult       string `json:"processing_result,omitempty"  valid:"MaxSize(1)"`         // 处理结果 0-未处理 1-已处理 2-已忽略
	ProcessingInstructions string `json:"processing_instructions,omitempty"  valid:"MaxSize(255)"` // 处理说明
	ProcessingTime         string `json:"processing_time,omitempty"  valid:"MaxSize(50)"`          // 处理时间
	ProcessingPeopleId     string `json:"processing_people_id,omitempty"  valid:"MaxSize(36)"`     // 处理人
	CreatedAt              int64  `json:"created_at,omitempty"`
	Remark                 string `json:"remark,omitempty"  valid:"MaxSize(255)"` // 备注
}

type AddTpWarningInformationValidate struct {
	Id                     string `json:"id"  valid:"MaxSize(36)"`
	TenantId               string `json:"tenant_id,omitempty"  valid:"MaxSize(36)"`
	WarningName            string `json:"warning_name,omitempty"  valid:"MaxSize(99)"`
	WarningLevel           string `json:"warning_level,omitempty"  valid:"MaxSize(99)"`            // 告警级别
	WarningDescription     string `json:"warning_description,omitempty"  valid:"MaxSize(36)"`      // 告警描述
	WarningContent         string `json:"warning_content,omitempty"  valid:"MaxSize(2)"`           // 告警内容
	ProcessingResult       string `json:"processing_result,omitempty"  valid:"MaxSize(1)"`         // 处理结果 0-未处理 1-已处理 2-已忽略
	ProcessingInstructions string `json:"processing_instructions,omitempty"  valid:"MaxSize(255)"` // 处理说明
	ProcessingTime         string `json:"processing_time,omitempty"  valid:"MaxSize(50)"`          // 处理时间
	ProcessingPeopleId     string `json:"processing_people_id,omitempty"  valid:"MaxSize(36)"`     // 处理人
	CreatedAt              int64  `json:"created_at,omitempty"`
	Remark                 string `json:"remark,omitempty"  valid:"MaxSize(255)"` // 备注
}

type TpWarningInformationPaginationValidate struct {
	CurrentPage      int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage          int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	ProcessingResult string `json:"processing_result,omitempty" alias:"处理结果" valid:"MaxSize(1)"`
	StartTime        string `json:"start_time,omitempty" alias:"开始日期" valid:"MaxSize(50)"`
	EndTime          string `json:"end_time,omitempty" alias:"结束日期" valid:"MaxSize(50)"`
	Id               string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpWarningInformationPaginationValidate struct {
	CurrentPage int                           `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                           `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpWarningInformation `json:"data" alias:"返回数据"`
	Total       int64                         `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpWarningInformationIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}

type BatchProcessingValidate struct {
	Id                     []string `json:"id"  valid:"Required"`
	ProcessingResult       string   `json:"processing_result,omitempty"  valid:"Required;MaxSize(1)"` // 处理结果 0-未处理 1-已处理 2-已忽略
	ProcessingInstructions string   `json:"processing_instructions,omitempty"  valid:"MaxSize(255)"`  // 处理说明
}
