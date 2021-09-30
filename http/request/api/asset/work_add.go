package asset

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
)

// AddWorkRequest : add a new job/work to business page
type AddWorkRequest struct {
	request.Request
	// Name : name of the work/job
	Name string `json:"name"`
}

// Encode : defines the logic for encoding add work request
func (a *AddWorkRequest)Encode()([]byte,error){
	byt, err := json.Marshal(a)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : defines the logic for decoding  add work request
func (a *AddWorkRequest)Decode(in []byte)error{
	err := json.Unmarshal(in, a)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}