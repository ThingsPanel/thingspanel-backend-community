package downlink

import "errors"

var (
	ErrInvalidMessage = errors.New("invalid message")
	ErrEncodeFailed   = errors.New("script encode failed")
	ErrPublishFailed  = errors.New("mqtt publish failed")
)
