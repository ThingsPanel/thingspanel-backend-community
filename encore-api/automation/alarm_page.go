package automation

import (
	"encore.app/automation/store"
	"encore.dev/beta/errs"
	"time"
)

func alarmPageFilter(req *ListAlarmsRequest) (store.EpisodeFilter, error) {
	f := store.EpisodeFilter{PolicyID: req.PolicyID, SubjectID: req.SubjectID, State: req.State, Level: req.Level, Limit: req.Limit, Offset: req.Offset}
	invalid := func() (store.EpisodeFilter, error) {
		return f, &errs.Error{Code: errs.InvalidArgument, Message: "invalid alarm pagination or filters"}
	}
	if req.Offset < 0 || req.Limit < 0 || req.Limit > 200 {
		return invalid()
	}
	if req.State != "" && req.State != "active" && req.State != "acked" && req.State != "cleared" {
		return invalid()
	}
	if req.Level != "" && req.Level != "H" && req.Level != "M" && req.Level != "L" && req.Level != "N" {
		return invalid()
	}
	for _, v := range []struct {
		raw  string
		dest **time.Time
	}{{req.StartTime, &f.StartTime}, {req.EndTime, &f.EndTime}} {
		if v.raw != "" {
			parsed, err := time.Parse(time.RFC3339Nano, v.raw)
			if err != nil {
				return invalid()
			}
			*v.dest = &parsed
		}
	}
	if f.StartTime != nil && f.EndTime != nil && f.StartTime.After(*f.EndTime) {
		return invalid()
	}
	return f, nil
}
