package strftime

import (
	"errors"
	"testing"
)

func Test_parser_literals(t *testing.T) {
	var noliterals parser
	noliterals.format = func(spec, flag byte) error { return nil }
	noliterals.literal = func(b byte) error { return errors.New("no literals") }

	for _, tt := range []string{"%+", "%c"} {
		if err := noliterals.parse(tt); err != nil {
			t.Errorf("noliterals.parse(%q) = %v", tt, err)
		}
	}

	for _, tt := range []string{"%-", "abc"} {
		if err := noliterals.parse(tt); err == nil {
			t.Errorf("noliterals.parse(%q) = %v", tt, err)
		}
	}
}

func Test_validModifier(t *testing.T) {
	for _, tt := range []string{"Ed", "Oc", "Yy"} {
		if okModifier(tt[0], tt[1]) {
			t.Errorf("okModifier(%q, %q)", tt[0], tt[1])
		}
	}

	for _, tt := range []string{"Ey", "Oy"} {
		if !okModifier(tt[0], tt[1]) {
			t.Errorf("not okModifier(%q, %q)", tt[0], tt[1])
		}
	}
}
