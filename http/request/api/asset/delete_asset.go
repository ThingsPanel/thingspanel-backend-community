package asset

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
)

// DeleteAssetRequest : delete a asset
type DeleteAssetRequest struct {
	request.Request
	ID string `json:"id"`
}

// Encode : defines the logic for encoding delete asset request
func (d *DeleteAssetRequest) Encode() ([]byte, error) {
	byt, err := json.Marshal(d)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : defines the logic for decoding  delete asset request
func (d *DeleteAssetRequest) Decode(in []byte) error {
	err := json.Unmarshal(in, d)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
