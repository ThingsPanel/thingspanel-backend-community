package uplink

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func TestParseResponseRequiresKnownResult(t *testing.T) {
	processor := NewResponseUplink(ResponseUplinkConfig{Logger: logrus.New()})
	defer processor.Stop()
	tests := []struct {
		payload string
		wantOK  bool
	}{
		{payload: `{"result":0,"message":"success"}`, wantOK: true},
		{payload: `{"result":1,"message":"rejected"}`, wantOK: false},
		{payload: `{"message":"success"}`, wantOK: false},
		{payload: `{"result":2}`, wantOK: false},
	}
	for _, tt := range tests {
		_, ok := processor.parseResponse([]byte(tt.payload))
		if ok != tt.wantOK {
			t.Errorf("parseResponse(%s) success=%v, want %v", tt.payload, ok, tt.wantOK)
		}
	}
}
