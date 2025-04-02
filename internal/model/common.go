package model

import (
	"encoding/json"

	"github.com/sirupsen/logrus"
)

// 分页请求参数
type PageReq struct {
	Page     int `json:"page" form:"page" validate:"required,gte=1"`                    // 页码
	PageSize int `json:"page_size" form:"page_size" validate:"required,gte=1,lte=1000"` // 每页数量
}

// PutMessage
// @DESCRIPTION:公用下发入参
type PutMessage struct {
	DeviceID string `json:"device_id" form:"device_id" validate:"required,max=36"`
	Value    string `json:"value" form:"value" validate:"required,max=9999"`
}

type PutMessageForCommand struct {
	DeviceID string  `json:"device_id" form:"device_id" validate:"required,max=36"`
	Value    *string `json:"value" form:"value" validate:"omitempty,max=9999"`
	Identify string  `json:"identify" form:"identify" validate:"required,max=255"`
}

type ParamID struct {
	ID string `query:"id" form:"id" json:"id" validate:"required"`
}

const OPEN = "OPEN"
const CLOSE = "CLOSE"

// 对于前端传入的部分无法定义固定结构的参数，例如：products.AdditionalInfo
// 使用 *json.RawMessage 来接收，并且将其转化为数据库可存储的 string
// 同时去除 json string 中多余的空格
func JsonRawMessage2Str(in *json.RawMessage) (str string, err error) {
	var data map[string]interface{}
	err = json.Unmarshal([]byte(*in), &data)
	if err != nil {
		return str, err
	}
	compactJson, err := json.Marshal(data)
	if err != nil {
		logrus.Error(err)
		return str, err
	}
	str = string(compactJson)
	return
}
