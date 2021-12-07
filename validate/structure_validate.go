package valid

// StructureAdd 校验
type StructureAdd struct {
	Data string `json:"data" alias:"数据" valid:"Required;"`
}

// StructureList 校验
type StructureList struct {
	ID string `json:"id" alias:"ID" valid:"Required;"`
}

// StructureUpdate 校验
type StructureUpdate struct {
	Data string `json:"data" alias:"数据" valid:"Required;"`
}

// StructureDelete
type StructureDelete struct {
	ID string `json:"id" alias:"ID" valid:"Required;"`
}

//StructureField
type StructureField struct {
	Field string `json:"field" alias:"字段" valid:""`
}
