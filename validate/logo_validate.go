package valid

type LogoValidate struct {
	Id         string `json:"id"  alias:"ID" valid:"MaxSize(36)"`             // ID
	SystemName string `json:"system_name"  alias:"系统名称" valid:"MaxSize(255)"` // 系统名称
	Theme      string `json:"theme"  alias:"主题" valid:"MaxSize(99)"`          // 主题
	LogoOne    string `json:"logo_one"  alias:"首页logo" valid:"MaxSize(255)"`  // 首页logo
	LogoTwo    string `json:"logo_two"  alias:"缓冲logo" valid:"MaxSize(255)"`  // 缓冲logo
	LogoThree  string `json:"logo_three"   valid:"MaxSize(255)"`
	CustomId   string `json:"custom_id"  valid:"MaxSize(99)"`
	Remark     string `json:"remark"  valid:"MaxSize(255)"`
}
