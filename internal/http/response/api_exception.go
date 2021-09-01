package response

import "encoding/json"

type apiException struct {
	Code        int           `json:"code"`
	Message     string        `json:"message"`
	Data        []interface{} `json:"data"`
	ErrorFields []interface{} `json:"error_fields"`
}

// NewAPIException APIException constructor
func NewAPIException(code int, message string, data, errorFields []interface{}) apiException {
	exp := apiException{
		Code:        code,
		Message:     message,
		Data:        data,
		ErrorFields: errorFields,
	}

	if len(exp.Message) == 0 {
		// todo - read message from lang.xml
	}
	return exp
}

func (a apiException) GetJsonResponse() (data []byte, err error) {
	return json.Marshal(a)
}
