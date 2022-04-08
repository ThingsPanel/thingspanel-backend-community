package valid

// AddFieldMapping 校验
type AddFieldMapping struct {
	Data string `json:"data" alias:"参数" valid:"Required"`
}

type FieldMapping struct {
	Data []struct {
		ID        string `json:"id"`
		DeviceID  string `json:"device_id"`
		FieldFrom string `json:"field_from"`
		FieldTo   string `json:"field_to"`
		Symbol    string `json:"symbol"`
	} `json:"data"`
}

type UpdateFieldMapping struct {
	Data []struct {
		ID        string `json:"id" valid:"Required"`
		DeviceID  string `json:"device_id"`
		FieldFrom string `json:"field_from"`
		FieldTo   string `json:"field_to"`
		Symbol    string `json:"symbol"`
	} `json:"data"`
}
