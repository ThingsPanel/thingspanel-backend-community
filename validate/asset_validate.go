package valid

type Asset struct {
	ID             string `json:"id" gorm:"primarykey"`
	AdditionalInfo string `json:"additional_info" gorm:"type:longtext"`
	CustomerID     string `json:"customer_id" gorm:"size:36"` // 客户ID
	Name           string `json:"name"`                       // 名称
	Label          string `json:"label"`                      // 标签
	SearchText     string `json:"search_text"`
	Type           string `json:"type"`                       // 类型
	ParentID       string `json:"parent_id" gorm:"size:36"`   // 父级ID
	Tier           int64  `json:"tier"`                       // 层级
	BusinessID     string `json:"business_id" gorm:"size:36"` // 业务ID
}

// AddAsset 校验
type AddAsset struct {
	Data string `json:"data" alias:"参数" valid:"Required"`
}

// EditAsset 校验
type EditAsset struct {
	Data string `json:"data" alias:"参数" valid:"Required"`
}

// WidgetAsset 校验
type GetAsset struct {
	AssetId string `json:"asset_id" alias:"参数"`
}

// WidgetAsset 校验
type WidgetAsset struct {
	ID string `json:"id" alias:"参数"`
}

// DeleteAsset 校验
type DeleteAsset struct {
	ID   string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
	TYPE int    `json:"type" alias:"type" valid:"Required;"`
}

// ListAsset 校验
type ListAsset struct {
	BusinessID string `json:"business_id" alias:"业务ID" valid:"Required; MaxSize(36)"` // 业务ID
}

// PropertyAsset
type PropertyAsset struct {
	BusinessID string `json:"wid" alias:"业务ID" valid:"Required; MaxSize(36)"` // 业务ID
}
