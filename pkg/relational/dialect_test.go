package relational

import "testing"

func TestParseDialect(t *testing.T) {
	for raw, want := range map[string]Dialect{"": Postgres, "postgres": Postgres, " POSTGRES ": Postgres, "mysql": MySQL, "MySQL": MySQL} {
		got, err := ParseDialect(raw)
		if err != nil || got != want {
			t.Errorf("ParseDialect(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"postgresql", "pg", "mariadb", "sqlite", "mysql8"} {
		if got, err := ParseDialect(raw); err == nil {
			t.Errorf("ParseDialect(%q) = %q; want an error", raw, got)
		}
	}
}

func TestDialectValid(t *testing.T) {
	if !Postgres.Valid() || !MySQL.Valid() || Dialect("").Valid() || Dialect("x").Valid() {
		t.Fatal("Valid is wrong")
	}
}
