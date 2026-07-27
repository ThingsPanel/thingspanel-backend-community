// These tests were adapted from 3rd party sources.

package strftime_test

import (
	"testing"
	"time"

	"github.com/ncruces/go-strftime"
)

func TestFormat_rubydoc(t *testing.T) {
	// https://ruby-doc.org/stdlib-2.6.1/libdoc/date/rdoc/DateTime.html#method-i-strftime
	reference := time.Date(2007, 11, 19, 8, 37, 48, 0, time.FixedZone("", -6*3600))
	tests := []struct {
		format string
		time   string
	}{
		{"Printed on %m/%d/%Y", "Printed on 11/19/2007"},
		{"at %I:%M%p", "at 08:37AM"},
		// Various ISO 8601 formats:
		{"%Y%m%d", "20071119"},                           // Calendar date (basic)
		{"%F", "2007-11-19"},                             // Calendar date (extended)
		{"%Y-%m", "2007-11"},                             // Calendar date, reduced accuracy, specific month
		{"%Y", "2007"},                                   // Calendar date, reduced accuracy, specific year
		{"%C", "20"},                                     // Calendar date, reduced accuracy, specific century
		{"%Y%j", "2007323"},                              // Ordinal date (basic)
		{"%Y-%j", "2007-323"},                            // Ordinal date (extended)
		{"%GW%V%u", "2007W471"},                          // Week date (basic)
		{"%G-W%V-%u", "2007-W47-1"},                      // Week date (extended)
		{"%GW%V", "2007W47"},                             // Week date, reduced accuracy, specific week (basic)
		{"%G-W%V", "2007-W47"},                           // Week date, reduced accuracy, specific week (extended)
		{"%H%M%S", "083748"},                             // Local time (basic)
		{"%T", "08:37:48"},                               // Local time (extended)
		{"%H%M", "0837"},                                 // Local time, reduced accuracy, specific minute (basic)
		{"%H:%M", "08:37"},                               // Local time, reduced accuracy, specific minute (extended)
		{"%H", "08"},                                     // Local time, reduced accuracy, specific hour
		{"%H%M%S,%L", "083748,000"},                      // Local time with decimal fraction, comma as decimal sign (basic)
		{"%T,%L", "08:37:48,000"},                        // Local time with decimal fraction, comma as decimal sign (extended)
		{"%H%M%S.%L", "083748.000"},                      // Local time with decimal fraction, full stop as decimal sign (basic)
		{"%T.%L", "08:37:48.000"},                        // Local time with decimal fraction, full stop as decimal sign (extended)
		{"%H%M%S%z", "083748-0600"},                      // Local time and the difference from UTC (basic)
		{"%T%:z", "08:37:48-06:00"},                      // Local time and the difference from UTC (extended)
		{"%Y%m%dT%H%M%S%z", "20071119T083748-0600"},      // Date and time of day for calendar date (basic)
		{"%FT%T%:z", "2007-11-19T08:37:48-06:00"},        // Date and time of day for calendar date (extended)
		{"%Y%jT%H%M%S%z", "2007323T083748-0600"},         // Date and time of day for ordinal date (basic)
		{"%Y-%jT%T%:z", "2007-323T08:37:48-06:00"},       // Date and time of day for ordinal date (extended)
		{"%GW%V%uT%H%M%S%z", "2007W471T083748-0600"},     // Date and time of day for week date (basic)
		{"%G-W%V-%uT%T%:z", "2007-W47-1T08:37:48-06:00"}, // Date and time of day for week date (extended)
		{"%Y%m%dT%H%M", "20071119T0837"},                 // Calendar date and local time (basic)
		{"%FT%R", "2007-11-19T08:37"},                    // Calendar date and local time (extended)
		{"%Y%jT%H%MZ", "2007323T0837Z"},                  // Ordinal date and UTC of day (basic)
		{"%Y-%jT%RZ", "2007-323T08:37Z"},                 // Ordinal date and UTC of day (extended)
		{"%GW%V%uT%H%M%z", "2007W471T0837-0600"},         // Week date and local time and difference from UTC (basic)
		{"%G-W%V-%uT%R%:z", "2007-W47-1T08:37-06:00"},    // Week date and local time and difference from UTC (extended)
	}

	for _, test := range tests {
		if got := strftime.Format(test.format, reference); got != test.time {
			t.Errorf("Format(%q) = %q, want %q", test.format, got, test.time)
		}
	}
}

func TestFormat_tebeka(t *testing.T) {
	// github.com/tebeka/strftime
	// github.com/hhkbp2/go-strftime
	reference := time.Date(2009, time.November, 10, 23, 1, 2, 3, time.UTC)
	tests := []struct {
		format string
		time   string
	}{
		{"%a", "Tue"},
		{"%A", "Tuesday"},
		{"%b", "Nov"},
		{"%B", "November"},
		{"%c", "Tue Nov 10 23:01:02 2009"}, // we use a different format
		{"%d", "10"},
		{"%H", "23"},
		{"%I", "11"},
		{"%j", "314"},
		{"%m", "11"},
		{"%M", "01"},
		{"%p", "PM"},
		{"%S", "02"},
		{"%U", "45"},
		{"%w", "2"},
		{"%W", "45"},
		{"%x", "11/10/09"},
		{"%X", "23:01:02"},
		{"%y", "09"},
		{"%Y", "2009"},
		{"%Z", "UTC"},
		{"%L", "000"},       // we use a different specifier
		{"%f", "000000"},    // we use a different specifier
		{"%N", "000000003"}, // we use a different specifier

		// Escape
		{"%%%Y", "%2009"},
		{"%3%%", "%3%"},
		{"%3%L", "%3000"},     // we use a different specifier
		{"%3xy%L", "%3xy000"}, // we use a different specifier

		// Embedded
		{"/path/%Y/%m/report", "/path/2009/11/report"},

		// Empty
		{"", ""},
	}

	for _, test := range tests {
		if got := strftime.Format(test.format, reference); got != test.time {
			t.Errorf("Format(%q) = %q, want %q", test.format, got, test.time)
		}
	}
}

