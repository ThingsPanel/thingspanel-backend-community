package asset

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
	"github.com/google/uuid"
)

// EditWorkRequest : edit the work/job
type EditWorkRequest struct {
	request.Request
	// ID : work id
	ID uuid.UUID `json:"id"`
	// Name : work name
	Name string `json:"name"`
}

// Encode : serialize the edit work request
func (l *EditWorkRequest) Encode() ([]byte, error) {
	byt, err := json.Marshal(l)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : deserialize the edit work request
func (l *EditWorkRequest) Decode(in []byte) error {
	err := json.Unmarshal(in, l)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
