package service

import "testing"

func TestAutomateEventParamConditionCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		triggerValue string
		actualValue  interface{}
		wantOK       bool
		wantHandled  bool
	}{
		{
			name: "match configured fields and ignore dynamic timestamp",
			triggerValue: `{
				"match_mode":"field",
				"conditions":[
					{"field":"action","operator":"=","value":"up"},
					{"field":"interface","operator":"=","value":"dummy0"}
				]
			}`,
			actualValue: `{"action":"up","interface":"dummy0","timestamp":1780471300}`,
			wantOK:      true,
			wantHandled: true,
		},
		{
			name: "exists matches present field",
			triggerValue: `{
				"match_mode":"field",
				"conditions":[{"field":"timestamp","operator":"exists","value":true}]
			}`,
			actualValue: `{"action":"up","timestamp":1780471300}`,
			wantOK:      true,
			wantHandled: true,
		},
		{
			name: "numeric greater or equal matches",
			triggerValue: `{
				"match_mode":"field",
				"conditions":[{"field":"level","operator":">=","value":80}]
			}`,
			actualValue: `{"level":85,"source":"battery"}`,
			wantOK:      true,
			wantHandled: true,
		},
		{
			name: "missing field with not equal does not match",
			triggerValue: `{
				"match_mode":"field",
				"conditions":[{"field":"action","operator":"!=","value":"down"}]
			}`,
			actualValue: `{"interface":"dummy0"}`,
			wantOK:      false,
			wantHandled: true,
		},
		{
			name:         "legacy full json is not handled by field matcher",
			triggerValue: `{"action":"up","interface":"dummy0","timestamp":1780471251}`,
			actualValue:  `{"action":"up","interface":"dummy0","timestamp":1780471300}`,
			wantOK:       false,
			wantHandled:  false,
		},
		{
			name: "nested dot path matches",
			triggerValue: `{
				"match_mode":"field",
				"conditions":[{"field":"data.status.code","operator":"=","value":"OK"}]
			}`,
			actualValue: `{"data":{"status":{"code":"OK"}}}`,
			wantOK:      true,
			wantHandled: true,
		},
		{
			name: "in matches one scalar array item",
			triggerValue: `{
				"match_mode":"field",
				"conditions":[{"field":"tags","operator":"in","value":["wan","lan"]}]
			}`,
			actualValue: `{"tags":["vpn","lan"]}`,
			wantOK:      true,
			wantHandled: true,
		},
	}

	automate := &Automate{}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotOK, _, gotHandled := automate.automateEventParamConditionCheck(tt.triggerValue, tt.actualValue)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotHandled != tt.wantHandled {
				t.Fatalf("handled = %v, want %v", gotHandled, tt.wantHandled)
			}
		})
	}
}
