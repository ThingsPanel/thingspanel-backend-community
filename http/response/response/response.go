package response

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
)

// SuccessResponse : common api response body
type SuccessResponse struct {
	Code    int         `json:"code"`
	Data    interface{} `json:"data"`
	Message string      `json:"message"`
}

//
func (s *SuccessResponse) Encode(in interface{}) (out []byte, err error) {
	byt, err := json.Marshal(s)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

func (s *SuccessResponse) Decode(in []byte) (err error) {
	err = json.Unmarshal(in, s)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
