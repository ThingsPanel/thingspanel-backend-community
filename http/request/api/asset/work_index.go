package asset

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
)

// WorkIndexRequest : list work request
type WorkIndexRequest struct {
	request.Request
	// WorkName : name of the work
	WorkName string `json:"work_name"`
}

// Encode : defines the logic for encoding list work request
func (a *WorkIndexRequest) Encode() ([]byte, error) {
	byt, err := json.Marshal(a)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : defines the logic for decoding  list work request
func (a *WorkIndexRequest) Decode(in []byte) error {
	err := json.Unmarshal(in, a)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
