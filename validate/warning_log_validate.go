package valid

// WarningLogListValidate 校验
type WarningLogListValidate struct {
	StartDate string `json:"start_date" alias:"开始时间" valid:"MaxSize(36)"`
	EndDate   string `json:"end_date" alias:"结束时间" valid:"MaxSize(36)"`
	Page      int    `json:"page" alias:"页码" valid:"Min(1)"`
	Limit     int    `json:"limit" alias:"条数" valid:"Min(10)"`
}