func TestFormat_fastly(t *testing.T) {
	// github.com/fastly/go-utils/strftime
	timezone, err := time.LoadLocation("MST")
	if err != nil {
		t.Skip("could not load timezone:", err)
	}

	reference := time.Unix(1136239445, 0).In(timezone)

	tests := []struct {
		format string
		time   string
	}{
		{"", ``},

		// invalid formats
		{"%", `%`},
		{"%^", `%^`},
		{"%^ ", `%^ `},
		{"%^ x", `%^ x`},
		{"x%^ x", `x%^ x`},

		// supported locale-invariant formats
		{"%a", `Mon`},
		{"%A", `Monday`},
		{"%b", `Jan`},
		{"%B", `January`},
		{"%C", `20`},
		{"%d", `02`},
		{"%D", `01/02/06`},
		{"%e", ` 2`},
		{"%F", `2006-01-02`},
		{"%G", `2006`},
		{"%g", `06`},
		{"%h", `Jan`},
		{"%H", `15`},
		{"%I", `03`},
		{"%j", `002`},
		{"%k", `15`},
		{"%l", ` 3`},
		{"%m", `01`},
		{"%M", `04`},
		{"%n", "\n"},
		{"%p", `PM`},
		{"%r", `03:04:05 PM`},
		{"%R", `15:04`},
		{"%s", `1136239445`},
		{"%S", `05`},
		{"%t", "\t"},
		{"%T", `15:04:05`},
		{"%u", `1`},
		{"%U", `01`},
		{"%V", `01`},
		{"%w", `1`},
		{"%W", `01`},
		{"%x", `01/02/06`},
		{"%X", `15:04:05`},
		{"%y", `06`},
		{"%Y", `2006`},
		{"%z", `-0700`},
		{"%Z", `MST`},
		{"%%", `%`},

		// supported locale-varying formats
		{"%c", `Mon Jan  2 15:04:05 2006`},
		{"%E", `%E`},
		{"%EF", `%EF`},
		{"%O", `%O`},
		{"%OF", `%OF`},
		{"%P", `pm`},
		{"%+", `Mon Jan  2 15:04:05 MST 2006`},
		{
			"%a|%A|%b|%B|%c|%C|%d|%D|%e|%E|%EF|%F|%G|%g|%h|%H|%I|%j|%k|%l|%m|%M|%O|%OF|%p|%P|%r|%R|%s|%S|%t|%T|%u|%U|%V|%w|%W|%x|%X|%y|%Y|%z|%Z|%%",
			`Mon|Monday|Jan|January|Mon Jan  2 15:04:05 2006|20|02|01/02/06| 2|%E|%EF|2006-01-02|2006|06|Jan|15|03|002|15| 3|01|04|%O|%OF|PM|pm|03:04:05 PM|15:04|1136239445|05|	|15:04:05|1|01|01|1|01|01/02/06|15:04:05|06|2006|-0700|MST|%`,
		},
	}

	for _, test := range tests {
		if got := strftime.Format(test.format, reference); got != test.time {
			t.Errorf("Format(%q) = %q, want %q", test.format, got, test.time)
		}
	}
}

func TestFormat_jehiah(t *testing.T) {
	// github.com/jehiah/go-strftime
	reference := time.Unix(1340244776, 0).UTC()
	tests := []struct {
		format string
		time   string
	}{
		{"%Y-%m-%d %H:%M:%S", "2012-06-21 02:12:56"},
		{"aaabbb0123456789%Y", "aaabbb01234567892012"},
		{"%H:%M:%S.%L", "02:12:56.000"}, // jehiah disagrees with Ruby on this one
		{"%0%1%%%2", "%0%1%%2"},
	}

	for _, test := range tests {
		if got := strftime.Format(test.format, reference); got != test.time {
			t.Errorf("Format(%q) = %q, want %q", test.format, got, test.time)
		}
	}
}

func TestFormat_lestrrat(t *testing.T) {
	// github.com/lestrrat-go/strftime
	reference := time.Unix(1136239445, 123456789).UTC()
	tests := []struct {
		format string
		time   string
	}{
		{
			`%A %a %B %b %C %c %D %d %e %F %H %h %I %j %k %l %M %m %n %p %R %r %S %T %t %U %u %V %v %W %w %X %x %Y %y %Z %z`,
			"Monday Mon January Jan 20 Mon Jan  2 22:04:05 2006 01/02/06 02  2 2006-01-02 22 Jan 10 002 22 10 04 01 \n PM 22:04 10:04:05 PM 05 22:04:05 \t 01 1 01  2-Jan-2006 01 1 22:04:05 01/02/06 2006 06 UTC +0000",
		},
	}

	for _, test := range tests {
		if got := strftime.Format(test.format, reference); got != test.time {
			t.Errorf("Format(%q) = %q, want %q", test.format, got, test.time)
		}
	}
}
