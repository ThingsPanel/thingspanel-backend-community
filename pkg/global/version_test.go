package global

import "testing"

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		name, override, compiled, want string
		warn                           bool
	}{
		{"unset", "", "v1.4.0-beta.2", "v1.4.0-beta.2", false},
		{"blank", "  ", "dev", "dev", false},
		{"promote", "v1.4.0", "v1.4.0-beta.2", "v1.4.0", false},
		{"no v prefix", "1.4.0", "dev", "1.4.0", false},
		{"beta", "v1.4.0-beta.3", "dev", "v1.4.0-beta.3", false},
		{"trimmed", " v1.4.0\n", "dev", "v1.4.0", false},
		{"rc rejected", "v1.4.0-rc.1", "dev", "dev", true},
		{"four parts rejected", "v1.4.0.1", "dev", "dev", true},
		{"garbage rejected", "latest", "v1.3.0", "v1.3.0", true},
		{"injection rejected", "v1.4.0\nX", "v1.3.0", "v1.3.0", true},
	}
	for _, c := range cases {
		got, warn := resolveVersion(c.override, c.compiled)
		if got != c.want || warn != c.warn {
			t.Errorf("%s: got (%q,%v) want (%q,%v)", c.name, got, warn, c.want, c.warn)
		}
	}
}
