package encoders

type Encoder interface {
	Encode() ([]byte, error)
	Decode(in []byte) error
}
