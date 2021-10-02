package asset

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
	"github.com/google/uuid"
)

// DeleteWorkRequest : delete a work
type DeleteWorkRequest struct {
	request.Request
	// ID : work id
	ID uuid.UUID `json:"id"`
}

// Encode : serialize the list delete work request
func (l *DeleteWorkRequest) Encode() ([]byte, error) {
	byt, err := json.Marshal(l)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : deserialize the list delete work request
func (l *DeleteWorkRequest) Decode(in []byte) error {
	err := json.Unmarshal(in, l)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
