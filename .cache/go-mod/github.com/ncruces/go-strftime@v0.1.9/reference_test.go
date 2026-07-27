//go:build reference

package strftime_test

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ncruces/go-strftime"
)

func TestFormat_ruby(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	exe, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip(err)
	}

	ref := reference.Format(time.RFC3339Nano)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ruby := func(format string) func(t *testing.T) {
		return func(t *testing.T) {
			script := fmt.Sprintf("print(DateTime.parse(%q).strftime(%q))", ref, format)
			cmd := exec.CommandContext(ctx, exe, "-e", "require 'date'", "-e", script)
			t.Parallel()

			want, err := cmd.CombinedOutput()
			if err != nil {
				t.Error(err)
			}

			if got := strftime.Format(format, reference); got != string(want) {
				t.Errorf("Format(%q) = %q, ruby wants %q", format, got, string(want))
			}
		}
	}

	for _, test := range timeTests {
		t.Run("", ruby(test.format))
	}
}

func TestFormat_osascript(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	exe, err := exec.LookPath("osascript")
	if err != nil {
		t.Skip(err)
	}

	zone, _ := reference.Zone()
	unix := float64(reference.UnixNano()) / 1e9
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	osascript := func(pattern, format string) func(t *testing.T) {
		return func(t *testing.T) {
			script := fmt.Sprintf(`
				ObjC.import('Cocoa')
				var fmt = $.NSDateFormatter.alloc.init
				fmt.dateFormat = %q
				fmt.timeZone = $.NSTimeZone.timeZoneWithName(%q)
				fmt.locale = $.NSLocale.localeWithLocaleIdentifier("en_US_POSIX");
				fmt.stringFromDate($.NSDate.dateWithTimeIntervalSince1970(%g))
			`, pattern, zone, unix)
			cmd := exec.CommandContext(ctx, exe, "-l", "JavaScript")
			cmd.Stdin = strings.NewReader(script)
			t.Parallel()

			want, err := cmd.CombinedOutput()
			if err != nil {
				t.Error(err)
			}
			want = bytes.TrimSuffix(want, []byte("\n"))

			if got := strftime.Format(format, reference); got != string(want) {
				t.Errorf("Format(%q) = %q, osascript wants %q", format, got, string(want))
			}
		}
	}

	for _, test := range timeTests {
		if test.uts35 != "" {
			t.Run("", osascript(test.uts35, test.format))
		}
	}
}
