package automation

import (
	"context"
	"encore.app/automation/store"
	"encore.dev/beta/errs"
	"fmt"
	"time"
)

// AlarmHistoryRequest filters episodes by occurrence time. Recovery updates an
// episode; it never removes it from history or counts it as another occurrence.
type AlarmHistoryRequest struct {
	SubjectID string `query:"subject_id"`
	State     string `query:"state"`
	Level     string `query:"level"`
	StartTime string `query:"start_time"`
	EndTime   string `query:"end_time"`
	Page      int    `query:"page"`
	PageSize  int    `query:"page_size"`
}

type AlarmHistoryResponse struct {
	Alarms        []AlarmResponse `json:"alarms"`
	Total         int64           `json:"total"`
	ActiveDevices int64           `json:"active_devices"`
}

func historyQuery(req *AlarmHistoryRequest) (store.EpisodeQuery, error) {
	f := store.EpisodeQuery{SubjectID: req.SubjectID, State: req.State, Level: req.Level}
	if req.State != "" && req.State != "active" && req.State != "cleared" {
		return f, fmt.Errorf("无效的告警状态")
	}
	if req.Level != "" && req.Level != "H" && req.Level != "M" && req.Level != "L" {
		return f, fmt.Errorf("无效的告警等级")
	}
	page, size := req.Page, req.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if page < 1 || page > 1000000 || size < 1 || size > 200 {
		return f, fmt.Errorf("无效的分页参数")
	}
	f.Limit, f.Offset = size, (page-1)*size
	var err error
	if req.StartTime != "" {
		f.Start, err = time.Parse(time.RFC3339, req.StartTime)
		if err != nil {
			return f, fmt.Errorf("开始时间必须为 RFC3339")
		}
	}
	if req.EndTime != "" {
		f.End, err = time.Parse(time.RFC3339, req.EndTime)
		if err != nil {
			return f, fmt.Errorf("结束时间必须为 RFC3339")
		}
	}
	if !f.Start.IsZero() && !f.End.IsZero() && f.Start.After(f.End) {
		return f, fmt.Errorf("开始时间不能晚于结束时间")
	}
	return f, nil
}

// QueryAlarmHistory is the paginated alarm read API, independent of the existing
// bounded episode diagnostic endpoint.
//
//encore:api auth method=GET path=/v1/alarm-history
func (s *Service) QueryAlarmHistory(ctx context.Context, req *AlarmHistoryRequest) (*AlarmHistoryResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	f, err := historyQuery(req)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	page, err := rt.Store().QueryEpisodes(ctx, tenantID, f)
	if err != nil {
		return nil, internal(err)
	}
	out := &AlarmHistoryResponse{Alarms: make([]AlarmResponse, 0, len(page.Episodes)), Total: page.Total, ActiveDevices: page.ActiveDevices}
	for _, ep := range page.Episodes {
		out.Alarms = append(out.Alarms, alarmResponse(ep))
	}
	return out, nil
}
