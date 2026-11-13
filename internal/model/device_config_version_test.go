package model

import (
	"errors"
	"strings"
	"testing"
)

func TestDeviceConfigCondition(t *testing.T) {
	c := &DeviceConfig{ID: "a", TenantID: "tenant", Name: "original"}
	tag, err := DeviceConfigETag(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"", tag, "*", `"other", ` + tag} {
		cond, e := ParseDeviceConfigCondition(v)
		if e != nil || cond.Check(c) != nil {
			t.Fatalf("valid condition rejected: %q", v)
		}
	}
	for _, v := range []string{`W/` + tag, `"other"`} {
		cond, e := ParseDeviceConfigCondition(v)
		if e != nil || !errors.Is(cond.Check(c), ErrDeviceConfigVersionConflict) {
			t.Fatalf("strong comparison failed: %q", v)
		}
	}
	for _, v := range []string{" ", "unquoted", tag + ",", "*, " + tag, `"unterminated`, strings.Repeat("x", 4097)} {
		if _, e := ParseDeviceConfigCondition(v); e == nil {
			t.Fatalf("invalid condition accepted: %q", v)
		}
	}
	original, _ := ParseDeviceConfigCondition(tag)
	next := *c
	next.VoucherType = new(string)
	*next.VoucherType = "ACCESSTOKEN"
	if !errors.Is(original.Check(&next), ErrDeviceConfigVersionConflict) {
		t.Fatal("field change without updated_at missed")
	}
	next = *c
	next.ID = "b"
	if original.Check(&next) == nil {
		t.Fatal("cross-entity token accepted")
	}
}
