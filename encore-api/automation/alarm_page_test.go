package automation

import "testing"

func TestAlarmPageFilterValidation(t *testing.T) {
	for _, r := range []ListAlarmsRequest{
		{Offset: -1}, {Limit: 201}, {Level: "invalid"}, {State: "invalid"}, {StartTime: "not-a-date"},
		{StartTime: "2026-11-11T00:00:00Z", EndTime: "2026-11-10T00:00:00Z"},
	} {
		if _, err := alarmPageFilter(&r); err == nil {
			t.Fatalf("accepted invalid filter: %+v", r)
		}
	}
	f, err := alarmPageFilter(&ListAlarmsRequest{Limit: 10, Offset: 10, Level: "H", StartTime: "2026-11-05T08:00:00+08:00", EndTime: "2026-11-05T01:00:00Z"})
	if err != nil || f.StartTime.Hour() != 8 || f.Offset != 10 {
		t.Fatalf("valid filter: %+v %v", f, err)
	}
}
