package downlink

import (
	"context"
	"errors"
	"project/internal/processor"
)

// PrepareCommand returns the encoded bytes synchronously. Durable dispatch owns
// the receipt transition; this method never changes a legacy log or starts I/O.
func (h *Handler) PrepareCommand(ctx context.Context, msg *Message) ([]byte, error) {
	if msg == nil || msg.Type != MessageTypeCommand || msg.DeviceNumber == "" || len(msg.Data) == 0 {
		return nil, ErrInvalidMessage
	}
	if msg.DeviceConfigID == "" {
		return append([]byte(nil), msg.Data...), nil
	}
	if h.processor == nil {
		return nil, errors.New("command encoder unavailable")
	}
	out, err := h.processor.Encode(ctx, &processor.EncodeInput{DeviceConfigID: msg.DeviceConfigID, Type: processor.DataTypeCommand, Data: msg.Data})
	if err != nil {
		return nil, err
	}
	if out == nil || !out.Success || len(out.EncodedData) == 0 {
		return nil, errors.New("command encode failed")
	}
	return out.EncodedData, nil
}
