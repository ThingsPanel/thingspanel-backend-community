package models

type TpOta struct {
	Id                 string `json:"id" gorm:"primaryKey"`
	PackageName        string `json:"package_name,omitempty"`
	PackageVersion     string `json:"package_version,omitempty"`
	PackageModule      string `json:"package_module,omitempty"`
	ProductId          string `json:"product_id,omitempty"`
	SignatureAlgorithm string `json:"signature_algorithm,omitempty"` //签名算法
	PackageUrl         string `json:"package_url,omitempty"`
	Description        string `json:"description,omitempty"`
	//OtherConfig        string `json:"other_config,omitempty"`
	CreatedAt int64 `json:"created_at,omitempty"`
}

func (TpOta) TableName() string {
	return "tp_ota"
}
