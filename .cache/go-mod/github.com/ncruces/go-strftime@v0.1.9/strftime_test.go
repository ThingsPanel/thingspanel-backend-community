package strftime_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/ncruces/go-strftime"
)

var reference = time.Date(2009, 8, 7, 6, 5, 4, 300000000, time.UTC)

var timeTests = []struct {
	format string
	layout string
	uts35  string
	time   string
}{
	// Date and time formats
	{"%c", time.ANSIC, "E MMM d HH:mm:ss yyyy", "Fri Aug  7 06:05:04 2009"},
	{"%+", time.UnixDate, "E MMM d HH:mm:ss zzz yyyy", "Fri Aug  7 06:05:04 UTC 2009"},
	{"%FT%TZ", time.RFC3339[:20], "yyyy-MM-dd'T'HH:mm:ss'Z'", "2009-08-07T06:05:04Z"},
	{"%a %b %e %T %Y", time.ANSIC, "", "Fri Aug  7 06:05:04 2009"},
	{"%a %b %e %T %Z %Y", time.UnixDate, "", "Fri Aug  7 06:05:04 UTC 2009"},
	{"%a %b %d %T %z %Y", time.RubyDate, "E MMM dd HH:mm:ss xx yyyy", "Fri Aug 07 06:05:04 +0000 2009"},
	{"%a %h %d %T %z %Y", time.RubyDate, "E MMM dd HH:mm:ss xx yyyy", "Fri Aug 07 06:05:04 +0000 2009"},
	{"%a, %d %b %Y %T %Z", time.RFC1123, "E, dd MMM yyyy HH:mm:ss zzz", "Fri, 07 Aug 2009 06:05:04 UTC"},
	{"%a, %d %b %Y %T GMT", http.TimeFormat, "E, dd MMM yyyy HH:mm:ss 'GMT'", "Fri, 07 Aug 2009 06:05:04 GMT"},
	{"%Y-%m-%dT%H:%M:%SZ", time.RFC3339[:20], "yyyy-MM-dd'T'HH:mm:ss'Z'", "2009-08-07T06:05:04Z"},
	// Date formats
	{"%v", "_2-Jan-2006", "d-MMM-yyyy", " 7-Aug-2009"},
	{"%F", "2006-01-02", "yyyy-MM-dd", "2009-08-07"},
	{"%D", "01/02/06", "MM/dd/yy", "08/07/09"},
	{"%x", "01/02/06", "MM/dd/yy", "08/07/09"},
	{"%e-%b-%Y", "_2-Jan-2006", "", " 7-Aug-2009"},
	{"%Y-%m-%d", "2006-01-02", "yyyy-MM-dd", "2009-08-07"},
	{"%m/%d/%y", "01/02/06", "MM/dd/yy", "08/07/09"},
	// Time formats
	{"%R", "15:04", "HH:mm", "06:05"},
	{"%T", "15:04:05", "HH:mm:ss", "06:05:04"},
	{"%X", "15:04:05", "HH:mm:ss", "06:05:04"},
	{"%r", "03:04:05 PM", "hh:mm:ss a", "06:05:04 AM"},
	{"%H:%M", "15:04", "HH:mm", "06:05"},
	{"%H:%M:%S", "15:04:05", "HH:mm:ss", "06:05:04"},
	{"%I:%M:%S %p", "03:04:05 PM", "hh:mm:ss a", "06:05:04 AM"},
	// Misc
	{"%g", "", "YY", "09"},
	{"%-EC", "", "", "20"},
	{"%-Od", "2", "d", "7"},
	{"%Ey", "06", "yy", "09"},
	{"%Oy", "06", "yy", "09"},
	{"%:z", "-07:00", "xxx", "+00:00"},
	{"%V/%G", "", "ww/YYYY", "32/2009"},
	{"%-V/%G", "", "w/YYYY", "32/2009"},
	{"%Cth Century Fox", "", "", "20th Century Fox"},
	{"%-d-%-m-%Y", "2-1-2006", "d-M-yyyy", "7-8-2009"},
	{"%-Hh%-Mm%-Ss", "", "H'h'm'm's's'", "6h5m4s"},
	{"%-I o'clock", "3 o'clock", "h 'o''clock'", "6 o'clock"},
	{"%-M past %-I %p", "4 past 3 PM", "m 'past 'h a", "5 past 6 AM"},
	{"%fμs since %T", "", "SSSSSSμ's since 'HH:mm:ss", "300000μs since 06:05:04"},
	{"%Nns since %T", "", "SSSSSSSSS'ns since 'HH:mm:ss", "300000000ns since 06:05:04"},
	{"%-S.%Ls since %R", "5.000s since 15:04", "s.SSS's since 'HH:mm", "4.300s since 06:05"},
	{"%-S,%fs since %R", "5,000000s since 15:04", "s,SSSSSS's since 'HH:mm", "4,300000s since 06:05"},
	{"%-S.%Ns since %R", "5.000000000s since 15:04", "s.SSSSSSSSS's since 'HH:mm", "4.300000000s since 06:05"},
	{"%-B, is month #%-m of the year", "January, is month #1 of the year", "MMMM, 'is month #'M 'of the year'", "August, is month #8 of the year"},
	{"%-d-%b-%Y is day %j of the year", "2-Jan-2006 is day 002 of the year", "d-MMM-yyyy 'is day 'DDD 'of the year'", "7-Aug-2009 is day 219 of the year"},
	{"%-d-%b-%Y is day %-j of the year", "", "d-MMM-yyyy 'is day 'D 'of the year'", "7-Aug-2009 is day 219 of the year"},
	{"%-A, is day #%u of the week", "", "", "Friday, is day #5 of the week"},
	// Parsing
	{"", "", "", ""},
	{"%", "%", "%", "%"},
	{"%%", "%", "%", "%"},
	{"%-", "%-", "%-", "%-"},
	{"%n", "\n", "\n", "\n"},
	{"%t", "\t", "\t", "\t"},
	{"%q", "", "", "%q"},
	{"%-q", "", "", "%-q"},
	{"'", "'", "''", "'"},
	{"0", "", "0", "0"},
	{"9", "", "9", "9"},
	{"100%", "", "100%", "100%"},
	{"Monday", "", "'Monday'", "Monday"},
	{"January", "", "'January'", "January"},
	{"MST", "", "'MST'", "MST"},
	{"AM", "AM", "'AM'", "AM"},
	{"am", "am", "'am'", "am"},
	{"PM", "", "'PM'", "PM"},
	{"pm", "", "'pm'", "pm"},
}

