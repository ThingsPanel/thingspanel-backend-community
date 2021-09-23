package auth

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
)

// LoginRequest : defines the user login request body
type LoginRequest struct {
	request.Request `json:"-"`
	// Email : user email used for registering with ThingPanel
	Email string `json:"email"`
	// Password : user password in plain-text format
	Password string `json:"password"`
}

// Encode : defines the serialization logic for login request
func (l *LoginRequest) Encode() ([]byte, error) {
	byt, err := json.Marshal(l)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : defines the deserialization logic for login request
func (l *LoginRequest) Decode(in []byte) (err error) {
	err = json.Unmarshal(in, l)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
