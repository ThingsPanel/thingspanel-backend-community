package exception

type EncodingError struct {
	error
}

// NewEncodingError : returns a new instance of type EncodingError embedded with the provided error
func NewEncodingError(err error) error {
	return EncodingError{
		err,
	}
}

type DecodingError struct {
	error
}

// NewDecodingError : returns a new instance of type DecodingError embedded with the provided error
func NewDecodingError(err error) error {
	return DecodingError{
		err,
	}
}
