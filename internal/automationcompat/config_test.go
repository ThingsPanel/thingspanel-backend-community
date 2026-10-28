package automationcompat

import (
	"strings"
	"testing"
	"time"
)

func TestOffIsTheDefaultAndNeedsNothing(t *testing.T) {
	t.Setenv(EnvMode, "")
	t.Setenv(EnvCoreURL, "")
	t.Setenv(EnvSunset, "")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("off must not require configuration: %v", err)
	}
	if cfg.Mode != ModeOff {
		t.Fatalf("the default is off, got %q", cfg.Mode)
	}
}

// TestTheAdapterCannotRunWithoutAnEndDate is the requirement, enforced.
//
// A compatibility layer without an expiry date is a second implementation that
// nobody removes. Refusing to start is the only version of that rule that
// survives a busy release.
func TestTheAdapterCannotRunWithoutAnEndDate(t *testing.T) {
	t.Setenv(EnvMode, string(ModeShadow))
	t.Setenv(EnvCoreURL, "http://127.0.0.1:4000")
	t.Setenv(EnvSunset, "")

	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("a shadow mount without a sunset date must be refused")
	} else if !strings.Contains(err.Error(), EnvSunset) {
		t.Fatalf("the error must name the missing variable, got %v", err)
	}
}

func TestTheAdapterCannotRunWithoutACoreURL(t *testing.T) {
	t.Setenv(EnvMode, string(ModeTakeover))
	t.Setenv(EnvCoreURL, "")
	t.Setenv(EnvSunset, "2027-04-30")

	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("a mount without an upstream must be refused")
	}
}

func TestAnUnknownModeIsRefused(t *testing.T) {
	t.Setenv(EnvMode, "on")
	t.Setenv(EnvCoreURL, "http://127.0.0.1:4000")
	t.Setenv(EnvSunset, "2027-04-30")

	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal(`"on" is not a mode; the adapter must say so instead of guessing`)
	}
}

func TestABareDateMeansTheWholeDay(t *testing.T) {
	t.Setenv(EnvMode, string(ModeShadow))
	t.Setenv(EnvCoreURL, "http://127.0.0.1:4000")
	t.Setenv(EnvSunset, "2027-04-30")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	want := time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC)
	if !cfg.Sunset.Equal(want) {
		t.Fatalf("2027-04-30 means the adapter serves that whole day, got %s", cfg.Sunset)
	}
}

func TestRelativeURLsAreRefused(t *testing.T) {
	t.Setenv(EnvMode, string(ModeShadow))
	t.Setenv(EnvCoreURL, "/v1")
	t.Setenv(EnvSunset, "2027-04-30")

	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("a relative upstream URL would only fail at request time")
	}
}
