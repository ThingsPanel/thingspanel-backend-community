package sqldialect

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The translator exists once per Go module (the platform and encore-api cannot import each other). The two
// copies must stay byte-identical; change one, copy it to the other.
func TestCopyMatchesEncoreAPI(t *testing.T) {
	other := filepath.Join("..", "..", "encore-api", "pkg", "sqldialect")
	if _, err := os.Stat(other); err != nil {
		t.Skip("encore-api is not checked out next to this package")
	}
	for _, name := range []string{"sqldialect.go", "driver.go", "sqldialect_test.go"} {
		a, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(other, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs from encore-api/pkg/sqldialect/%s", name, name)
		}
	}
}
