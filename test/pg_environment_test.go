package test

import "testing"

func TestDatabaseTestConfigPath(t *testing.T) {
	tests := []struct {
		name string
		env  string
		path string
		ok   bool
	}{
		{name: "local development", env: "localdev", path: "../configs/conf-localdev.yml", ok: true},
		{name: "CI", env: "git-actions", path: "../configs/conf-push-test.yml", ok: true},
		{name: "unset", env: "", ok: false},
		{name: "unknown", env: "production", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := databaseTestConfigPath(tt.env)
			if path != tt.path || ok != tt.ok {
				t.Fatalf("databaseTestConfigPath(%q) = (%q, %t), want (%q, %t)", tt.env, path, ok, tt.path, tt.ok)
			}
		})
	}
}
