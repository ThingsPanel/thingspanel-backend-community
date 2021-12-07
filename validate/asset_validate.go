package valid

// AddAsset 校验
type AddAsset struct {
	Data string `json:"data" alias:"参数" valid:"Required"`
}

// EditAsset 校验
type EditAsset struct {
	Data string `json:"data" alias:"参数" valid:"Required"`
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
	BusinessID string `json:"business_id" alias:"业务ID" valid:"Required; MaxSize(36)"` // 业务ID
}
