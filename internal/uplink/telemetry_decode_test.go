package uplink

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"project/internal/processor"
)

type decodeProcessorStub struct {
	calls int
	input *processor.DecodeInput
	out   *processor.DecodeOutput
	err   error
}

func (s *decodeProcessorStub) Decode(_ context.Context, input *processor.DecodeInput) (*processor.DecodeOutput, error) {
	s.calls++
	s.input = input
	return s.out, s.err
}

func (*decodeProcessorStub) Encode(context.Context, *processor.EncodeInput) (*processor.EncodeOutput, error) {
	return nil, errors.New("unexpected Encode call")
}

func TestDecodeTelemetryPayloadWithoutDeviceConfigPassesRawPayloadThrough(t *testing.T) {
	raw := []byte(`{"temperature":21.5,"state":"on"}`)
	for _, configID := range []*string{nil, ptrString("")} {
		stub := &decodeProcessorStub{}
		got, output, err := decodeTelemetryPayload(context.Background(), stub, configID, raw, 123)
		if err != nil {
			t.Fatalf("decodeTelemetryPayload error = %v", err)
		}
		if output != nil || !bytes.Equal(got, raw) {
			t.Fatalf("got payload=%s output=%v, want unchanged payload and no output", got, output)
		}
		if stub.calls != 0 {
			t.Fatalf("Decode calls = %d, want 0 without a device config", stub.calls)
		}
	}
}

func TestDecodeTelemetryPayloadWithDeviceConfigRetainsDecodeBehavior(t *testing.T) {
	configID := "config-1"
	raw := []byte(`{"raw":1}`)
	decoded := []byte(`{"normalized":2}`)
	stub := &decodeProcessorStub{out: &processor.DecodeOutput{Success: true, Data: decoded}}

	got, output, err := decodeTelemetryPayload(context.Background(), stub, &configID, raw, 123)
	if err != nil {
		t.Fatalf("decodeTelemetryPayload error = %v", err)
	}
	if !bytes.Equal(got, decoded) || output == nil || !output.Success {
		t.Fatalf("got payload=%s output=%+v, want decoded payload", got, output)
	}
	if stub.calls != 1 || stub.input.DeviceConfigID != configID || stub.input.Type != processor.DataTypeTelemetry || !bytes.Equal(stub.input.RawData, raw) || stub.input.Timestamp != 123 {
		t.Fatalf("unexpected Decode input/call count: calls=%d input=%+v", stub.calls, stub.input)
	}
}

func TestDecodeTelemetryPayloadPreservesProcessorFailureSignals(t *testing.T) {
	configID := "config-1"
	decodeErr := errors.New("decode failed")
	t.Run("decode error", func(t *testing.T) {
		stub := &decodeProcessorStub{err: decodeErr}
		_, _, err := decodeTelemetryPayload(context.Background(), stub, &configID, []byte(`{}`), 0)
		if !errors.Is(err, decodeErr) {
			t.Fatalf("error = %v, want %v", err, decodeErr)
		}
	})
	t.Run("execution failure remains visible to caller", func(t *testing.T) {
		failure := errors.New("script returned false")
		stub := &decodeProcessorStub{out: &processor.DecodeOutput{Success: false, Error: failure}}
		_, output, err := decodeTelemetryPayload(context.Background(), stub, &configID, []byte(`{}`), 0)
		if err != nil || output == nil || output.Success || !errors.Is(output.Error, failure) {
			t.Fatalf("output=%+v error=%v, want Success=false preserved", output, err)
		}
	})
	t.Run("nil output is rejected", func(t *testing.T) {
		stub := &decodeProcessorStub{}
		_, _, err := decodeTelemetryPayload(context.Background(), stub, &configID, []byte(`{}`), 0)
		if err == nil || err.Error() != "processor returned no telemetry output" {
			t.Fatalf("error = %v, want nil-output error", err)
		}
	})
}

func ptrString(value string) *string { return &value }
