package domain

import "time"

// TimeWindow limits a condition group to local weekdays (Monday=1, Sunday=7).
// End is exclusive. An overnight interval belongs to its starting weekday.
type TimeWindow struct {
	Weekdays []int `json:"weekdays"`
	Start    int   `json:"start_second"`
	End      int   `json:"end_second"`
	Offset   int   `json:"utc_offset_minutes"`
}

func (w TimeWindow) valid() bool {
	if len(w.Weekdays) == 0 || w.Start < 0 || w.Start >= 86400 || w.End < 0 || w.End >= 86400 || w.Start == w.End || w.Offset < -840 || w.Offset > 840 {
		return false
	}
	for _, day := range w.Weekdays {
		if day < 1 || day > 7 {
			return false
		}
	}
	return true
}

func (w TimeWindow) contains(now time.Time) bool {
	if !w.valid() {
		return false
	}
	local := now.UTC().Add(time.Duration(w.Offset) * time.Minute)
	second := local.Hour()*3600 + local.Minute()*60 + local.Second()
	if w.Start < w.End {
		if second < w.Start || second >= w.End {
			return false
		}
	} else if second < w.End {
		local = local.AddDate(0, 0, -1)
	} else if second < w.Start {
		return false
	}
	weekday := (int(local.Weekday())+6)%7 + 1
	for _, day := range w.Weekdays {
		if day == weekday {
			return true
		}
	}
	return false
}