func TestFormat(t *testing.T) {
	for _, test := range timeTests {
		if got := strftime.Format(test.format, reference); got != test.time {
			t.Errorf("Format(%q) = %q, want %q", test.format, got, test.time)
		}
	}
}

func TestFormat_Unix(t *testing.T) {
	tm := time.Unix(123456, 789*int64(time.Millisecond))

	if got := strftime.Format("%s", tm); got != "123456" {
		t.Errorf("Format(%q) = %q, want %q", "%s", got, "123456")
	}

	if got := strftime.Format("%Q", tm); got != "123456789" {
		t.Errorf("Format(%q) = %q, want %q", "%Q", got, "123456789")
	}
}

func TestFormat_Hour(t *testing.T) {
	hours := []struct{ hour12, hour24 string }{
		0:  {"12", " 0"},
		1:  {" 1", " 1"},
		2:  {" 2", " 2"},
		3:  {" 3", " 3"},
		4:  {" 4", " 4"},
		5:  {" 5", " 5"},
		6:  {" 6", " 6"},
		7:  {" 7", " 7"},
		8:  {" 8", " 8"},
		9:  {" 9", " 9"},
		10: {"10", "10"},
		11: {"11", "11"},
		12: {"12", "12"},
		13: {" 1", "13"},
		14: {" 2", "14"},
		15: {" 3", "15"},
		16: {" 4", "16"},
		17: {" 5", "17"},
		18: {" 6", "18"},
		19: {" 7", "19"},
		20: {" 8", "20"},
		21: {" 9", "21"},
		22: {"10", "22"},
		23: {"11", "23"},
	}

	for h := 0; h < len(hours); h++ {
		base := reference.Add(time.Duration(h) * time.Hour)
		want := hours[base.Hour()]
		if got := strftime.Format("%l", base); got != want.hour12 {
			t.Errorf("Format(%q) = %q, want %q", "%l", got, want.hour12)
		}
		if got := strftime.Format("%k", base); got != want.hour24 {
			t.Errorf("Format(%q) = %q, want %q", "%k", got, want.hour24)
		}
	}
}

