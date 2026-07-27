//go:build go1.18

package strftime_test

import (
	"strings"
	"testing"

	"github.com/ncruces/go-strftime"
)

func FuzzFormat(f *testing.F) {
	for _, test := range timeTests {
		f.Add(test.format)
	}

	f.Fuzz(func(t *testing.T, format string) {
		str := strftime.Format(format, reference)
		if str == "" && format != "" {
			t.Errorf("Format(%q) = %q", format, str)
		}
		if str != format && !strings.Contains(format, "%") {
			t.Errorf("Format(%q) = %q", format, str)
		}
	})
}

func FuzzParse(f *testing.F) {
	for _, test := range timeTests {
		f.Add(test.format, strftime.Format(test.format, reference))
	}

	f.Fuzz(func(t *testing.T, format, value string) {
		parsed, err := strftime.Parse(format, value)
		if err != nil && !parsed.IsZero() {
			t.Errorf("Parse(%q, %q) = (%v, %v)", format, value, parsed, err)
		}
	})
}

func FuzzLayout(f *testing.F) {
	for _, test := range timeTests {
		f.Add(test.format)
	}

	f.Fuzz(func(t *testing.T, format string) {
		layout, err := strftime.Layout(format)
		if err != nil && layout != "" {
			t.Errorf("Layout(%q) = %v", format, err)
		}
		if err == nil && layout == "" && format != "" {
			t.Errorf("Layout(%q) = (%q, %v)", format, layout, err)
		}
	})
}

func FuzzUTS35(f *testing.F) {
	for _, test := range timeTests {
		f.Add(test.format)
	}

	f.Fuzz(func(t *testing.T, format string) {
		pattern, err := strftime.UTS35(format)
		if err != nil && pattern != "" {
			t.Errorf("UTS35(%q) = %v", format, err)
		}
		if err == nil && pattern == "" && format != "" {
			t.Errorf("UTS35(%q) = (%q, %v)", format, pattern, err)
		}
	})
}
