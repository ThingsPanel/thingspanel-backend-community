package valid

// OperationLog 校验
type ConditionsLogListValidate struct {
	Current       int    `json:"current_page" alias:"页码" valid:"Required;Min(1)"`
	Size          int    `json:"per_page" alias:"条数" valid:"Required;Min(10)"`
	DeviceId      string `json:"device_id" alias:"设备id" valid:"MaxSize(36)"`
	OperationType string `json:"operation_type" alias:"操作类型" valid:"MaxSize(2)"`
	SendResult    string `json:"send_result" alias:"发送结果" valid:"MaxSize(2)"`
	BusinessId    string `json:"business_id" alias:"业务id" valid:"MaxSize(36)"`
	AssetId       string `json:"asset_id" alias:"资产id" valid:"MaxSize(36)"`
	BusinessName  string `json:"business_name" alias:"业务名" valid:"MaxSize(255)"`
	AssetName     string `json:"asset_name" alias:"资产名" valid:"MaxSize(255)"`
	DeviceName    string `json:"device_name" alias:"设备名" valid:"MaxSize(255)"`
	UserName      string `json:"user_name" alias:"操作用户" valid:"MaxSize(255)"`
}