func TestFormat_Weekday(t *testing.T) {
	weekdays := []struct{ sunday0, sunday7 string }{
		time.Sunday:    {"0", "7"},
		time.Monday:    {"1", "1"},
		time.Tuesday:   {"2", "2"},
		time.Wednesday: {"3", "3"},
		time.Thursday:  {"4", "4"},
		time.Friday:    {"5", "5"},
		time.Saturday:  {"6", "6"},
	}

	for d := 0; d < len(weekdays); d++ {
		base := reference.AddDate(0, 0, d)
		want := weekdays[base.Weekday()]
		if got := strftime.Format("%w", base); got != want.sunday0 {
			t.Errorf("Format(%q) = %q, want %q", "%w", got, want.sunday0)
		}
		if got := strftime.Format("%u", base); got != want.sunday7 {
			t.Errorf("Format(%q) = %q, want %q", "%u", got, want.sunday7)
		}
	}
}

func TestFormat_WeekNumber(t *testing.T) {
	for y := 2000; y < 2020; y++ {
		sunday := "00"
		monday := "00"
		for d := 1; d < 8; d++ {
			base := time.Date(y, time.January, d, 0, 0, 0, 0, time.UTC)

			switch base.Weekday() {
			case time.Sunday:
				sunday = "01"
			case time.Monday:
				monday = "01"
			}

			if got := strftime.Format("%U", base); got != sunday {
				t.Errorf("Format(%q, %d) = %q, want %q", "%U", base.Unix(), got, sunday)
			}
			if got := strftime.Format("%W", base); got != monday {
				t.Errorf("Format(%q, %d) = %q, want %q", "%W", base.Unix(), got, monday)
			}
		}
	}
}

func TestParse(t *testing.T) {
	for _, test := range timeTests {
		if got, err := strftime.Parse(test.format, test.time); err != nil && test.layout != "" {
			t.Errorf("Parse(%q) = %v", test.format, err)
		} else if err != nil && !got.IsZero() {
			t.Errorf("Parse(%q) = %v", test.format, got)
		} else if then := strftime.Format(test.format, got); err == nil && then != test.time {
			t.Errorf("Parse(%q) = %q, want %q", test.format, got, test.time)
		} else if err != nil {
			t.Logf("Parse(%q) = %v", test.format, err)
		}
	}
}

func TestParse_Error(t *testing.T) {
	if got, err := strftime.Parse("%C", ""); err == nil || !got.IsZero() {
		t.Errorf("Parse(%q) = %v", "%C", got)
	}
}

func TestParse_Table(t *testing.T) {
	var parseTests = []struct {
		format string
		time   string
	}{
		{"%FT%T%:z", "2009-08-07T06:05:04.300Z"},
		{"%FT%T%:z", "2009-8-7T6:5:4.3Z"},
		{"%r %D", "06:05:04.3 AM 08/07/09"},
		{"%r %D", "6:5:4.3 AM 8/7/09"},
	}

	for _, test := range parseTests {
		if got, err := strftime.Parse(test.format, test.time); err != nil {
			t.Errorf("Parse(%q) = %v", test.format, err)
		} else if got != reference {
			t.Errorf("Parse(%q) = %q, want %q", test.format, got, test.time)
		}
	}
}

func TestLayout(t *testing.T) {
	for _, test := range timeTests {
		if got, err := strftime.Layout(test.format); err != nil && test.layout != "" {
			t.Errorf("Layout(%q) = %v", test.format, err)
		} else if got != test.layout {
			t.Errorf("Layout(%q) = %q, want %q", test.format, got, test.layout)
		} else if err != nil {
			t.Logf("Layout(%q) = %v", test.format, err)
		}
	}
}

func TestLayout_Format(t *testing.T) {
	for _, test := range timeTests {
		if test.layout == "" {
			continue
		}
		if got := reference.Format(test.layout); got != test.time {
			t.Errorf("Format(%q) = %q, want %q", test.layout, got, test.time)
		}
	}
}

func TestUTS35(t *testing.T) {
	for _, test := range timeTests {
		if got, err := strftime.UTS35(test.format); err != nil && test.uts35 != "" {
			t.Errorf("UTS35(%q) = %v", test.format, err)
		} else if got != test.uts35 {
			t.Errorf("UTS35(%q) = %q, want %q", test.format, got, test.uts35)
		} else if err != nil {
			t.Logf("UTS35(%q) = %v", test.format, err)
		}
	}
}
