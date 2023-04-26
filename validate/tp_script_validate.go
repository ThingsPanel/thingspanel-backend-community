package valid

import "ThingsPanel-Go/models"

type TpScriptValidate struct {
	Id             string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
	ProtocolType   string `json:"protocol_type,omitempty" alias:"协议类型" valid:"MaxSize(99)"`
	ScriptName     string `json:"script_name" alias:"脚本名称" valid:"Required;MaxSize(99)"`
	Company        string `json:"company,omitempty" alias:"公司名称" valid:"MaxSize(99)"`
	ProductName    string `json:"product_name,omitempty" alias:"产品名称" valid:"MaxSize(99)"`
	ScriptContentA string `json:"script_content_a,omitempty" alias:"下行脚本内容" valid:"MaxSize(10000)"`
	ScriptContentB string `json:"script_content_b,omitempty" alias:"上行脚本内容" valid:"MaxSize(10000)"`
	CreatedAt      int64  `json:"created_at,omitempty" alias:"创建时间"`
	ScriptType     string `json:"script_type,omitempty" alias:"脚本类型" valid:"MaxSize(99)"`
	Remark         string `json:"remark,omitempty" alias:"备注" valid:"MaxSize(255)"`
	DeviceType     string `json:"device_type,omitempty" alias:"设备类型" valid:"MaxSize(36)"`
}

type AddTpScriptValidate struct {
	ProtocolType   string `json:"protocol_type,omitempty" alias:"协议类型" valid:"MaxSize(99)"`
	ScriptName     string `json:"script_name" alias:"脚本名称" valid:"Required;MaxSize(99)"`
	Company        string `json:"company,omitempty" alias:"公司名称" valid:"MaxSize(99)"`
	ProductName    string `json:"product_name,omitempty" alias:"产品名称" valid:"MaxSize(99)"`
	ScriptContentA string `json:"script_content_a,omitempty" alias:"下行脚本内容" valid:"MaxSize(10000)"`
	ScriptContentB string `json:"script_content_b,omitempty" alias:"上行脚本内容" valid:"MaxSize(10000)"`
	CreatedAt      int64  `json:"created_at,omitempty" alias:"创建时间"`
	ScriptType     string `json:"script_type,omitempty" alias:"脚本类型" valid:"MaxSize(99)"`
	Remark         string `json:"remark,omitempty" alias:"备注" valid:"MaxSize(255)"`
	DeviceType     string `json:"device_type,omitempty" alias:"设备类型" valid:"Required;MaxSize(36)"`
}

type TpScriptPaginationValidate struct {
	CurrentPage  int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage      int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	ProtocolType string `json:"protocol_type,omitempty" alias:"协议类型" valid:"MaxSize(99)"`
	DeviceType   string `json:"device_type,omitempty" alias:"设备类型" valid:"MaxSize(36)"`
	Id           string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpScriptPaginationValidate struct {
	CurrentPage int               `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int               `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpScript `json:"data" alias:"返回数据"`
	Total       int64             `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpScriptIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}
type TpScriptTestValidate struct {
	ScriptContent string `json:"script_content,omitempty" alias:"下行脚本内容" valid:"MaxSize(10000)"`
	MsgContent    string `json:"msg_content,omitempty" alias:"下行消息内容" valid:"MaxSize(10000)"`
}
