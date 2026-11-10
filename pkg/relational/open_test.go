package relational

import (
	"gorm.io/gorm"
	"testing"
)

func TestAdoptWithFlavorKeepsGORMMySQLDialect(t *testing.T) {
	dial, err := dialector(MySQL, FlavorMariaDB, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := &gorm.DB{Config: &gorm.Config{Dialector: dial}}
	if got := db.Dialector.Name(); got != "mysql" {
		t.Fatalf("GORM dialector name = %q; want mysql", got)
	}
	if got, err := DialectOf(db); err != nil || got != MySQL {
		t.Fatalf("DialectOf = %q, %v", got, err)
	}
	if got, err := FlavorOf(db); err != nil || got != FlavorMariaDB {
		t.Fatalf("FlavorOf = %q, %v", got, err)
	}
}

func TestAdoptDefaultsMySQLFlavor(t *testing.T) {
	dial, err := dialector(MySQL, FlavorMySQL, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := &gorm.DB{Config: &gorm.Config{Dialector: dial}}
	if got, err := FlavorOf(db); err != nil || got != FlavorMySQL {
		t.Fatalf("FlavorOf = %q, %v", got, err)
	}
}

func TestValidateServerVersionFlavorContract(t *testing.T) {
	for version, wantErr := range map[string]bool{
		"10.11.19-MariaDB-ubu2204": false,
		"11.4.13-MariaDB-ubu2404":  false,
		"10.6.20-MariaDB":          true,
		"11.3.2-MariaDB":           true,
		"8.4.4":                    true,
	} {
		err := validateServerVersion(version, FlavorMariaDB, true)
		if (err != nil) != wantErr {
			t.Errorf("validateServerVersion(%q) error=%v, wantErr=%t", version, err, wantErr)
		}
	}
	if err := validateServerVersion("10.11.19-MariaDB", FlavorMySQL, true); err == nil {
		t.Fatal("MariaDB server accepted without explicit flavor")
	}
	if err := validateServerVersion("8.4.4", FlavorMySQL, false); err != nil {
		t.Fatalf("MySQL server rejected: %v", err)
	}
}
